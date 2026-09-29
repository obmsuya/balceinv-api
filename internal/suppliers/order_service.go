package suppliers

import (
	"context"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/identity"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/google/uuid"
)

var knownOrderStatuses = map[string]bool{
	"": true, OrderDraft: true, OrderSent: true, OrderPartlyReceived: true, OrderReceived: true, OrderCancelled: true,
}

func (service *Service) CreateOrder(ctx context.Context, querier database.Querier, principal *identity.Principal, request OrderRequest) (OrderView, error) {
	_, featureError := service.requireFeature(ctx, querier, principal.CompanyId, ordersRule)
	if featureError != nil {
		return OrderView{}, featureError
	}

	foundSupplier, supplierError := service.findUsableSupplier(ctx, querier, principal.CompanyId, uuid.MustParse(request.SupplierId), true)
	if supplierError != nil {
		return OrderView{}, supplierError
	}
	shopId, shopError := service.resolveShop(ctx, querier, principal, request.ShopId)
	if shopError != nil {
		return OrderView{}, shopError
	}
	expectedDate, dateError := parseOptionalDate(request.ExpectedDate)
	if dateError != nil {
		return OrderView{}, dateError
	}

	rawProductIds := make([]string, 0, len(request.Lines))
	for _, lineRequest := range request.Lines {
		rawProductIds = append(rawProductIds, lineRequest.ProductId)
	}
	productIds, productsError := service.validateProducts(ctx, querier, principal.CompanyId, rawProductIds)
	if productsError != nil {
		return OrderView{}, productsError
	}

	createdAt := time.Now().UTC()
	newOrder := PurchaseOrder{
		Id:           uuid.Must(uuid.NewV7()),
		CompanyId:    principal.CompanyId,
		SupplierId:   foundSupplier.Id,
		ShopId:       shopId,
		Status:       OrderDraft,
		ExpectedDate: expectedDate,
		Note:         trimmedOrNil(request.Note),
		CreatedBy:    principal.UserId,
		CreatedAt:    createdAt,
	}
	if request.Send {
		newOrder.Status = OrderSent
		newOrder.SentAt = &createdAt
	}
	orderLines := make([]OrderLine, 0, len(request.Lines))
	for lineIndex, lineRequest := range request.Lines {
		orderLines = append(orderLines, OrderLine{
			ProductId:        productIds[lineIndex],
			QuantityOrdered:  lineRequest.Quantity,
			ExpectedUnitCost: lineRequest.ExpectedUnitCost,
		})
	}

	orderNumber, numberError := service.repository.TakeDocumentNumber(ctx, querier, principal.CompanyId, documentOrder)
	if numberError != nil {
		return OrderView{}, numberError
	}
	newOrder.OrderNumber = orderNumber
	insertError := service.repository.InsertOrder(ctx, querier, newOrder, orderLines)
	if insertError != nil {
		return OrderView{}, insertError
	}

	return service.orderView(ctx, querier, principal.CompanyId, newOrder.Id)
}

func (service *Service) GetOrder(ctx context.Context, querier database.Querier, principal *identity.Principal, orderId uuid.UUID) (OrderView, error) {
	_, featureError := service.requireFeature(ctx, querier, principal.CompanyId, ordersRule)
	if featureError != nil {
		return OrderView{}, featureError
	}
	return service.orderView(ctx, querier, principal.CompanyId, orderId)
}

func (service *Service) orderView(ctx context.Context, querier database.Querier, companyId uuid.UUID, orderId uuid.UUID) (OrderView, error) {
	foundOrder, findError := service.repository.FindOrder(ctx, querier, companyId, orderId)
	if findError != nil {
		return OrderView{}, findError
	}
	if foundOrder == nil {
		return OrderView{}, ErrOrderNotFound
	}
	orderLines, linesError := service.repository.ListOrderLines(ctx, querier, companyId, orderId)
	if linesError != nil {
		return OrderView{}, linesError
	}
	foundOrder.Lines = orderLines
	return *foundOrder, nil
}

func (service *Service) ListOrders(ctx context.Context, querier database.Querier, principal *identity.Principal, filter DocumentFilter, limit int, offset int) (response.Page[OrderView], error) {
	_, featureError := service.requireFeature(ctx, querier, principal.CompanyId, ordersRule)
	if featureError != nil {
		return response.Page[OrderView]{}, featureError
	}
	if !knownOrderStatuses[filter.Status] {
		return response.Page[OrderView]{}, ErrInvalidFilter
	}

	totalOrders, countError := service.repository.CountOrders(ctx, querier, principal.CompanyId, filter)
	if countError != nil {
		return response.Page[OrderView]{}, countError
	}
	orderViews, listError := service.repository.ListOrders(ctx, querier, principal.CompanyId, filter, limit, offset)
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

func (service *Service) SendOrder(ctx context.Context, querier database.Querier, principal *identity.Principal, orderId uuid.UUID) (OrderView, error) {
	_, featureError := service.requireFeature(ctx, querier, principal.CompanyId, ordersRule)
	if featureError != nil {
		return OrderView{}, featureError
	}
	foundOrder, findError := service.orderView(ctx, querier, principal.CompanyId, orderId)
	if findError != nil {
		return OrderView{}, findError
	}
	if foundOrder.Status != OrderDraft {
		return OrderView{}, ErrOrderNotDraft
	}

	sendError := service.repository.MarkOrderSent(ctx, querier, principal.CompanyId, orderId, time.Now().UTC())
	if sendError != nil {
		return OrderView{}, sendError
	}
	return service.orderView(ctx, querier, principal.CompanyId, orderId)
}

func (service *Service) CancelOrder(ctx context.Context, querier database.Querier, principal *identity.Principal, orderId uuid.UUID, request ReasonRequest) (OrderView, error) {
	_, featureError := service.requireFeature(ctx, querier, principal.CompanyId, ordersRule)
	if featureError != nil {
		return OrderView{}, featureError
	}
	cancelReason, reasonError := requireReason(request.Reason)
	if reasonError != nil {
		return OrderView{}, reasonError
	}
	foundOrder, findError := service.orderView(ctx, querier, principal.CompanyId, orderId)
	if findError != nil {
		return OrderView{}, findError
	}
	isClosed := foundOrder.Status == OrderReceived || foundOrder.Status == OrderCancelled
	if isClosed {
		return OrderView{}, ErrOrderClosed
	}

	cancelError := service.repository.CancelOrder(ctx, querier, principal.CompanyId, orderId, principal.UserId, cancelReason, time.Now().UTC())
	if cancelError != nil {
		return OrderView{}, cancelError
	}
	return service.orderView(ctx, querier, principal.CompanyId, orderId)
}
