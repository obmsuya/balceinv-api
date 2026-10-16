package cleardata

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

func (handler *Handler) Clear(c *fiber.Ctx) error {
	principal := httpx.CurrentPrincipal(c)
	if !principal.IsOwner {
		return response.Error(c, fiber.StatusForbidden, "clear_owner_only", ErrOwnerOnly.Error())
	}

	request := ClearRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	clearSummary, clearError := handler.service.Clear(c.UserContext(), principal, request)
	if clearError != nil {
		return respondWithServiceError(c, clearError)
	}
	return response.Success(c, "Business data cleared", toView(clearSummary))
}

func respondWithServiceError(c *fiber.Ctx, serviceError error) error {
	switch {
	case errors.Is(serviceError, ErrOwnerOnly):
		return response.Error(c, fiber.StatusForbidden, "clear_owner_only", serviceError.Error())
	case errors.Is(serviceError, ErrNameMismatch):
		return response.Error(c, fiber.StatusUnprocessableEntity, "clear_name_mismatch", serviceError.Error())
	case errors.Is(serviceError, ErrWrongPassword):
		return response.Error(c, fiber.StatusUnprocessableEntity, "clear_wrong_password", serviceError.Error())
	case errors.Is(serviceError, ErrNoShopToKeep):
		return response.Error(c, fiber.StatusConflict, "clear_no_shop", serviceError.Error())
	default:
		return serviceError
	}
}
