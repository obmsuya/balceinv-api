package orders

import (
	"errors"

	"github.com/chrisostomemataba/balceinv-api/internal/common/httpx"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/chrisostomemataba/balceinv-api/internal/customers"
	"github.com/chrisostomemataba/balceinv-api/internal/sales"
	"github.com/chrisostomemataba/balceinv-api/internal/stock"
	"github.com/gofiber/fiber/v2"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{
		service: service,
	}
}

func (handler *Handler) List(c *fiber.Ctx) error {
	pagination := httpx.ParsePagination(c)
	orderFilter := OrderFilter{
		Status:     c.Query("status"),
		SearchText: c.Query("q"),
	}

	orderPage, listError := handler.service.List(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), orderFilter, pagination.Limit, pagination.Offset)
	if listError != nil {
		return respondWithServiceError(c, listError)
	}
	return response.Paged(c, "Orders", orderPage)
}

func (handler *Handler) Get(c *fiber.Ctx) error {
	orderId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrOrderNotFound)
	}

	orderView, getError := handler.service.Get(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), orderId)
	if getError != nil {
		return respondWithServiceError(c, getError)
	}
	return response.Success(c, "Order", orderView)
}

func (handler *Handler) Create(c *fiber.Ctx) error {
	request := OrderRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	orderView, createError := handler.service.Create(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), request)
	if createError != nil {
		return respondWithServiceError(c, createError)
	}
	return response.Created(c, "Order saved", orderView)
}

func (handler *Handler) AddDeposit(c *fiber.Ctx) error {
	orderId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrOrderNotFound)
	}

	request := MoneyRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	orderView, depositError := handler.service.AddDeposit(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), orderId, request)
	if depositError != nil {
		return respondWithServiceError(c, depositError)
	}
	return response.Success(c, "Deposit recorded", orderView)
}

func (handler *Handler) MarkReady(c *fiber.Ctx) error {
	orderId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrOrderNotFound)
	}

	orderView, readyError := handler.service.MarkReady(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), orderId)
	if readyError != nil {
		return respondWithServiceError(c, readyError)
	}
	return response.Success(c, "Order ready", orderView)
}

func (handler *Handler) Collect(c *fiber.Ctx) error {
	orderId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrOrderNotFound)
	}

	request := CollectRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	orderView, collectError := handler.service.Collect(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), orderId, request)
	if collectError != nil {
		return respondWithServiceError(c, collectError)
	}
	return response.Success(c, "Order collected", orderView)
}

func (handler *Handler) Cancel(c *fiber.Ctx) error {
	orderId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrOrderNotFound)
	}

	request := CancelRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	orderView, cancelError := handler.service.Cancel(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), orderId, request)
	if cancelError != nil {
		return respondWithServiceError(c, cancelError)
	}
	return response.Success(c, "Order cancelled", orderView)
}

func respondWithServiceError(c *fiber.Ctx, serviceError error) error {
	switch {
	case errors.Is(serviceError, ErrOrderNotFound), errors.Is(serviceError, sales.ErrProductNotFound):
		return response.Error(c, fiber.StatusNotFound, "not_found", serviceError.Error())
	case errors.Is(serviceError, ErrNoActiveShop), errors.Is(serviceError, sales.ErrNoActiveShop):
		return response.Error(c, fiber.StatusBadRequest, "no_active_shop", serviceError.Error())
	case errors.Is(serviceError, ErrInvalidStatus):
		return response.Error(c, fiber.StatusConflict, "invalid_status", serviceError.Error())
	case errors.Is(serviceError, ErrDepositTooHigh):
		return response.Error(c, fiber.StatusBadRequest, "deposit_too_high", serviceError.Error())
	case errors.Is(serviceError, ErrRefundMethodRequired):
		return response.Error(c, fiber.StatusBadRequest, "refund_method_required", serviceError.Error())
	case errors.Is(serviceError, ErrInvalidDueDate), errors.Is(serviceError, ErrInvalidReason):
		return response.Error(c, fiber.StatusBadRequest, "validation_failed", serviceError.Error())
	case errors.Is(serviceError, ErrInvalidStatusFilter):
		return response.Error(c, fiber.StatusBadRequest, "invalid_filter", serviceError.Error())
	case errors.Is(serviceError, ErrOrderNumberInFlight), errors.Is(serviceError, sales.ErrClientRefInFlight):
		return response.Error(c, fiber.StatusConflict, "try_again", serviceError.Error())
	case errors.Is(serviceError, ErrCreditNeedsSalesRight):
		return response.Error(c, fiber.StatusForbidden, "forbidden", serviceError.Error())
	case errors.Is(serviceError, sales.ErrDuplicatePayment), errors.Is(serviceError, sales.ErrPaymentTooLow), errors.Is(serviceError, sales.ErrChangeWithoutCash):
		return response.Error(c, fiber.StatusBadRequest, "invalid_payment", serviceError.Error())
	case errors.Is(serviceError, sales.ErrShopClosed):
		return response.Error(c, fiber.StatusConflict, "shop_closed", serviceError.Error())
	case errors.Is(serviceError, stock.ErrInsufficientStock):
		return stock.RespondWithServiceError(c, serviceError)
	default:
		return customers.RespondWithServiceError(c, serviceError)
	}
}
