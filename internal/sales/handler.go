package sales

import (
	"errors"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/httpx"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/chrisostomemataba/balceinv-api/internal/customers"
	"github.com/chrisostomemataba/balceinv-api/internal/stock"
	"github.com/gofiber/fiber/v2"
)

var ErrInvalidFilter = errors.New("the filter is not valid")

type Handler struct {
	service       *Service
	fiscalService *FiscalService
}

func NewHandler(service *Service, fiscalService *FiscalService) *Handler {
	return &Handler{
		service:       service,
		fiscalService: fiscalService,
	}
}

func (handler *Handler) Quote(c *fiber.Ctx) error {
	request := QuoteRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	quoteView, quoteError := handler.service.Quote(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), request)
	if quoteError != nil {
		return respondWithServiceError(c, quoteError)
	}
	return response.Success(c, "Quote", quoteView)
}

func (handler *Handler) TillOptions(c *fiber.Ctx) error {
	principal := httpx.CurrentPrincipal(c)
	tillOptions, optionsError := handler.service.TillOptions(c.UserContext(), httpx.RequestQuerier(c), principal.CompanyId)
	if optionsError != nil {
		return respondWithServiceError(c, optionsError)
	}
	return response.Success(c, "Till options", tillOptions)
}

func (handler *Handler) SendToEfd(c *fiber.Ctx) error {
	saleId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrSaleNotFound)
	}

	fiscalView, sendError := handler.fiscalService.Send(c.UserContext(), httpx.CurrentPrincipal(c).CompanyId, saleId)
	if sendError != nil {
		return respondWithServiceError(c, sendError)
	}
	return response.Success(c, "EFD status", fiscalView)
}

func (handler *Handler) SendWaitingToEfd(c *fiber.Ctx) error {
	sendSummary, sendError := handler.fiscalService.SendWaiting(c.UserContext(), httpx.CurrentPrincipal(c).CompanyId)
	if sendError != nil {
		return respondWithServiceError(c, sendError)
	}
	return response.Success(c, "EFD sending finished", sendSummary)
}

func (handler *Handler) Create(c *fiber.Ctx) error {
	request := SaleRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	saleView, createError := handler.service.Create(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), request)
	if createError != nil {
		return respondWithServiceError(c, createError)
	}
	return response.Created(c, "Sale complete", saleView)
}

func (handler *Handler) List(c *fiber.Ctx) error {
	pagination := httpx.ParsePagination(c)
	saleFilter, isValidFilter := parseSaleFilter(c)
	if !isValidFilter {
		return respondWithServiceError(c, ErrInvalidFilter)
	}

	salePage, listError := handler.service.List(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), saleFilter, pagination.Limit, pagination.Offset)
	if listError != nil {
		return respondWithServiceError(c, listError)
	}
	return response.Paged(c, "Sales", salePage)
}

func (handler *Handler) Totals(c *fiber.Ctx) error {
	saleFilter, isValidFilter := parseSaleFilter(c)
	if !isValidFilter {
		return respondWithServiceError(c, ErrInvalidFilter)
	}

	totals, totalsError := handler.service.Totals(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), saleFilter)
	if totalsError != nil {
		return respondWithServiceError(c, totalsError)
	}
	return response.Success(c, "Sales totals", totals)
}

func (handler *Handler) Get(c *fiber.Ctx) error {
	saleId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrSaleNotFound)
	}

	saleView, getError := handler.service.Get(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c).CompanyId, saleId)
	if getError != nil {
		return respondWithServiceError(c, getError)
	}
	return response.Success(c, "Sale", saleView)
}

func (handler *Handler) Void(c *fiber.Ctx) error {
	saleId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrSaleNotFound)
	}
	request := VoidRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	saleView, voidError := handler.service.Void(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), saleId, request)
	if voidError != nil {
		return respondWithServiceError(c, voidError)
	}
	return response.Success(c, "Sale voided", saleView)
}

func (handler *Handler) Refund(c *fiber.Ctx) error {
	saleId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrSaleNotFound)
	}
	request := RefundRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	saleView, isNew, refundError := handler.service.Refund(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), saleId, request)
	if refundError != nil {
		return respondWithServiceError(c, refundError)
	}
	if !isNew {
		return response.Success(c, "Refund already recorded", saleView)
	}
	return response.Created(c, "Refund recorded", saleView)
}

func (handler *Handler) Receipt(c *fiber.Ctx) error {
	saleId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrSaleNotFound)
	}

	receiptView, receiptError := handler.service.Receipt(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c).CompanyId, saleId)
	if receiptError != nil {
		return respondWithServiceError(c, receiptError)
	}
	return response.Success(c, "Receipt", receiptView)
}

