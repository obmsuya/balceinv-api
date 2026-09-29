package shops

import (
	"errors"

	"github.com/chrisostomemataba/balceinv-api/internal/common/httpx"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
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
	principal := httpx.CurrentPrincipal(c)

	shopPage, listError := handler.service.List(c.UserContext(), httpx.RequestQuerier(c), principal.CompanyId, pagination.Limit, pagination.Offset)
	if listError != nil {
		return respondWithServiceError(c, listError)
	}
	return response.Paged(c, "Shops", shopPage)
}

func (handler *Handler) Get(c *fiber.Ctx) error {
	shopId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrShopNotFound)
	}

	shopView, getError := handler.service.Get(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c).CompanyId, shopId)
	if getError != nil {
		return respondWithServiceError(c, getError)
	}
	return response.Success(c, "Shop", shopView)
}

func (handler *Handler) Create(c *fiber.Ctx) error {
	request := ShopRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	shopView, createError := handler.service.Create(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c).CompanyId, request)
	if createError != nil {
		return respondWithServiceError(c, createError)
	}
	return response.Created(c, "Shop created", shopView)
}

func (handler *Handler) Update(c *fiber.Ctx) error {
	shopId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrShopNotFound)
	}

	request := ShopRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	shopView, updateError := handler.service.Update(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c).CompanyId, shopId, request)
	if updateError != nil {
		return respondWithServiceError(c, updateError)
	}
	return response.Success(c, "Shop saved", shopView)
}

func (handler *Handler) Close(c *fiber.Ctx) error {
	shopId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrShopNotFound)
	}

	shopView, closeError := handler.service.Close(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c).CompanyId, shopId)
	if closeError != nil {
		return respondWithServiceError(c, closeError)
	}
	return response.Success(c, "Shop closed", shopView)
}

func respondWithServiceError(c *fiber.Ctx, serviceError error) error {
	switch {
	case errors.Is(serviceError, ErrShopNotFound):
		return response.Error(c, fiber.StatusNotFound, "not_found", serviceError.Error())
	case errors.Is(serviceError, ErrShopNameTaken):
		return response.Error(c, fiber.StatusConflict, "shop_name_taken", serviceError.Error())
	case errors.Is(serviceError, ErrLastActiveShop):
		return response.Error(c, fiber.StatusConflict, "last_active_shop", serviceError.Error())
	default:
		return serviceError
	}
}
