package suppliers

import (
	"context"
	"errors"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/identity"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/chrisostomemataba/balceinv-api/internal/features"
	"github.com/chrisostomemataba/balceinv-api/internal/media"
	"github.com/chrisostomemataba/balceinv-api/internal/stock"
	"github.com/google/uuid"
)

const defaultPaymentMethod = "cash"

func (service *Service) RecordPurchase(ctx context.Context, querier database.Querier, principal *identity.Principal, request PurchaseRequest) (PurchaseView, error) {
	companyFeatures, featureError := service.requireFeature(ctx, querier, principal.CompanyId, purchasesRule)
	if featureError != nil {
		return PurchaseView{}, featureError
	}

	existingPurchaseId, lookupError := service.repository.FindPurchaseIdByClientRef(ctx, querier, principal.CompanyId, request.ClientRef)
	if lookupError != nil {
		return PurchaseView{}, lookupError
	}
	if existingPurchaseId != nil {
		return service.purchaseView(ctx, querier, principal.CompanyId, *existingPurchaseId)
	}

	shopId, shopError := service.resolveShop(ctx, querier, principal, request.ShopId)
	if shopError != nil {
		return PurchaseView{}, shopError
	}

	supplierId, orderId, sourceError := service.resolvePurchaseSource(ctx, querier, principal.CompanyId, companyFeatures, request)
	if sourceError != nil {
		return PurchaseView{}, sourceError
	}

	rawProductIds := make([]string, 0, len(request.Lines))
	for _, lineRequest := range request.Lines {
		rawProductIds = append(rawProductIds, lineRequest.ProductId)
	}
	productIds, productsError := service.validateProducts(ctx, querier, principal.CompanyId, rawProductIds)
	if productsError != nil {
		return PurchaseView{}, productsError
	}
	costedProducts, costsError := service.repository.LoadCostedProducts(ctx, querier, principal.CompanyId, productIds)
	if costsError != nil {
		return PurchaseView{}, costsError
	}

	invoiceDate, dateError := parseOptionalDate(request.InvoiceDate)
	if dateError != nil {
		return PurchaseView{}, dateError
	}
	createdAt := time.Now().UTC()
	receivedAt, momentError := resolveMoment(request.ReceivedAt, createdAt)
	if momentError != nil {
		return PurchaseView{}, momentError
	}

	companySettings, settingsError := service.settingsRepository.FindSettings(ctx, querier, principal.CompanyId)
	if settingsError != nil {
		return PurchaseView{}, settingsError
	}
	if companySettings == nil {
		return PurchaseView{}, ErrMissingSettings
	}

	chargesVat := companyFeatures.VatRegistered && request.InvoiceHasVat
	pricesIncludeVat := chargesVat && request.PricesIncludeVat
	pricedLines := make([]PricedLine, 0, len(request.Lines))
	newPurchase := Purchase{
		Id:                    uuid.Must(uuid.NewV7()),
		CompanyId:             principal.CompanyId,
		ShopId:                shopId,
		SupplierId:            supplierId,
		SupplierInvoiceNumber: trimmedOrNil(request.SupplierInvoiceNumber),
		InvoiceDate:           invoiceDate,
		ReceivedAt:            receivedAt,
		PricesIncludeVat:      pricesIncludeVat,
		Note:                  trimmedOrNil(request.Note),
		PurchaseOrderId:       orderId,
		ClientRef:             request.ClientRef,
		CreatedBy:             principal.UserId,
		CreatedAt:             createdAt,
	}
	for lineIndex, lineRequest := range request.Lines {
		pricedLine := PriceLine(productIds[lineIndex], lineRequest.Quantity, lineRequest.UnitCost, chargesVat, pricesIncludeVat, companySettings.TaxRateBasisPoints)
		pricedLines = append(pricedLines, pricedLine)
		newPurchase.Subtotal += pricedLine.LineTotal - pricedLine.VatAmount
		newPurchase.VatTotal += pricedLine.VatAmount
		newPurchase.Total += pricedLine.LineTotal
	}

	if request.AmountPaid > newPurchase.Total {
		return PurchaseView{}, ErrPaidMoreThanTotal
	}
	if supplierId == nil && request.AmountPaid != newPurchase.Total {
		return PurchaseView{}, ErrMustPayInFull
	}

	purchaseNumber, numberError := service.repository.TakeDocumentNumber(ctx, querier, principal.CompanyId, documentPurchase)
	if numberError != nil {
		return PurchaseView{}, numberError
	}
	newPurchase.PurchaseNumber = purchaseNumber

	insertError := service.repository.InsertPurchase(ctx, querier, newPurchase, pricedLines)
	if database.IsUniqueViolation(insertError) {
		return PurchaseView{}, ErrClientRefInFlight
	}
	if insertError != nil {
		return PurchaseView{}, insertError
	}

	for _, pricedLine := range pricedLines {
		receiveError := service.receiveLine(ctx, querier, principal, newPurchase, pricedLine, costedProducts[pricedLine.ProductId])
		if receiveError != nil {
			return PurchaseView{}, receiveError
		}
	}
	if orderId != nil {
		refreshError := service.repository.RefreshOrderStatus(ctx, querier, principal.CompanyId, *orderId)
		if refreshError != nil {
			return PurchaseView{}, refreshError
		}
	}

	if request.AmountPaid > 0 {
		paymentMethod := request.PaymentMethod
		if paymentMethod == "" {
			paymentMethod = defaultPaymentMethod
		}
		_, paymentError := service.insertPayment(ctx, querier, Payment{
			CompanyId:  principal.CompanyId,
			SupplierId: supplierId,
			PurchaseId: &newPurchase.Id,
			ShopId:     shopId,
			Amount:     request.AmountPaid,
			Method:     paymentMethod,
			Reference:  trimmedOrNil(request.PaymentReference),
			PaidAt:     receivedAt,
			CreatedBy:  principal.UserId,
			CreatedAt:  createdAt,
		})
		if paymentError != nil {
			return PurchaseView{}, paymentError
		}
	}

	return service.purchaseView(ctx, querier, principal.CompanyId, newPurchase.Id)
}

