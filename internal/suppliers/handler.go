package suppliers

import (
	"errors"
	"io"

	"github.com/chrisostomemataba/balceinv-api/internal/common/httpx"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/chrisostomemataba/balceinv-api/internal/documents"
	"github.com/chrisostomemataba/balceinv-api/internal/media"
	"github.com/chrisostomemataba/balceinv-api/internal/stock"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{
		service: service,
	}
}

func optionalIdQuery(c *fiber.Ctx, parameterName string) (*uuid.UUID, bool) {
	rawId := c.Query(parameterName)
	if rawId == "" {
		return nil, true
	}
	parsedId, parseError := uuid.Parse(rawId)
	if parseError != nil {
		return nil, false
	}
	return &parsedId, true
}

func documentFilterFrom(c *fiber.Ctx) (DocumentFilter, bool) {
	supplierId, isValidSupplier := optionalIdQuery(c, "supplier_id")
	orderId, isValidOrder := optionalIdQuery(c, "purchase_order_id")
	documentFilter := DocumentFilter{
		SupplierId:      supplierId,
		PurchaseOrderId: orderId,
		Status:          c.Query("status"),
	}
	return documentFilter, isValidSupplier && isValidOrder
}

func (handler *Handler) ListSuppliers(c *fiber.Ctx) error {
	pagination := httpx.ParsePagination(c)
	supplierFilter := SupplierFilter{
		SearchText:      c.Query("q"),
		IncludeInactive: c.QueryBool("include_inactive"),
	}

	supplierPage, listError := handler.service.ListSuppliers(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), supplierFilter, pagination.Limit, pagination.Offset)
	if listError != nil {
		return respondWithServiceError(c, listError)
	}
	return response.Paged(c, "Suppliers", supplierPage)
}

func (handler *Handler) GetSupplier(c *fiber.Ctx) error {
	supplierId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrSupplierNotFound)
	}

	supplierView, getError := handler.service.GetSupplier(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), supplierId)
	if getError != nil {
		return respondWithServiceError(c, getError)
	}
	return response.Success(c, "Supplier", supplierView)
}

func (handler *Handler) CreateSupplier(c *fiber.Ctx) error {
	request := SupplierRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	supplierView, createError := handler.service.CreateSupplier(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), request)
	if createError != nil {
		return respondWithServiceError(c, createError)
	}
	return response.Created(c, "Supplier added", supplierView)
}

func (handler *Handler) UpdateSupplier(c *fiber.Ctx) error {
	supplierId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrSupplierNotFound)
	}
	request := SupplierRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	supplierView, updateError := handler.service.UpdateSupplier(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), supplierId, request)
	if updateError != nil {
		return respondWithServiceError(c, updateError)
	}
	return response.Success(c, "Supplier saved", supplierView)
}

func (handler *Handler) DeactivateSupplier(c *fiber.Ctx) error {
	supplierId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrSupplierNotFound)
	}

	deactivateError := handler.service.DeactivateSupplier(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), supplierId)
	if deactivateError != nil {
		return respondWithServiceError(c, deactivateError)
	}
	return response.Success(c, "Supplier turned off", nil)
}

func (handler *Handler) Aging(c *fiber.Ctx) error {
	agingView, agingError := handler.service.Aging(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), c.Query("as_of"))
	if agingError != nil {
		return respondWithServiceError(c, agingError)
	}
	return response.Success(c, "What you owe suppliers", agingView)
}

func (handler *Handler) Statement(c *fiber.Ctx) error {
	supplierId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrSupplierNotFound)
	}

	if c.Query("format") != "" {
		statementFile, documentError := handler.service.StatementDocument(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), supplierId, StatementDocumentRequest{
			Format:   c.Query("format"),
			Language: c.Query("lang"),
			From:     c.Query("from"),
			To:       c.Query("to"),
		})
		if documentError != nil {
			return respondWithServiceError(c, documentError)
		}
		return httpx.SendFile(c, statementFile.Name, statementFile.ContentType, statementFile.Bytes)
	}
	statementView, statementError := handler.service.Statement(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), supplierId, c.Query("from"), c.Query("to"))
	if statementError != nil {
		return respondWithServiceError(c, statementError)
	}
	return response.Success(c, "Supplier statement", statementView)
}

