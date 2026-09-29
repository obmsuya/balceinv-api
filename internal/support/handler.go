package support

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

func (handler *Handler) Submit(c *fiber.Ctx) error {
	request := SubmitRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	submitView, submitError := handler.service.Submit(c.UserContext(), httpx.CurrentPrincipal(c), request)
	if submitError != nil {
		return respondWithServiceError(c, submitError)
	}
	return response.Created(c, "Support message saved", submitView)
}

func (handler *Handler) Messages(c *fiber.Ctx) error {
	messageViews, listError := handler.service.ListMine(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c))
	if listError != nil {
		return listError
	}
	return response.Success(c, "Support messages", messageViews)
}

func (handler *Handler) Status(c *fiber.Ctx) error {
	return response.Success(c, "Support status", handler.service.Status())
}

func respondWithServiceError(c *fiber.Ctx, serviceError error) error {
	switch {
	case errors.Is(serviceError, ErrMessageLength):
		return response.Error(c, fiber.StatusBadRequest, "message_length", serviceError.Error())
	case errors.Is(serviceError, ErrContactRequired):
		return response.Error(c, fiber.StatusBadRequest, "contact_required", serviceError.Error())
	case errors.Is(serviceError, ErrInvalidPhone):
		return response.Error(c, fiber.StatusBadRequest, "invalid_phone", serviceError.Error())
	case errors.Is(serviceError, ErrInvalidScreenshot):
		return response.Error(c, fiber.StatusBadRequest, "invalid_image", serviceError.Error())
	case errors.Is(serviceError, ErrScreenshotTooLarge):
		return response.Error(c, fiber.StatusBadRequest, "image_too_large", serviceError.Error())
	case errors.Is(serviceError, ErrRateLimited):
		return response.Error(c, fiber.StatusTooManyRequests, "rate_limited", serviceError.Error())
	case errors.Is(serviceError, ErrUserNotFound):
		return response.Error(c, fiber.StatusUnauthorized, "unauthenticated", "Please sign in")
	default:
		return serviceError
	}
}