func (service *Service) resolvePurchaseSource(ctx context.Context, querier database.Querier, companyId uuid.UUID, companyFeatures features.Features, request PurchaseRequest) (*uuid.UUID, *uuid.UUID, error) {
	var supplierId *uuid.UUID
	if request.SupplierId != nil {
		if !companyFeatures.SuppliersEnabled {
			return nil, nil, ErrFeatureOff
		}
		requestedSupplierId := uuid.MustParse(*request.SupplierId)
		supplierId = &requestedSupplierId
	}

	var orderId *uuid.UUID
	if request.PurchaseOrderId != nil {
		if !ordersRule(companyFeatures) {
			return nil, nil, ErrFeatureOff
		}
		foundOrder, findError := service.repository.FindOrder(ctx, querier, companyId, uuid.MustParse(*request.PurchaseOrderId))
		if findError != nil {
			return nil, nil, findError
		}
		if foundOrder == nil {
			return nil, nil, ErrOrderNotFound
		}
		isClosed := foundOrder.Status == OrderReceived || foundOrder.Status == OrderCancelled
		if isClosed {
			return nil, nil, ErrOrderClosed
		}
		if supplierId != nil && *supplierId != foundOrder.SupplierId {
			return nil, nil, ErrOrderSupplierMismatch
		}
		supplierId = &foundOrder.SupplierId
		orderId = &foundOrder.Id
	}

	if supplierId != nil {
		_, supplierError := service.findUsableSupplier(ctx, querier, companyId, *supplierId, true)
		if supplierError != nil {
			return nil, nil, supplierError
		}
	}
	return supplierId, orderId, nil
}

