package transfers

import (
	"errors"

	"github.com/chrisostomemataba/balceinv-api/internal/common/httpx"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
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

	transferPage, listError := handler.service.List(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), pagination.Limit, pagination.Offset)
	if listError != nil {
		return respondWithServiceError(c, listError)
	}
	return response.Paged(c, "Transfers", transferPage)
}

func (handler *Handler) Get(c *fiber.Ctx) error {
	transferId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrTransferNotFound)
	}

	transferView, getError := handler.service.Get(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), transferId)
	if getError != nil {
		return respondWithServiceError(c, getError)
	}
	return response.Success(c, "Transfer", transferView)
}

func (handler *Handler) Create(c *fiber.Ctx) error {
	request := TransferRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	transferView, createError := handler.service.Create(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), request)
	if createError != nil {
		return respondWithServiceError(c, createError)
	}
	return response.Created(c, "Stock sent", transferView)
}

func respondWithServiceError(c *fiber.Ctx, serviceError error) error {
	switch {
	case errors.Is(serviceError, ErrTransferNotFound), errors.Is(serviceError, ErrShopNotFound), errors.Is(serviceError, ErrProductNotFound):
		return response.Error(c, fiber.StatusNotFound, "not_found", serviceError.Error())
	case errors.Is(serviceError, ErrSameShop), errors.Is(serviceError, ErrDuplicateProduct):
		return response.Error(c, fiber.StatusBadRequest, "invalid_transfer", serviceError.Error())
	case errors.Is(serviceError, ErrShopNotAssigned):
		return response.Error(c, fiber.StatusForbidden, "shop_not_assigned", serviceError.Error())
	case errors.Is(serviceError, ErrNoActiveShop):
		return response.Error(c, fiber.StatusBadRequest, "no_active_shop", serviceError.Error())
	default:
		return stock.RespondWithServiceError(c, serviceError)
	}
}
