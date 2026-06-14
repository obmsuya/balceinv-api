package handlers

import (
	"strconv"

	"github.com/chrisostomemataba/balceinv-api/services"
	"github.com/chrisostomemataba/balceinv-api/utils"
	"github.com/gofiber/fiber/v2"
)

type AddonHandler struct {
	addonService *services.AddonService
}

func NewAddonHandler(addonService *services.AddonService) *AddonHandler {
	return &AddonHandler{addonService: addonService}
}

func (handler *AddonHandler) GetByProduct(context *fiber.Ctx) error {
	productID, err := strconv.ParseUint(context.Params("id"), 10, 32)
	if err != nil {
		return utils.Error(context, fiber.StatusBadRequest, "Invalid product ID")
	}

	addons, err := handler.addonService.GetByProduct(uint(productID))
	if err != nil {
		status := fiber.StatusInternalServerError
		if err.Error() == "product not found" {
			status = fiber.StatusNotFound
		}
		return utils.Error(context, status, err.Error())
	}

	return utils.Success(context, "Addons fetched", addons)
}

func (handler *AddonHandler) Create(context *fiber.Ctx) error {
	productID, err := strconv.ParseUint(context.Params("id"), 10, 32)
	if err != nil {
		return utils.Error(context, fiber.StatusBadRequest, "Invalid product ID")
	}

	var input services.CreateAddonInput
	if err := context.BodyParser(&input); err != nil {
		return utils.Error(context, fiber.StatusBadRequest, "Invalid request body")
	}

	addon, err := handler.addonService.Create(uint(productID), input)
	if err != nil {
		status := fiber.StatusBadRequest
		if err.Error() == "product not found" {
			status = fiber.StatusNotFound
		}
		return utils.Error(context, status, err.Error())
	}

	return utils.Success(context, "Addon created", addon)
}

func (handler *AddonHandler) Update(context *fiber.Ctx) error {
	addonID, err := strconv.ParseUint(context.Params("id"), 10, 32)
	if err != nil {
		return utils.Error(context, fiber.StatusBadRequest, "Invalid addon ID")
	}

	var input services.UpdateAddonInput
	if err := context.BodyParser(&input); err != nil {
		return utils.Error(context, fiber.StatusBadRequest, "Invalid request body")
	}

	addon, err := handler.addonService.Update(uint(addonID), input)
	if err != nil {
		status := fiber.StatusBadRequest
		if err.Error() == "addon not found" {
			status = fiber.StatusNotFound
		}
		return utils.Error(context, status, err.Error())
	}

	return utils.Success(context, "Addon updated", addon)
}

func (handler *AddonHandler) Delete(context *fiber.Ctx) error {
	addonID, err := strconv.ParseUint(context.Params("id"), 10, 32)
	if err != nil {
		return utils.Error(context, fiber.StatusBadRequest, "Invalid addon ID")
	}

	if err := handler.addonService.Delete(uint(addonID)); err != nil {
		status := fiber.StatusInternalServerError
		if err.Error() == "addon not found" {
			status = fiber.StatusNotFound
		}
		return utils.Error(context, status, err.Error())
	}

	return utils.Success(context, "Addon deleted", nil)
}