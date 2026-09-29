package orders

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/accounting"
	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/identity"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/chrisostomemataba/balceinv-api/internal/customers"
	"github.com/chrisostomemataba/balceinv-api/internal/sales"
	"github.com/chrisostomemataba/balceinv-api/internal/stock"
	"github.com/google/uuid"
)

var (
	ErrNoActiveShop          = errors.New("choose a shop first")
	ErrOrderNotFound         = errors.New("order not found")
	ErrInvalidStatus         = errors.New("this order cannot do that in its current state")
	ErrDepositTooHigh        = errors.New("the deposit cannot be more than the order total")
	ErrRefundMethodRequired  = errors.New("choose how the deposit is handed back")
	ErrInvalidDueDate        = errors.New("the due date is not valid")
	ErrInvalidReason         = errors.New("write why the order is being cancelled")
	ErrOrderNumberInFlight   = errors.New("another order was saved at the same moment; try again")
	ErrCreditNeedsSalesRight = errors.New("you do not have permission to sell on credit")
	ErrInvalidStatusFilter   = errors.New("the status filter is not valid")
)

type Service struct {
	repository       *Repository
	salesService     *sales.Service
	customersService *customers.Service
	stockService     *stock.Service
	ledger           *accounting.Ledger
}

func NewService(repository *Repository, salesService *sales.Service, customersService *customers.Service, stockService *stock.Service, ledger *accounting.Ledger) *Service {
	return &Service{
		repository:       repository,
		salesService:     salesService,
		customersService: customersService,
		stockService:     stockService,
		ledger:           ledger,
	}
}

func (service *Service) List(ctx context.Context, querier database.Querier, principal *identity.Principal, filter OrderFilter, limit int, offset int) (response.Page[OrderView], error) {
	if principal.ShopId == nil {
		return response.Page[OrderView]{}, ErrNoActiveShop
	}
	isKnownStatus := filter.Status == "" || filter.Status == StatusOpen || filter.Status == StatusReady || filter.Status == StatusCollected || filter.Status == StatusCancelled
	if !isKnownStatus {
		return response.Page[OrderView]{}, ErrInvalidStatusFilter
	}

	orderViews, totalOrders, listError := service.repository.List(ctx, querier, principal.CompanyId, *principal.ShopId, filter, limit, offset)
	if listError != nil {
		return response.Page[OrderView]{}, listError
	}

	orderPage := response.Page[OrderView]{
		Items:  orderViews,
		Total:  totalOrders,
		Limit:  limit,
		Offset: offset,
	}
	return orderPage, nil
}

func (service *Service) Get(ctx context.Context, querier database.Querier, principal *identity.Principal, orderId uuid.UUID) (OrderView, error) {
	foundOrder, findError := service.find(ctx, querier, principal, orderId)
	if findError != nil {
		return OrderView{}, findError
	}

	pricedLines, linesError := service.repository.PricedLines(ctx, querier, principal.CompanyId, orderId)
	if linesError != nil {
		return OrderView{}, linesError
	}
	paymentViews, paymentsError := service.repository.ListPayments(ctx, querier, principal.CompanyId, orderId)
	if paymentsError != nil {
		return OrderView{}, paymentsError
	}

	for _, pricedLine := range pricedLines {
		foundOrder.Lines = append(foundOrder.Lines, OrderLineView{
			ProductId:      pricedLine.Product.Id,
			ProductName:    pricedLine.Product.Name,
			VariantLabel:   pricedLine.Product.VariantLabel,
			Sku:            pricedLine.Product.Sku,
			Unit:           pricedLine.Product.Unit,
			Quantity:       pricedLine.Quantity,
			UnitPrice:      pricedLine.UnitPrice,
			IsWholesale:    pricedLine.IsWholesale,
			DiscountName:   pricedLine.DiscountName,
			DiscountAmount: pricedLine.DiscountAmount,
			LineTotal:      pricedLine.LineTotal,
		})
	}
	foundOrder.Payments = paymentViews
	return *foundOrder, nil
}

