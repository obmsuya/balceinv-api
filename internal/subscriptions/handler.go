package subscriptions

import (
	"errors"
	"strings"

	"github.com/chrisostomemataba/balceinv-api/internal/common/httpx"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/chrisostomemataba/balceinv-api/internal/licensing"
	"github.com/chrisostomemataba/balceinv-api/license"
	"github.com/gofiber/fiber/v2"
)

const subscriptionEndedMessage = "Your subscription has ended. Renew it to continue."
const noInternetMessage = "No internet connection. Check the connection and try again."

var ungatedPathPrefixes = []string{"/api/auth/"}

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (handler *Handler) Gate() fiber.Handler {
	return func(c *fiber.Ctx) error {
		principal := httpx.CurrentPrincipal(c)
		if principal == nil {
			return c.Next()
		}
		requestPath := c.Path()
		if licensing.IsSkippedPath(requestPath) {
			return c.Next()
		}
		for _, ungatedPrefix := range ungatedPathPrefixes {
			if strings.HasPrefix(requestPath, ungatedPrefix) {
				return c.Next()
			}
		}

		subscriptionStatus, statusError := handler.service.Status(c.UserContext(), httpx.RequestQuerier(c), principal.CompanyId)
		if statusError != nil {
			return statusError
		}
		if !subscriptionStatus.Licensed {
			return licensing.PaymentRequired(c, subscriptionEndedMessage)
		}
		return c.Next()
	}
}

func (handler *Handler) Status(c *fiber.Ctx) error {
	subscriptionStatus, statusError := handler.service.Status(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c).CompanyId)
	if statusError != nil {
		return statusError
	}
	return response.Success(c, "License status", subscriptionStatus)
}

func (handler *Handler) Refresh(c *fiber.Ctx) error {
	subscriptionStatus, refreshError := handler.service.Refresh(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c).CompanyId)
	if errors.Is(refreshError, license.ErrLicensingServerUnreachable) {
		return c.Status(fiber.StatusServiceUnavailable).JSON(fiber.Map{"success": false, "error": noInternetMessage, "message": noInternetMessage})
	}
	if refreshError != nil {
		return refreshError
	}
	return response.Success(c, "License status", subscriptionStatus)
}

func (handler *Handler) DeviceId(c *fiber.Ctx) error {
	return c.JSON(fiber.Map{"success": true, "hardware_id": DeviceIdFor(httpx.CurrentPrincipal(c).CompanyId)})
}

func (handler *Handler) Pay(c *fiber.Ctx) error {
	deviceId := DeviceIdFor(httpx.CurrentPrincipal(c).CompanyId)
	return licensing.PayFor(c, func() (string, error) { return deviceId, nil })
}