func (handler *Handler) ListPurchases(c *fiber.Ctx) error {
	pagination := httpx.ParsePagination(c)
	documentFilter, isValidFilter := documentFilterFrom(c)
	if !isValidFilter {
		return respondWithServiceError(c, ErrInvalidFilter)
	}

	purchasePage, listError := handler.service.ListPurchases(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), documentFilter, pagination.Limit, pagination.Offset)
	if listError != nil {
		return respondWithServiceError(c, listError)
	}
	return response.Paged(c, "Stock arrived", purchasePage)
}

func (handler *Handler) GetPurchase(c *fiber.Ctx) error {
	purchaseId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrPurchaseNotFound)
	}

	purchaseView, getError := handler.service.GetPurchase(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), purchaseId)
	if getError != nil {
		return respondWithServiceError(c, getError)
	}
	return response.Success(c, "Stock arrival", purchaseView)
}

func (handler *Handler) RecordPurchase(c *fiber.Ctx) error {
	request := PurchaseRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	purchaseView, recordError := handler.service.RecordPurchase(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), request)
	if recordError != nil {
		return respondWithServiceError(c, recordError)
	}
	return response.Created(c, "Stock arrival saved", purchaseView)
}

func (handler *Handler) CancelPurchase(c *fiber.Ctx) error {
	purchaseId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrPurchaseNotFound)
	}
	request := ReasonRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	purchaseView, cancelError := handler.service.CancelPurchase(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), purchaseId, request)
	if cancelError != nil {
		return respondWithServiceError(c, cancelError)
	}
	return response.Success(c, "Stock arrival cancelled", purchaseView)
}

func (handler *Handler) AttachInvoicePhoto(c *fiber.Ctx) error {
	purchaseId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrPurchaseNotFound)
	}
	uploadedFile, formFileError := c.FormFile("image")
	if formFileError != nil {
		return response.Error(c, fiber.StatusBadRequest, "bad_request", "Attach the photo as a field named image")
	}
	if uploadedFile.Size > MaximumAttachmentBytes {
		return respondWithServiceError(c, media.ErrImageTooLarge)
	}
	openedFile, openError := uploadedFile.Open()
	if openError != nil {
		return response.Error(c, fiber.StatusBadRequest, "bad_request", "The photo could not be read")
	}
	defer openedFile.Close()
	imageBytes, readError := io.ReadAll(io.LimitReader(openedFile, MaximumAttachmentBytes+1))
	if readError != nil {
		return response.Error(c, fiber.StatusBadRequest, "bad_request", "The photo could not be read")
	}

	purchaseView, attachError := handler.service.AttachInvoicePhoto(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), purchaseId, imageBytes)
	if attachError != nil {
		return respondWithServiceError(c, attachError)
	}
	return response.Success(c, "Invoice photo saved", purchaseView)
}

func (handler *Handler) VatRate(c *fiber.Ctx) error {
	vatRateView, rateError := handler.service.VatRate(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c))
	if rateError != nil {
		return respondWithServiceError(c, rateError)
	}
	return response.Success(c, "VAT rate", vatRateView)
}

func (handler *Handler) LastCost(c *fiber.Ctx) error {
	productId, isValidProduct := optionalIdQuery(c, "product_id")
	supplierId, isValidSupplier := optionalIdQuery(c, "supplier_id")
	if productId == nil || !isValidProduct || !isValidSupplier {
		return respondWithServiceError(c, ErrInvalidFilter)
	}

	lastCostView, costError := handler.service.LastCost(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), *productId, supplierId)
	if costError != nil {
		return respondWithServiceError(c, costError)
	}
	return response.Success(c, "Last cost", lastCostView)
}