func parseSaleFilter(c *fiber.Ctx) (SaleFilter, bool) {
	fiscalFilter := c.Query("fiscal")
	if fiscalFilter != "" && fiscalFilter != "waiting" {
		return SaleFilter{}, false
	}
	saleFilter := SaleFilter{
		SearchText:    c.Query("q"),
		FiscalWaiting: fiscalFilter == "waiting",
	}

	for _, rawTime := range []struct {
		value       string
		destination **time.Time
	}{
		{c.Query("from"), &saleFilter.From},
		{c.Query("to"), &saleFilter.To},
	} {
		if rawTime.value == "" {
			continue
		}
		parsedTime, parseError := time.Parse(time.RFC3339, rawTime.value)
		if parseError != nil {
			return SaleFilter{}, false
		}
		utcTime := parsedTime.UTC()
		*rawTime.destination = &utcTime
	}

	return saleFilter, true
}

func respondWithServiceError(c *fiber.Ctx, serviceError error) error {
	switch {
	case errors.Is(serviceError, ErrSaleNotFound), errors.Is(serviceError, ErrProductNotFound), errors.Is(serviceError, ErrAddonNotFound):
		return response.Error(c, fiber.StatusNotFound, "not_found", serviceError.Error())
	case errors.Is(serviceError, ErrClientRefReused):
		return response.Error(c, fiber.StatusConflict, "client_ref_reused", serviceError.Error())
	case errors.Is(serviceError, ErrClientRefInFlight):
		return response.Error(c, fiber.StatusConflict, "client_ref_in_flight", serviceError.Error())
	case errors.Is(serviceError, ErrShopClosed):
		return response.Error(c, fiber.StatusConflict, "shop_closed", serviceError.Error())
	case errors.Is(serviceError, ErrDuplicatePayment), errors.Is(serviceError, ErrPaymentTooLow), errors.Is(serviceError, ErrChangeWithoutCash):
		return response.Error(c, fiber.StatusBadRequest, "invalid_payment", serviceError.Error())
	case errors.Is(serviceError, ErrSaleVoidedNoRefund), errors.Is(serviceError, ErrSaleHasRefunds):
		return response.Error(c, fiber.StatusConflict, "refund_not_possible", serviceError.Error())
	case errors.Is(serviceError, ErrRefundLineUnknown), errors.Is(serviceError, ErrRefundTooMany):
		return response.Error(c, fiber.StatusBadRequest, "refund_too_many", serviceError.Error())
	case errors.Is(serviceError, ErrRefundCreditTooMuch), errors.Is(serviceError, ErrRefundCreditNoDebt):
		return response.Error(c, fiber.StatusBadRequest, "refund_credit_not_possible", serviceError.Error())
	case errors.Is(serviceError, ErrRefundRefReused):
		return response.Error(c, fiber.StatusConflict, "client_ref_reused", serviceError.Error())
	case errors.Is(serviceError, ErrSaleAlreadyVoided):
		return response.Error(c, fiber.StatusConflict, "sale_already_voided", serviceError.Error())
	case errors.Is(serviceError, ErrSaleFromOrder):
		return response.Error(c, fiber.StatusConflict, "sale_from_order", serviceError.Error())
	case errors.Is(serviceError, ErrFiscalBusy):
		return response.Error(c, fiber.StatusConflict, "efd_busy", serviceError.Error())
	case errors.Is(serviceError, ErrDiscountNotAllowed):
		return response.Error(c, fiber.StatusForbidden, "till_discount_not_allowed", serviceError.Error())
	case errors.Is(serviceError, ErrNoActiveShop):
		return response.Error(c, fiber.StatusBadRequest, "no_active_shop", serviceError.Error())
	case errors.Is(serviceError, ErrEfdOff), errors.Is(serviceError, ErrEfdNotReady), errors.Is(serviceError, ErrFiscalNotQueued):
		return response.Error(c, fiber.StatusConflict, "efd_unavailable", serviceError.Error())
	case errors.Is(serviceError, ErrInvalidFilter):
		return response.Error(c, fiber.StatusBadRequest, "invalid_filter", serviceError.Error())
	case errors.Is(serviceError, ErrInvalidCustomerId):
		return response.Error(c, fiber.StatusNotFound, "not_found", serviceError.Error())
	case errors.Is(serviceError, customers.ErrFeatureOff), errors.Is(serviceError, customers.ErrCustomerNotFound),
		errors.Is(serviceError, customers.ErrCustomerRequired), errors.Is(serviceError, customers.ErrCustomerInactive),
		errors.Is(serviceError, customers.ErrCreditLimitExceeded):
		return customers.RespondWithServiceError(c, serviceError)
	default:
		return stock.RespondWithServiceError(c, serviceError)
	}
}
