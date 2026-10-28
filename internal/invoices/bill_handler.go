package invoices

import (
	"context"
	"errors"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/identity"

	"github.com/chrisostomemataba/balceinv-api/internal/common/httpx"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/chrisostomemataba/balceinv-api/internal/common/shopscope"
	"github.com/chrisostomemataba/balceinv-api/internal/customers"
	"github.com/chrisostomemataba/balceinv-api/internal/documents"
	"github.com/chrisostomemataba/balceinv-api/internal/stock"
	"github.com/gofiber/fiber/v2"
	"github.com/google/uuid"
)

type BillHandler struct {
	service *BillService
}

func NewBillHandler(service *BillService) *BillHandler {
	return &BillHandler{
		service: service,
	}
}

func (handler *BillHandler) List(c *fiber.Ctx) error {
	pagination := httpx.ParsePagination(c)
	filter := Filter{
		Kind:          c.Query("kind"),
		Status:        c.Query("status"),
		PaymentStatus: c.Query("payment"),
		SearchText:    c.Query("q"),
	}
	for queryName, target := range map[string]**uuid.UUID{"customer_id": &filter.CustomerId, "supplier_id": &filter.SupplierId} {
		if c.Query(queryName) == "" {
			continue
		}
		partyId, parseError := uuid.Parse(c.Query(queryName))
		if parseError != nil {
			return respondWithBillError(c, ErrInvalidFilter)
		}
		*target = &partyId
	}

	invoicePage, listError := handler.service.List(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), c.Query("shop"), filter, pagination.Limit, pagination.Offset)
	if listError != nil {
		return respondWithBillError(c, listError)
	}
	return response.Paged(c, "Invoices", invoicePage)
}

func (handler *BillHandler) Summary(c *fiber.Ctx) error {
	summary, summaryError := handler.service.Summary(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), c.Query("shop"))
	if summaryError != nil {
		return respondWithBillError(c, summaryError)
	}
	return response.Success(c, "Invoice summary", summary)
}

func (handler *BillHandler) Get(c *fiber.Ctx) error {
	invoiceId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithBillError(c, ErrInvoiceNotFound)
	}
	invoiceView, getError := handler.service.Get(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), invoiceId)
	if getError != nil {
		return respondWithBillError(c, getError)
	}
	return response.Success(c, "Invoice", invoiceView)
}

func (handler *BillHandler) Create(c *fiber.Ctx) error {
	request := InvoiceRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}
	invoiceView, createError := handler.service.Create(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), request)
	if createError != nil {
		return respondWithBillError(c, createError)
	}
	return response.Created(c, "Invoice saved", invoiceView)
}

func (handler *BillHandler) Update(c *fiber.Ctx) error {
	invoiceId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithBillError(c, ErrInvoiceNotFound)
	}
	request := InvoiceRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}
	invoiceView, updateError := handler.service.Update(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), invoiceId, request)
	if updateError != nil {
		return respondWithBillError(c, updateError)
	}
	return response.Success(c, "Invoice saved", invoiceView)
}

func (handler *BillHandler) Issue(c *fiber.Ctx) error {
	return handler.change(c, "Invoice issued", handler.service.Issue)
}

func (handler *BillHandler) Accept(c *fiber.Ctx) error {
	return handler.change(c, "Quotation accepted", handler.service.Accept)
}

func (handler *BillHandler) Deliver(c *fiber.Ctx) error {
	return handler.change(c, "Invoice delivered", handler.service.Deliver)
}

func (handler *BillHandler) RecordPayment(c *fiber.Ctx) error {
	invoiceId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithBillError(c, ErrInvoiceNotFound)
	}
	request := PaymentRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}
	invoiceView, paymentError := handler.service.RecordPayment(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), invoiceId, request)
	if paymentError != nil {
		return respondWithBillError(c, paymentError)
	}
	return response.Success(c, "Payment recorded", invoiceView)
}

func (handler *BillHandler) VoidPayment(c *fiber.Ctx) error {
	invoiceId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithBillError(c, ErrInvoiceNotFound)
	}
	paymentId, isValidPaymentId := httpx.UuidParam(c, "paymentId")
	if !isValidPaymentId {
		return respondWithBillError(c, ErrPaymentNotFound)
	}
	request := ReasonRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}
	invoiceView, voidError := handler.service.VoidPayment(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), invoiceId, paymentId, request)
	if voidError != nil {
		return respondWithBillError(c, voidError)
	}
	return response.Success(c, "Payment cancelled", invoiceView)
}

func (handler *BillHandler) Convert(c *fiber.Ctx) error {
	invoiceId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithBillError(c, ErrInvoiceNotFound)
	}
	request := ConvertRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}
	invoiceView, convertError := handler.service.Convert(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), invoiceId, request)
	if convertError != nil {
		return respondWithBillError(c, convertError)
	}
	return response.Created(c, "Document made", invoiceView)
}