func (handler *Handler) ListPayments(c *fiber.Ctx) error {
	pagination := httpx.ParsePagination(c)
	supplierId, isValidSupplier := optionalIdQuery(c, "supplier_id")
	if !isValidSupplier {
		return respondWithServiceError(c, ErrInvalidFilter)
	}

	paymentPage, listError := handler.service.ListPayments(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), supplierId, pagination.Limit, pagination.Offset)
	if listError != nil {
		return respondWithServiceError(c, listError)
	}
	return response.Paged(c, "Supplier payments", paymentPage)
}

func (handler *Handler) RecordPayment(c *fiber.Ctx) error {
	request := PaymentRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	paymentView, recordError := handler.service.RecordPayment(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), request)
	if recordError != nil {
		return respondWithServiceError(c, recordError)
	}
	return response.Created(c, "Payment saved", paymentView)
}

func (handler *Handler) VoidPayment(c *fiber.Ctx) error {
	paymentId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrPaymentNotFound)
	}
	request := ReasonRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	paymentView, voidError := handler.service.VoidPayment(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), paymentId, request)
	if voidError != nil {
		return respondWithServiceError(c, voidError)
	}
	return response.Success(c, "Payment voided", paymentView)
}

func (handler *Handler) ListReturns(c *fiber.Ctx) error {
	pagination := httpx.ParsePagination(c)
	supplierId, isValidSupplier := optionalIdQuery(c, "supplier_id")
	if !isValidSupplier {
		return respondWithServiceError(c, ErrInvalidFilter)
	}

	returnPage, listError := handler.service.ListReturns(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), supplierId, pagination.Limit, pagination.Offset)
	if listError != nil {
		return respondWithServiceError(c, listError)
	}
	return response.Paged(c, "Returns to suppliers", returnPage)
}

func (handler *Handler) GetReturn(c *fiber.Ctx) error {
	returnId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrReturnNotFound)
	}

	returnView, getError := handler.service.GetReturn(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), returnId)
	if getError != nil {
		return respondWithServiceError(c, getError)
	}
	return response.Success(c, "Return to supplier", returnView)
}

func (handler *Handler) RecordReturn(c *fiber.Ctx) error {
	request := ReturnRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	returnView, recordError := handler.service.RecordReturn(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), request)
	if recordError != nil {
		return respondWithServiceError(c, recordError)
	}
	return response.Created(c, "Return saved", returnView)
}

func (handler *Handler) ListOrders(c *fiber.Ctx) error {
	pagination := httpx.ParsePagination(c)
	documentFilter, isValidFilter := documentFilterFrom(c)
	if !isValidFilter {
		return respondWithServiceError(c, ErrInvalidFilter)
	}

	orderPage, listError := handler.service.ListOrders(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), documentFilter, pagination.Limit, pagination.Offset)
	if listError != nil {
		return respondWithServiceError(c, listError)
	}
	return response.Paged(c, "Orders to suppliers", orderPage)
}

func (handler *Handler) GetOrder(c *fiber.Ctx) error {
	orderId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrOrderNotFound)
	}

	orderView, getError := handler.service.GetOrder(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), orderId)
	if getError != nil {
		return respondWithServiceError(c, getError)
	}
	return response.Success(c, "Order to supplier", orderView)
}

func (handler *Handler) CreateOrder(c *fiber.Ctx) error {
	request := OrderRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	orderView, createError := handler.service.CreateOrder(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), request)
	if createError != nil {
		return respondWithServiceError(c, createError)
	}
	return response.Created(c, "Order saved", orderView)
}

func (handler *Handler) SendOrder(c *fiber.Ctx) error {
	orderId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrOrderNotFound)
	}

	orderView, sendError := handler.service.SendOrder(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), orderId)
	if sendError != nil {
		return respondWithServiceError(c, sendError)
	}
	return response.Success(c, "Order marked as sent", orderView)
}

func (handler *Handler) CancelOrder(c *fiber.Ctx) error {
	orderId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrOrderNotFound)
	}
	request := ReasonRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	orderView, cancelError := handler.service.CancelOrder(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), orderId, request)
	if cancelError != nil {
		return respondWithServiceError(c, cancelError)
	}
	return response.Success(c, "Order cancelled", orderView)
}

