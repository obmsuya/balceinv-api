package handlers

import (
	"strconv"

	"github.com/chrisostomemataba/balceinv-api/services"
	"github.com/chrisostomemataba/balceinv-api/utils"
	"github.com/gofiber/fiber/v2"
)

type NotificationHandler struct {
	service *services.NotificationService
}

func NewNotificationHandler(service *services.NotificationService) *NotificationHandler {
	return &NotificationHandler{service: service}
}

func (h *NotificationHandler) GetAll(c *fiber.Ctx) error {
	// includeSeen defaults to false — the UI normally only wants unread alerts
	includeSeen := c.Query("includeSeen") == "true"

	notifications, err := h.service.GetAll(includeSeen)
	if err != nil {
		return utils.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.Success(c, "Notifications fetched", notifications)
}

func (h *NotificationHandler) GetCount(c *fiber.Ctx) error {
	count, err := h.service.GetCount()
	if err != nil {
		return utils.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	// Return count in the same shape your composable expects: { success, count }
	return c.JSON(fiber.Map{
		"success": true,
		"count":   count,
	})
}

func (h *NotificationHandler) MarkAsSeen(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "Invalid notification ID")
	}

	if err := h.service.MarkAsSeen(uint(id)); err != nil {
		return utils.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.Success(c, "Notification marked as seen", nil)
}

func (h *NotificationHandler) MarkAllAsSeen(c *fiber.Ctx) error {
	if err := h.service.MarkAllAsSeen(); err != nil {
		return utils.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.Success(c, "All notifications marked as seen", nil)
}

func (h *NotificationHandler) Delete(c *fiber.Ctx) error {
	id, err := strconv.ParseUint(c.Params("id"), 10, 32)
	if err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "Invalid notification ID")
	}

	if err := h.service.Delete(uint(id)); err != nil {
		return utils.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.Success(c, "Notification deleted", nil)
}

func (h *NotificationHandler) ClearSeen(c *fiber.Ctx) error {
	if err := h.service.ClearSeen(); err != nil {
		return utils.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.Success(c, "Seen notifications cleared", nil)
}