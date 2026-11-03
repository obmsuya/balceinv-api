package passwordreset

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
	return &Handler{service: service}
}

func (handler *Handler) SendCode(c *fiber.Ctx) error {
	request := CodeRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	sendError := handler.service.SendCode(c.UserContext(), request.Email)
	if sendError != nil {
		return sendError
	}
	return response.Success(c, "If that email has an account, a reset code is on its way", nil)
}

func (handler *Handler) Reset(c *fiber.Ctx) error {
	request := ResetRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	resetError := handler.service.Reset(c.UserContext(), request)
	switch {
	case errors.Is(resetError, ErrInvalidCode):
		return response.Error(c, fiber.StatusBadRequest, "invalid_reset_code", resetError.Error())
	case errors.Is(resetError, ErrPasswordTooLong):
		return response.Error(c, fiber.StatusBadRequest, "validation_failed", resetError.Error())
	case resetError != nil:
		return resetError
	}
	return response.Success(c, "Password changed. Sign in with the new password", nil)
}