func (service *Service) receiveLine(ctx context.Context, querier database.Querier, principal *identity.Principal, newPurchase Purchase, pricedLine PricedLine, costedProduct CostedProduct) error {
	receivedAmount := pricedLine.LineTotal - pricedLine.VatAmount
	averageCost := WeightedAverageCost(costedProduct.OnHandQuantity, costedProduct.CostPrice, int64(pricedLine.Quantity), receivedAmount)
	if averageCost != costedProduct.CostPrice {
		costError := service.repository.SetProductCost(ctx, querier, principal.CompanyId, pricedLine.ProductId, averageCost)
		if costError != nil {
			return costError
		}
	}

	purchaseMovement := stock.MovementRequest{
		CompanyId: principal.CompanyId,
		ShopId:    newPurchase.ShopId,
		ProductId: pricedLine.ProductId,
		Change:    pricedLine.Quantity,
		Reason:    stockReasonPurchase,
		Reference: &newPurchase.PurchaseNumber,
		UserId:    &principal.UserId,
	}
	_, movementError := service.stockService.RecordMovement(ctx, querier, purchaseMovement)
	if movementError != nil {
		return movementError
	}

	if newPurchase.SupplierId != nil && costedProduct.PreferredSupplierId == nil {
		preferredError := service.repository.SetPreferredSupplier(ctx, querier, principal.CompanyId, pricedLine.ProductId, *newPurchase.SupplierId)
		if preferredError != nil {
			return preferredError
		}
	}

	if newPurchase.PurchaseOrderId != nil {
		return service.repository.AddReceivedToOrder(ctx, querier, principal.CompanyId, *newPurchase.PurchaseOrderId, pricedLine.ProductId, pricedLine.Quantity)
	}
	return nil
}

func (service *Service) insertPayment(ctx context.Context, querier database.Querier, newPayment Payment) (uuid.UUID, error) {
	paymentNumber, numberError := service.repository.TakeDocumentNumber(ctx, querier, newPayment.CompanyId, documentPayment)
	if numberError != nil {
		return uuid.Nil, numberError
	}
	newPayment.Id = uuid.Must(uuid.NewV7())
	newPayment.PaymentNumber = paymentNumber
	insertError := service.repository.InsertPayment(ctx, querier, newPayment)
	if insertError != nil {
		return uuid.Nil, insertError
	}
	return newPayment.Id, nil
}

func (service *Service) ListPurchases(ctx context.Context, querier database.Querier, principal *identity.Principal, filter DocumentFilter, limit int, offset int) (response.Page[PurchaseView], error) {
	_, featureError := service.requireFeature(ctx, querier, principal.CompanyId, purchasesRule)
	if featureError != nil {
		return response.Page[PurchaseView]{}, featureError
	}
	isKnownStatus := filter.Status == "" || filter.Status == PurchaseReceived || filter.Status == PurchaseCancelled
	if !isKnownStatus {
		return response.Page[PurchaseView]{}, ErrInvalidFilter
	}

	totalPurchases, countError := service.repository.CountPurchases(ctx, querier, principal.CompanyId, filter)
	if countError != nil {
		return response.Page[PurchaseView]{}, countError
	}
	purchaseViews, listError := service.repository.ListPurchases(ctx, querier, principal.CompanyId, filter, limit, offset)
	if listError != nil {
		return response.Page[PurchaseView]{}, listError
	}
	statusError := service.fillPaymentStatus(ctx, querier, principal.CompanyId, purchaseViews)
	if statusError != nil {
		return response.Page[PurchaseView]{}, statusError
	}

	purchasePage := response.Page[PurchaseView]{
		Items:  purchaseViews,
		Total:  totalPurchases,
		Limit:  limit,
		Offset: offset,
	}
	return purchasePage, nil
}

func (service *Service) GetPurchase(ctx context.Context, querier database.Querier, principal *identity.Principal, purchaseId uuid.UUID) (PurchaseView, error) {
	_, featureError := service.requireFeature(ctx, querier, principal.CompanyId, purchasesRule)
	if featureError != nil {
		return PurchaseView{}, featureError
	}
	return service.purchaseView(ctx, querier, principal.CompanyId, purchaseId)
}