type errorResponse struct {
	status int
	code   string
}

var errorResponses = map[error]errorResponse{
	documents.ErrUnknownFormat: {fiber.StatusBadRequest, "invalid_format"},
	ErrFeatureOff:              {fiber.StatusForbidden, "feature_off"},
	ErrNoActiveShop:            {fiber.StatusBadRequest, "no_active_shop"},
	ErrShopNotFound:            {fiber.StatusNotFound, "shop_not_found"},
	ErrShopNotAssigned:         {fiber.StatusForbidden, "shop_not_assigned"},
	ErrSupplierNotFound:        {fiber.StatusNotFound, "supplier_not_found"},
	ErrSupplierInactive:        {fiber.StatusBadRequest, "supplier_inactive"},
	ErrSupplierNameTaken:       {fiber.StatusConflict, "supplier_name_taken"},
	ErrNameRequired:            {fiber.StatusBadRequest, "supplier_name_required"},
	ErrInvalidPhone:            {fiber.StatusBadRequest, "invalid_phone"},
	ErrInvalidEmail:            {fiber.StatusBadRequest, "invalid_email"},
	ErrProductNotFound:         {fiber.StatusNotFound, "product_not_found"},
	ErrDuplicateProduct:        {fiber.StatusBadRequest, "duplicate_product"},
	ErrMustPayInFull:           {fiber.StatusBadRequest, "must_pay_in_full"},
	ErrPaidMoreThanTotal:       {fiber.StatusBadRequest, "paid_more_than_total"},
	ErrOverpayment:             {fiber.StatusBadRequest, "overpayment"},
	ErrPurchaseNotFound:        {fiber.StatusNotFound, "purchase_not_found"},
	ErrAlreadyCancelled:        {fiber.StatusConflict, "already_cancelled"},
	ErrStockAlreadyUsed:        {fiber.StatusConflict, "stock_already_used"},
	ErrPaymentNotFound:         {fiber.StatusNotFound, "payment_not_found"},
	ErrPaymentAlreadyVoided:    {fiber.StatusConflict, "payment_already_voided"},
	ErrPaymentLocked:           {fiber.StatusConflict, "payment_locked"},
	ErrReturnNotFound:          {fiber.StatusNotFound, "return_not_found"},
	ErrOrderNotFound:           {fiber.StatusNotFound, "order_not_found"},
	ErrOrderClosed:             {fiber.StatusConflict, "order_closed"},
	ErrOrderNotDraft:           {fiber.StatusConflict, "order_not_draft"},
	ErrOrderSupplierMismatch:   {fiber.StatusBadRequest, "order_supplier_mismatch"},
	ErrDateInFuture:            {fiber.StatusBadRequest, "date_in_future"},
	ErrInvalidDate:             {fiber.StatusBadRequest, "invalid_date"},
	ErrInvalidRange:            {fiber.StatusBadRequest, "invalid_range"},
	ErrInvalidFilter:           {fiber.StatusBadRequest, "invalid_filter"},
	ErrReasonRequired:          {fiber.StatusBadRequest, "reason_required"},
	ErrClientRefInFlight:       {fiber.StatusConflict, "client_ref_in_flight"},
	stock.ErrInsufficientStock: {fiber.StatusConflict, "insufficient_stock"},
	media.ErrEmptyImage:        {fiber.StatusBadRequest, "invalid_image"},
	media.ErrUnsupportedImage:  {fiber.StatusBadRequest, "invalid_image"},
	media.ErrImageTooLarge:     {fiber.StatusRequestEntityTooLarge, "image_too_large"},
}

func respondWithServiceError(c *fiber.Ctx, serviceError error) error {
	for knownError, knownResponse := range errorResponses {
		if errors.Is(serviceError, knownError) {
			return response.Error(c, knownResponse.status, knownResponse.code, serviceError.Error())
		}
	}
	return serviceError
}
