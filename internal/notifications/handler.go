package notifications

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
	unreadOnly := c.QueryBool("unread", false)

	notificationPage, listError := handler.service.List(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), unreadOnly, pagination.Limit, pagination.Offset)
	if listError != nil {
		return respondWithServiceError(c, listError)
	}
	return response.Paged(c, "Notifications", notificationPage)
}

func (handler *Handler) UnreadCount(c *fiber.Ctx) error {
	unreadCount, countError := handler.service.UnreadCount(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c))
	if countError != nil {
		return respondWithServiceError(c, countError)
	}
	return response.Success(c, "Unread notifications", unreadCount)
}

func (handler *Handler) MarkRead(c *fiber.Ctx) error {
	notificationId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrNotificationNotFound)
	}

	changedCount, markError := handler.service.MarkRead(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), notificationId)
	if markError != nil {
		return respondWithServiceError(c, markError)
	}
	return response.Success(c, "Marked as read", changedCount)
}

func (handler *Handler) MarkAllRead(c *fiber.Ctx) error {
	changedCount, markError := handler.service.MarkAllRead(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c))
	if markError != nil {
		return respondWithServiceError(c, markError)
	}
	return response.Success(c, "All marked as read", changedCount)
}

func (handler *Handler) ClearRead(c *fiber.Ctx) error {
	removedCount, clearError := handler.service.ClearRead(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c))
	if clearError != nil {
		return respondWithServiceError(c, clearError)
	}
	return response.Success(c, "Read notifications cleared", removedCount)
}

func respondWithServiceError(c *fiber.Ctx, serviceError error) error {
	switch {
	case errors.Is(serviceError, ErrNotificationNotFound):
		return response.Error(c, fiber.StatusNotFound, "not_found", serviceError.Error())
	case errors.Is(serviceError, ErrNoActiveShop):
		return response.Error(c, fiber.StatusBadRequest, "no_active_shop", serviceError.Error())
	default:
		return serviceError
	}
}