func (service *Service) purchaseView(ctx context.Context, querier database.Querier, companyId uuid.UUID, purchaseId uuid.UUID) (PurchaseView, error) {
	foundPurchase, findError := service.repository.FindPurchase(ctx, querier, companyId, purchaseId)
	if findError != nil {
		return PurchaseView{}, findError
	}
	if foundPurchase == nil {
		return PurchaseView{}, ErrPurchaseNotFound
	}

	purchaseLines, linesError := service.repository.ListPurchaseLines(ctx, querier, companyId, purchaseId)
	if linesError != nil {
		return PurchaseView{}, linesError
	}
	purchasePayments, paymentsError := service.repository.ListPurchasePayments(ctx, querier, companyId, purchaseId)
	if paymentsError != nil {
		return PurchaseView{}, paymentsError
	}
	foundPurchase.Lines = purchaseLines
	foundPurchase.Payments = purchasePayments

	purchaseViews := []PurchaseView{*foundPurchase}
	statusError := service.fillPaymentStatus(ctx, querier, companyId, purchaseViews)
	if statusError != nil {
		return PurchaseView{}, statusError
	}
	return purchaseViews[0], nil
}

func (service *Service) fillPaymentStatus(ctx context.Context, querier database.Querier, companyId uuid.UUID, purchaseViews []PurchaseView) error {
	seenSuppliers := map[uuid.UUID]bool{}
	suppliersInScope := []Supplier{}
	for _, purchaseView := range purchaseViews {
		needsLedger := purchaseView.SupplierId != nil && purchaseView.Status == PurchaseReceived && !seenSuppliers[*purchaseView.SupplierId]
		if !needsLedger {
			continue
		}
		seenSuppliers[*purchaseView.SupplierId] = true
		foundSupplier, findError := service.repository.FindSupplier(ctx, querier, companyId, *purchaseView.SupplierId)
		if findError != nil {
			return findError
		}
		if foundSupplier != nil {
			suppliersInScope = append(suppliersInScope, *foundSupplier)
		}
	}

	remainingByPurchase := map[uuid.UUID]int64{}
	if len(suppliersInScope) > 0 {
		ledgers, ledgerError := service.repository.LoadLedgers(ctx, querier, companyId, suppliersInScope, true)
		if ledgerError != nil {
			return ledgerError
		}
		for _, supplierInScope := range suppliersInScope {
			openDebits, _ := Allocate(ledgers[supplierInScope.Id])
			for _, openDebit := range openDebits {
				if openDebit.PurchaseId != nil {
					remainingByPurchase[*openDebit.PurchaseId] = openDebit.Remaining
				}
			}
		}
	}

	for purchaseIndex := range purchaseViews {
		purchaseView := &purchaseViews[purchaseIndex]
		switch {
		case purchaseView.Status == PurchaseCancelled:
			purchaseView.PaymentStatus = PurchaseCancelled
		case purchaseView.SupplierId == nil:
			purchaseView.AmountPaid = purchaseView.Total
			purchaseView.PaymentStatus = PaymentPaid
		default:
			purchaseView.AmountDue = remainingByPurchase[purchaseView.Id]
			purchaseView.AmountPaid = purchaseView.Total - purchaseView.AmountDue
			purchaseView.PaymentStatus = paymentStatusFor(purchaseView.Total, purchaseView.AmountDue)
		}
	}
	return nil
}

func paymentStatusFor(total int64, amountDue int64) string {
	switch {
	case amountDue <= 0:
		return PaymentPaid
	case amountDue < total:
		return PaymentPartPaid
	default:
		return PaymentUnpaid
	}
}

