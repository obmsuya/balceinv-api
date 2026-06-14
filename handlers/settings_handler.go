package handlers

import (
	"github.com/chrisostomemataba/balceinv-api/services"
	"github.com/chrisostomemataba/balceinv-api/utils"
	"github.com/gofiber/fiber/v2"
)

type SettingsHandler struct {
	service *services.SettingsService
}

func NewSettingsHandler(service *services.SettingsService) *SettingsHandler {
	return &SettingsHandler{service: service}
}

func (h *SettingsHandler) Get(c *fiber.Ctx) error {
	settings, err := h.service.GetOrCreate()
	if err != nil {
		return utils.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.Success(c, "Settings fetched", settings)
}

func (h *SettingsHandler) Update(c *fiber.Ctx) error {
	var input services.UpdateSettingsInput
	if err := c.BodyParser(&input); err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "Invalid request body")
	}

	payload := c.Locals("user").(*utils.TokenPayload)

	settings, err := h.service.Update(input, payload.UserID)
	if err != nil {
		return utils.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.Success(c, "Settings updated successfully", settings)
}

func (h *SettingsHandler) TestEFD(c *fiber.Ctx) error {
	var body struct {
		Endpoint string `json:"endpoint"`
		APIKey   string `json:"apiKey"`
	}
	if err := c.BodyParser(&body); err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "Invalid request body")
	}

	if body.Endpoint == "" || body.APIKey == "" {
		return utils.Error(c, fiber.StatusBadRequest, "Endpoint and API key are required")
	}

	result, err := h.service.TestEFD(body.Endpoint, body.APIKey)
	if err != nil {
		return utils.Error(c, fiber.StatusInternalServerError, err.Error())
	}

	// Return success true even when the EFD test itself fails — the request
	// succeeded, it is just the EFD connection that reported a problem.
	// The frontend reads result.status to know whether the EFD is reachable.
	return utils.Success(c, "EFD test completed", result)
}

func (h *SettingsHandler) UploadLogo(c *fiber.Ctx) error {
	file, err := c.FormFile("file")
	if err != nil {
		return utils.Error(c, fiber.StatusBadRequest, "No file uploaded")
	}

	logoURL, err := h.service.UploadLogo(file)
	if err != nil {
		return utils.Error(c, fiber.StatusInternalServerError, err.Error())
	}
	return utils.Success(c, "Logo uploaded successfully", fiber.Map{"logoUrl": logoURL})
}