func (handler *BillHandler) Cancel(c *fiber.Ctx) error {
	invoiceId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithBillError(c, ErrInvoiceNotFound)
	}
	request := ReasonRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}
	invoiceView, cancelError := handler.service.Cancel(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), invoiceId, request)
	if cancelError != nil {
		return respondWithBillError(c, cancelError)
	}
	return response.Success(c, "Invoice cancelled", invoiceView)
}

func (handler *BillHandler) Document(c *fiber.Ctx) error {
	invoiceId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithBillError(c, ErrInvoiceNotFound)
	}
	documentRequest := DocumentRequest{Format: c.Query("format"), Language: c.Query("lang")}
	invoiceFile, documentError := handler.service.Document(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), invoiceId, documentRequest)
	if documentError != nil {
		return respondWithBillError(c, documentError)
	}
	return httpx.SendFile(c, invoiceFile.Name, invoiceFile.ContentType, invoiceFile.Bytes)
}

func (handler *BillHandler) change(c *fiber.Ctx, message string, changeInvoice func(ctx context.Context, querier database.Querier, principal *identity.Principal, invoiceId uuid.UUID) (InvoiceView, error)) error {
	invoiceId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithBillError(c, ErrInvoiceNotFound)
	}
	invoiceView, changeError := changeInvoice(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), invoiceId)
	if changeError != nil {
		return respondWithBillError(c, changeError)
	}
	return response.Success(c, message, invoiceView)
}

var billErrors = map[error]struct {
	status int
	code   string
}{
	ErrInvoiceNotFound:         {fiber.StatusNotFound, "not_found"},
	ErrPaymentNotFound:         {fiber.StatusNotFound, "not_found"},
	ErrProductNotFound:         {fiber.StatusNotFound, "not_found"},
	ErrNoActiveShop:            {fiber.StatusBadRequest, "no_active_shop"},
	ErrInvoicesOff:             {fiber.StatusForbidden, "feature_off"},
	ErrCustomersOff:            {fiber.StatusForbidden, "feature_off"},
	ErrSuppliersOff:            {fiber.StatusForbidden, "feature_off"},
	ErrPartyRequired:           {fiber.StatusBadRequest, "party_required"},
	ErrDescriptionRequired:     {fiber.StatusBadRequest, "line_description_required"},
	ErrPriceRequired:           {fiber.StatusBadRequest, "line_price_required"},
	ErrDiscountTooHigh:         {fiber.StatusBadRequest, "discount_too_high"},
	ErrInvalidDate:             {fiber.StatusBadRequest, "invalid_date"},
	ErrDueBeforeIssue:          {fiber.StatusBadRequest, "due_before_issue"},
	ErrNotDraft:                {fiber.StatusConflict, "not_draft"},
	ErrInvalidStatus:           {fiber.StatusConflict, "invalid_status"},
	ErrNotABill:                {fiber.StatusConflict, "not_a_bill"},
	ErrAlreadyDelivered:        {fiber.StatusConflict, "already_delivered"},
	ErrPaymentTooHigh:          {fiber.StatusBadRequest, "payment_too_high"},
	ErrWrongMethod:             {fiber.StatusBadRequest, "invalid_payment"},
	ErrPaymentAlreadyVoided:    {fiber.StatusConflict, "already_voided"},
	ErrHasPayments:             {fiber.StatusConflict, "has_payments"},
	ErrInvalidReason:           {fiber.StatusBadRequest, "validation_failed"},
	ErrCannotConvert:           {fiber.StatusConflict, "cannot_convert"},
	ErrNumberInFlight:          {fiber.StatusConflict, "try_again"},
	ErrInvalidFilter:           {fiber.StatusBadRequest, "invalid_filter"},
	documents.ErrUnknownFormat: {fiber.StatusBadRequest, "invalid_format"},
}

func respondWithBillError(c *fiber.Ctx, serviceError error) error {
	for knownError, mapping := range billErrors {
		if errors.Is(serviceError, knownError) {
			return response.Error(c, mapping.status, mapping.code, serviceError.Error())
		}
	}
	switch {
	case errors.Is(serviceError, shopscope.ErrShopNotAssigned):
		return response.Error(c, fiber.StatusForbidden, "shop_not_assigned", serviceError.Error())
	case errors.Is(serviceError, shopscope.ErrShopNotFound):
		return response.Error(c, fiber.StatusNotFound, "not_found", serviceError.Error())
	case errors.Is(serviceError, shopscope.ErrNoActiveShop):
		return response.Error(c, fiber.StatusBadRequest, "no_active_shop", serviceError.Error())
	case errors.Is(serviceError, stock.ErrInsufficientStock):
		return stock.RespondWithServiceError(c, serviceError)
	default:
		return customers.RespondWithServiceError(c, serviceError)
	}
}