func (service *Service) CancelPurchase(ctx context.Context, querier database.Querier, principal *identity.Principal, purchaseId uuid.UUID, request ReasonRequest) (PurchaseView, error) {
	_, featureError := service.requireFeature(ctx, querier, principal.CompanyId, purchasesRule)
	if featureError != nil {
		return PurchaseView{}, featureError
	}
	cancelReason, reasonError := requireReason(request.Reason)
	if reasonError != nil {
		return PurchaseView{}, reasonError
	}

	foundPurchase, findError := service.repository.FindPurchase(ctx, querier, principal.CompanyId, purchaseId)
	if findError != nil {
		return PurchaseView{}, findError
	}
	if foundPurchase == nil {
		return PurchaseView{}, ErrPurchaseNotFound
	}
	if foundPurchase.Status == PurchaseCancelled {
		return PurchaseView{}, ErrAlreadyCancelled
	}
	purchaseShopId := foundPurchase.ShopId.String()
	_, shopError := service.resolveShop(ctx, querier, principal, &purchaseShopId)
	if shopError != nil {
		return PurchaseView{}, shopError
	}

	purchaseLines, linesError := service.repository.ListPurchaseLines(ctx, querier, principal.CompanyId, purchaseId)
	if linesError != nil {
		return PurchaseView{}, linesError
	}
	for _, purchaseLine := range purchaseLines {
		reversingMovement := stock.MovementRequest{
			CompanyId: principal.CompanyId,
			ShopId:    foundPurchase.ShopId,
			ProductId: purchaseLine.ProductId,
			Change:    -purchaseLine.Quantity,
			Reason:    stockReasonPurchase,
			Reference: &foundPurchase.PurchaseNumber,
			UserId:    &principal.UserId,
		}
		_, movementError := service.stockService.RecordMovement(ctx, querier, reversingMovement)
		if errors.Is(movementError, stock.ErrInsufficientStock) {
			return PurchaseView{}, ErrStockAlreadyUsed
		}
		if movementError != nil {
			return PurchaseView{}, movementError
		}
		if foundPurchase.PurchaseOrderId != nil {
			orderError := service.repository.AddReceivedToOrder(ctx, querier, principal.CompanyId, *foundPurchase.PurchaseOrderId, purchaseLine.ProductId, -purchaseLine.Quantity)
			if orderError != nil {
				return PurchaseView{}, orderError
			}
		}
	}
	if foundPurchase.PurchaseOrderId != nil {
		refreshError := service.repository.RefreshOrderStatus(ctx, querier, principal.CompanyId, *foundPurchase.PurchaseOrderId)
		if refreshError != nil {
			return PurchaseView{}, refreshError
		}
	}

	cancelledAt := time.Now().UTC()
	linkedPayments, paymentsError := service.repository.ListPurchasePayments(ctx, querier, principal.CompanyId, purchaseId)
	if paymentsError != nil {
		return PurchaseView{}, paymentsError
	}
	voidReason := "Cancelled with " + foundPurchase.PurchaseNumber + ": " + cancelReason
	for _, linkedPayment := range linkedPayments {
		if linkedPayment.IsVoided {
			continue
		}
		voidError := service.repository.VoidPayment(ctx, querier, principal.CompanyId, linkedPayment.Id, principal.UserId, voidReason, cancelledAt)
		if voidError != nil {
			return PurchaseView{}, voidError
		}
	}

	cancelError := service.repository.CancelPurchase(ctx, querier, principal.CompanyId, purchaseId, principal.UserId, cancelReason, cancelledAt)
	if cancelError != nil {
		return PurchaseView{}, cancelError
	}
	return service.purchaseView(ctx, querier, principal.CompanyId, purchaseId)
}

func (service *Service) AttachInvoicePhoto(ctx context.Context, querier database.Querier, principal *identity.Principal, purchaseId uuid.UUID, imageBytes []byte) (PurchaseView, error) {
	_, featureError := service.requireFeature(ctx, querier, principal.CompanyId, purchasesRule)
	if featureError != nil {
		return PurchaseView{}, featureError
	}
	foundPurchase, findError := service.repository.FindPurchase(ctx, querier, principal.CompanyId, purchaseId)
	if findError != nil {
		return PurchaseView{}, findError
	}
	if foundPurchase == nil {
		return PurchaseView{}, ErrPurchaseNotFound
	}

	attachmentKey, storeError := media.StoreImage(ctx, service.objectStore, media.FolderPurchases, principal.CompanyId, imageBytes, MaximumAttachmentBytes)
	if storeError != nil {
		return PurchaseView{}, storeError
	}
	attachError := service.repository.SetAttachmentKey(ctx, querier, principal.CompanyId, purchaseId, attachmentKey)
	if attachError != nil {
		return PurchaseView{}, attachError
	}
	return service.purchaseView(ctx, querier, principal.CompanyId, purchaseId)
}

func (service *Service) LastCost(ctx context.Context, querier database.Querier, principal *identity.Principal, productId uuid.UUID, supplierId *uuid.UUID) (LastCostView, error) {
	_, featureError := service.requireFeature(ctx, querier, principal.CompanyId, purchasesRule)
	if featureError != nil {
		return LastCostView{}, featureError
	}
	return service.repository.FindLastCost(ctx, querier, principal.CompanyId, productId, supplierId)
}