func (service *Service) Create(ctx context.Context, querier database.Querier, principal *identity.Principal, request OrderRequest) (OrderView, error) {
	if principal.ShopId == nil {
		return OrderView{}, ErrNoActiveShop
	}

	customerId := uuid.MustParse(request.CustomerId)
	customerError := service.customersService.RequireActive(ctx, querier, principal.CompanyId, customerId)
	if customerError != nil {
		return OrderView{}, customerError
	}

	dueDate, dueDateError := parseDueDate(request.DueDate)
	if dueDateError != nil {
		return OrderView{}, dueDateError
	}

	lineRequests := make([]sales.LineRequest, 0, len(request.Items))
	for _, itemRequest := range request.Items {
		lineRequests = append(lineRequests, sales.LineRequest{ProductId: itemRequest.ProductId, Quantity: itemRequest.Quantity})
	}
	pricedSale, priceError := service.salesService.PriceForOrder(ctx, querier, principal, lineRequests)
	if priceError != nil {
		return OrderView{}, priceError
	}

	hasDeposit := request.Deposit != nil
	if hasDeposit && request.Deposit.Amount > pricedSale.Total {
		return OrderView{}, ErrDepositTooHigh
	}

	orderNumber, numberError := service.repository.NextNumber(ctx, querier, principal.CompanyId)
	if numberError != nil {
		return OrderView{}, numberError
	}

	createdAt := time.Now().UTC()
	newOrder := Order{
		Id:                 uuid.Must(uuid.NewV7()),
		CompanyId:          principal.CompanyId,
		ShopId:             *principal.ShopId,
		CustomerId:         customerId,
		Number:             orderNumber,
		Status:             StatusOpen,
		DueDate:            dueDate,
		Note:               trimmedOrNil(request.Note),
		Subtotal:           pricedSale.Subtotal,
		DiscountTotal:      pricedSale.DiscountTotal,
		Total:              pricedSale.Total,
		TaxTotal:           pricedSale.TaxTotal,
		TaxRateBasisPoints: pricedSale.TaxRateBasisPoints,
		CreatedBy:          principal.UserId,
		CreatedAt:          createdAt,
	}
	insertError := service.repository.Insert(ctx, querier, newOrder, pricedSale.Lines)
	if database.IsUniqueViolation(insertError) {
		return OrderView{}, ErrOrderNumberInFlight
	}
	if insertError != nil {
		return OrderView{}, insertError
	}

	if hasDeposit {
		depositError := service.recordMoney(ctx, querier, principal, newOrder.Id, KindDeposit, request.Deposit.Method, request.Deposit.Amount)
		if depositError != nil {
			return OrderView{}, depositError
		}
	}

	orderReference := sales.FormatOrderNumber(orderNumber)
	for _, pricedLine := range pricedSale.Lines {
		reserveError := service.moveStock(ctx, querier, principal, newOrder.ShopId, pricedLine.Product.Id, -pricedLine.Quantity, ReasonReserved, orderReference)
		if reserveError != nil {
			return OrderView{}, reserveError
		}
	}

	return service.Get(ctx, querier, principal, newOrder.Id)
}

func (service *Service) AddDeposit(ctx context.Context, querier database.Querier, principal *identity.Principal, orderId uuid.UUID, request MoneyRequest) (OrderView, error) {
	foundOrder, lockError := service.lockAwaiting(ctx, querier, principal, orderId)
	if lockError != nil {
		return OrderView{}, lockError
	}
	if foundOrder.DepositTotal+request.Amount > foundOrder.Total {
		return OrderView{}, ErrDepositTooHigh
	}

	depositError := service.recordMoney(ctx, querier, principal, orderId, KindDeposit, request.Method, request.Amount)
	if depositError != nil {
		return OrderView{}, depositError
	}
	return service.Get(ctx, querier, principal, orderId)
}

func (service *Service) MarkReady(ctx context.Context, querier database.Querier, principal *identity.Principal, orderId uuid.UUID) (OrderView, error) {
	foundOrder, lockError := service.lockAwaiting(ctx, querier, principal, orderId)
	if lockError != nil {
		return OrderView{}, lockError
	}
	if foundOrder.Status != StatusOpen {
		return OrderView{}, ErrInvalidStatus
	}

	readyError := service.repository.MarkReady(ctx, querier, principal.CompanyId, orderId, time.Now().UTC())
	if readyError != nil {
		return OrderView{}, readyError
	}
	return service.Get(ctx, querier, principal, orderId)
}

