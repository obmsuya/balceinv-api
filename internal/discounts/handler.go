package discounts

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

	discountPage, listError := handler.service.List(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c).CompanyId, pagination.Limit, pagination.Offset)
	if listError != nil {
		return respondWithServiceError(c, listError)
	}
	return response.Paged(c, "Discounts", discountPage)
}

func (handler *Handler) Get(c *fiber.Ctx) error {
	discountId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrDiscountNotFound)
	}

	discountView, getError := handler.service.Get(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c).CompanyId, discountId)
	if getError != nil {
		return respondWithServiceError(c, getError)
	}
	return response.Success(c, "Discount", discountView)
}

func (handler *Handler) Create(c *fiber.Ctx) error {
	request := DiscountRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	discountView, createError := handler.service.Create(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), request)
	if createError != nil {
		return respondWithServiceError(c, createError)
	}
	return response.Created(c, "Discount created", discountView)
}

func (handler *Handler) Update(c *fiber.Ctx) error {
	discountId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrDiscountNotFound)
	}

	request := DiscountRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	discountView, updateError := handler.service.Update(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), discountId, request)
	if updateError != nil {
		return respondWithServiceError(c, updateError)
	}
	return response.Success(c, "Discount saved", discountView)
}

func (handler *Handler) Stop(c *fiber.Ctx) error {
	discountId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrDiscountNotFound)
	}

	discountView, stopError := handler.service.Stop(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), discountId)
	if stopError != nil {
		return respondWithServiceError(c, stopError)
	}
	return response.Success(c, "Discount stopped", discountView)
}

func respondWithServiceError(c *fiber.Ctx, serviceError error) error {
	switch {
	case errors.Is(serviceError, ErrDiscountNotFound), errors.Is(serviceError, ErrProductNotFound):
		return response.Error(c, fiber.StatusNotFound, "not_found", serviceError.Error())
	case errors.Is(serviceError, ErrPercentTooLarge), errors.Is(serviceError, ErrEndsBeforeStart):
		return response.Error(c, fiber.StatusBadRequest, "invalid_discount", serviceError.Error())
	default:
		return serviceError
	}
}
