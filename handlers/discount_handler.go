package handlers

import (
	"strconv"

	"github.com/chrisostomemataba/balceinv-api/services"
	"github.com/chrisostomemataba/balceinv-api/utils"
	"github.com/gofiber/fiber/v2"
)

type DiscountHandler struct {
	discountService *services.DiscountService
}

func NewDiscountHandler(discountService *services.DiscountService) *DiscountHandler {
	return &DiscountHandler{discountService: discountService}
}

func (handler *DiscountHandler) GetAll(context *fiber.Ctx) error {
	discounts, err := handler.discountService.GetAll()
	if err != nil {
		return utils.Error(context, fiber.StatusInternalServerError, err.Error())
	}

	return utils.Success(context, "Discounts fetched", discounts)
}

func (handler *DiscountHandler) GetActiveForProduct(context *fiber.Ctx) error {
	productIDParam := context.Query("productId")
	if productIDParam == "" {
		return utils.Error(context, fiber.StatusBadRequest, "productId query parameter is required")
	}

	productID, err := strconv.ParseUint(productIDParam, 10, 32)
	if err != nil {
		return utils.Error(context, fiber.StatusBadRequest, "Invalid product ID")
	}

	priceParam := context.Query("price")
	if priceParam == "" {
		return utils.Error(context, fiber.StatusBadRequest, "price query parameter is required")
	}

	var originalPrice float64
	if _, err := strconv.ParseFloat(priceParam, 64); err != nil {
		return utils.Error(context, fiber.StatusBadRequest, "Invalid price value")
	}
	originalPrice, _ = strconv.ParseFloat(priceParam, 64)

	appliedDiscount, err := handler.discountService.GetActiveForProduct(uint(productID), originalPrice)
	if err != nil {
		return utils.Error(context, fiber.StatusInternalServerError, err.Error())
	}

	return utils.Success(context, "Active discount fetched", appliedDiscount)
}

func (handler *DiscountHandler) Create(context *fiber.Ctx) error {
	var input services.CreateDiscountInput
	if err := context.BodyParser(&input); err != nil {
		return utils.Error(context, fiber.StatusBadRequest, "Invalid request body")
	}

	payload := context.Locals("user").(*utils.TokenPayload)

	discount, err := handler.discountService.Create(input, payload.UserID)
	if err != nil {
		return utils.Error(context, fiber.StatusBadRequest, err.Error())
	}

	return utils.Success(context, "Discount created", discount)
}

func (handler *DiscountHandler) Update(context *fiber.Ctx) error {
	discountID, err := strconv.ParseUint(context.Params("id"), 10, 32)
	if err != nil {
		return utils.Error(context, fiber.StatusBadRequest, "Invalid discount ID")
	}

	var input services.UpdateDiscountInput
	if err := context.BodyParser(&input); err != nil {
		return utils.Error(context, fiber.StatusBadRequest, "Invalid request body")
	}

	discount, err := handler.discountService.Update(uint(discountID), input)
	if err != nil {
		status := fiber.StatusBadRequest
		if err.Error() == "discount not found" {
			status = fiber.StatusNotFound
		}
		return utils.Error(context, status, err.Error())
	}

	return utils.Success(context, "Discount updated", discount)
}

func (handler *DiscountHandler) Delete(context *fiber.Ctx) error {
	discountID, err := strconv.ParseUint(context.Params("id"), 10, 32)
	if err != nil {
		return utils.Error(context, fiber.StatusBadRequest, "Invalid discount ID")
	}

	if err := handler.discountService.Delete(uint(discountID)); err != nil {
		status := fiber.StatusInternalServerError
		if err.Error() == "discount not found" {
			status = fiber.StatusNotFound
		}
		return utils.Error(context, status, err.Error())
	}

	return utils.Success(context, "Discount deleted", nil)
}

func (handler *DiscountHandler) Deactivate(context *fiber.Ctx) error {
	discountID, err := strconv.ParseUint(context.Params("id"), 10, 32)
	if err != nil {
		return utils.Error(context, fiber.StatusBadRequest, "Invalid discount ID")
	}

	if err := handler.discountService.Deactivate(uint(discountID)); err != nil {
		status := fiber.StatusInternalServerError
		if err.Error() == "discount not found" {
			status = fiber.StatusNotFound
		}
		return utils.Error(context, status, err.Error())
	}

	return utils.Success(context, "Discount deactivated", nil)
}