func (service *Service) Collect(ctx context.Context, querier database.Querier, principal *identity.Principal, orderId uuid.UUID, request CollectRequest) (OrderView, error) {
	foundOrder, lockError := service.lockAwaiting(ctx, querier, principal, orderId)
	if lockError != nil {
		return OrderView{}, lockError
	}

	collectionPayments, paymentsError := service.collectionPayments(ctx, querier, principal, orderId, request.Payments)
	if paymentsError != nil {
		return OrderView{}, paymentsError
	}
	pricedLines, linesError := service.repository.PricedLines(ctx, querier, principal.CompanyId, orderId)
	if linesError != nil {
		return OrderView{}, linesError
	}

	orderSale := sales.OrderSaleRequest{
		ClientRef:          "order-" + orderId.String(),
		ShopId:             foundOrder.ShopId,
		CustomerId:         foundOrder.CustomerId,
		Lines:              pricedLines,
		TaxRateBasisPoints: foundOrder.TaxRateBasisPoints,
		Payments:           collectionPayments,
	}
	saleView, saleError := service.salesService.CreateFromOrder(ctx, querier, principal, orderSale)
	if saleError != nil {
		return OrderView{}, saleError
	}

	collectedError := service.repository.MarkCollected(ctx, querier, principal.CompanyId, orderId, saleView.Id, time.Now().UTC())
	if collectedError != nil {
		return OrderView{}, collectedError
	}
	postError := service.ledger.PostSaleById(ctx, querier, principal.CompanyId, saleView.Id)
	if postError != nil {
		return OrderView{}, postError
	}
	return service.Get(ctx, querier, principal, orderId)
}

func (service *Service) Cancel(ctx context.Context, querier database.Querier, principal *identity.Principal, orderId uuid.UUID, request CancelRequest) (OrderView, error) {
	foundOrder, lockError := service.lockAwaiting(ctx, querier, principal, orderId)
	if lockError != nil {
		return OrderView{}, lockError
	}

	cancelReason := strings.TrimSpace(request.Reason)
	if cancelReason == "" {
		return OrderView{}, ErrInvalidReason
	}
	hasDeposit := foundOrder.DepositTotal > 0
	if hasDeposit && request.RefundMethod == nil {
		return OrderView{}, ErrRefundMethodRequired
	}
	if hasDeposit {
		refundError := service.recordMoney(ctx, querier, principal, orderId, KindRefund, *request.RefundMethod, foundOrder.DepositTotal)
		if refundError != nil {
			return OrderView{}, refundError
		}
	}

	pricedLines, linesError := service.repository.PricedLines(ctx, querier, principal.CompanyId, orderId)
	if linesError != nil {
		return OrderView{}, linesError
	}
	for _, pricedLine := range pricedLines {
		returnError := service.moveStock(ctx, querier, principal, foundOrder.ShopId, pricedLine.Product.Id, pricedLine.Quantity, ReasonCancelled, foundOrder.Number)
		if returnError != nil {
			return OrderView{}, returnError
		}
	}

	cancelError := service.repository.MarkCancelled(ctx, querier, principal.CompanyId, orderId, principal.UserId, cancelReason, time.Now().UTC())
	if cancelError != nil {
		return OrderView{}, cancelError
	}
	return service.Get(ctx, querier, principal, orderId)
}

