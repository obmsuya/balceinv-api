package handlers

import (
	"github.com/chrisostomemataba/balceinv-api/services"
	"github.com/chrisostomemataba/balceinv-api/utils"
	"github.com/gofiber/fiber/v2"
)

type SetupHandler struct {
	service *services.SetupService
}

func NewSetupHandler(service *services.SetupService) *SetupHandler {
	return &SetupHandler{service: service}
}

func (h *SetupHandler) Status(c *fiber.Ctx) error {
	return utils.Success(c, "Setup status", fiber.Map{
		"configured": h.service.IsConfigured(),
	})
}

func (h *SetupHandler) Run(c *fiber.Ctx) error {
	var input services.SetupInput
	if err := c.BodyParser(&input); err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if input.BusinessName == "" || input.BusinessType == "" ||
		input.OwnerName == "" || input.OwnerEmail == "" || input.OwnerPassword == "" {
		return utils.Error(c, fiber.StatusBadRequest, "All required fields must be provided")
	}

	if err := h.service.Run(input); err != nil {
		return utils.Error(c, fiber.StatusBadRequest, err.Error())
	}

	return utils.Success(c, "Setup complete. You can now log in.", nil)
}