func (service *Service) collectionPayments(ctx context.Context, querier database.Querier, principal *identity.Principal, orderId uuid.UUID, paymentRequests []sales.PaymentRequest) ([]sales.PaymentRequest, error) {
	amountByMethod := map[string]int64{}
	methodOrder := []string{}
	addAmount := func(method string, amount int64) {
		if _, isKnownMethod := amountByMethod[method]; !isKnownMethod {
			methodOrder = append(methodOrder, method)
		}
		amountByMethod[method] += amount
	}

	seenMethods := map[string]bool{}
	for _, paymentRequest := range paymentRequests {
		if seenMethods[paymentRequest.Method] {
			return nil, sales.ErrDuplicatePayment
		}
		seenMethods[paymentRequest.Method] = true
		isCredit := paymentRequest.Method == sales.PaymentCredit
		if isCredit && !principal.Can("sales:create") {
			return nil, ErrCreditNeedsSalesRight
		}
	}

	orderPayments, paymentsError := service.repository.ListPayments(ctx, querier, principal.CompanyId, orderId)
	if paymentsError != nil {
		return nil, paymentsError
	}
	for _, orderPayment := range orderPayments {
		signedAmount := orderPayment.Amount
		if orderPayment.Kind == KindRefund {
			signedAmount = -orderPayment.Amount
		}
		addAmount(orderPayment.Method, signedAmount)
	}
	for _, paymentRequest := range paymentRequests {
		addAmount(paymentRequest.Method, paymentRequest.Amount)
	}

	mergedPayments := []sales.PaymentRequest{}
	for _, method := range methodOrder {
		if amountByMethod[method] > 0 {
			mergedPayments = append(mergedPayments, sales.PaymentRequest{Method: method, Amount: amountByMethod[method]})
		}
	}
	return mergedPayments, nil
}

func (service *Service) recordMoney(ctx context.Context, querier database.Querier, principal *identity.Principal, orderId uuid.UUID, kind string, method string, amount int64) error {
	orderPayment := OrderPayment{
		Id:        uuid.Must(uuid.NewV7()),
		CompanyId: principal.CompanyId,
		OrderId:   orderId,
		Kind:      kind,
		Method:    method,
		Amount:    amount,
		CreatedBy: principal.UserId,
		CreatedAt: time.Now().UTC(),
	}
	insertError := service.repository.InsertPayment(ctx, querier, orderPayment)
	if insertError != nil {
		return insertError
	}
	return service.ledger.PostOrderMoneyById(ctx, querier, principal.CompanyId, orderPayment.Id)
}

func (service *Service) moveStock(ctx context.Context, querier database.Querier, principal *identity.Principal, shopId uuid.UUID, productId uuid.UUID, change int, reason string, orderReference string) error {
	stockMovement := stock.MovementRequest{
		CompanyId: principal.CompanyId,
		ShopId:    shopId,
		ProductId: productId,
		Change:    change,
		Reason:    reason,
		Reference: &orderReference,
		UserId:    &principal.UserId,
	}
	_, movementError := service.stockService.RecordMovement(ctx, querier, stockMovement)
	return movementError
}

func (service *Service) lockAwaiting(ctx context.Context, querier database.Querier, principal *identity.Principal, orderId uuid.UUID) (*OrderView, error) {
	if principal.ShopId == nil {
		return nil, ErrNoActiveShop
	}
	wasLocked, lockError := service.repository.Lock(ctx, querier, principal.CompanyId, *principal.ShopId, orderId)
	if lockError != nil {
		return nil, lockError
	}
	if !wasLocked {
		return nil, ErrOrderNotFound
	}

	foundOrder, findError := service.find(ctx, querier, principal, orderId)
	if findError != nil {
		return nil, findError
	}
	isAwaitingCollection := foundOrder.Status == StatusOpen || foundOrder.Status == StatusReady
	if !isAwaitingCollection {
		return nil, ErrInvalidStatus
	}
	return foundOrder, nil
}

func (service *Service) find(ctx context.Context, querier database.Querier, principal *identity.Principal, orderId uuid.UUID) (*OrderView, error) {
	if principal.ShopId == nil {
		return nil, ErrNoActiveShop
	}
	foundOrder, findError := service.repository.Find(ctx, querier, principal.CompanyId, *principal.ShopId, orderId)
	if findError != nil {
		return nil, findError
	}
	if foundOrder == nil {
		return nil, ErrOrderNotFound
	}
	return foundOrder, nil
}

func parseDueDate(rawDueDate *string) (*string, error) {
	dueDateText := trimmedOrNil(rawDueDate)
	if dueDateText == nil {
		return nil, nil
	}
	_, parseError := time.Parse("2006-01-02", *dueDateText)
	if parseError != nil {
		return nil, ErrInvalidDueDate
	}
	return dueDateText, nil
}

func trimmedOrNil(value *string) *string {
	if value == nil {
		return nil
	}
	trimmedValue := strings.TrimSpace(*value)
	if trimmedValue == "" {
		return nil
	}
	return &trimmedValue
}
