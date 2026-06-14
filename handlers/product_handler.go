package handlers

import (
	"fmt"
	"strconv"

	"github.com/chrisostomemataba/balceinv-api/services"
	"github.com/chrisostomemataba/balceinv-api/utils"
	"github.com/gofiber/fiber/v2"
)

type ProductHandler struct {
	productService *services.ProductService
}

func NewProductHandler(productService *services.ProductService) *ProductHandler {
	return &ProductHandler{productService: productService}
}

func (handler *ProductHandler) GetAll(context *fiber.Ctx) error {
	search := context.Query("search")
	category := context.Query("category")

	products, err := handler.productService.GetAll(search, category)
	if err != nil {
		return utils.Error(context, fiber.StatusInternalServerError, err.Error())
	}

	return utils.Success(context, "Products fetched", products)
}

func (handler *ProductHandler) GetByID(context *fiber.Ctx) error {
	productID, err := strconv.ParseUint(context.Params("id"), 10, 32)
	if err != nil {
		return utils.Error(context, fiber.StatusBadRequest, "Invalid product ID")
	}

	product, err := handler.productService.GetByID(uint(productID))
	if err != nil {
		return utils.Error(context, fiber.StatusNotFound, err.Error())
	}

	return utils.Success(context, "Product fetched", product)
}

func (handler *ProductHandler) GetVariants(context *fiber.Ctx) error {
	parentID, err := strconv.ParseUint(context.Params("id"), 10, 32)
	if err != nil {
		return utils.Error(context, fiber.StatusBadRequest, "Invalid product ID")
	}

	variants, err := handler.productService.GetVariants(uint(parentID))
	if err != nil {
		status := fiber.StatusInternalServerError
		if err.Error() == "product not found" {
			status = fiber.StatusNotFound
		}
		return utils.Error(context, status, err.Error())
	}

	return utils.Success(context, "Variants fetched", variants)
}

func (handler *ProductHandler) Create(context *fiber.Ctx) error {
	var input services.CreateProductInput
	if err := context.BodyParser(&input); err != nil {
		return utils.Error(context, fiber.StatusBadRequest, "Invalid request body")
	}

	product, err := handler.productService.Create(input)
	if err != nil {
		status := fiber.StatusInternalServerError
		if err.Error() == "product with this SKU already exists" {
			status = fiber.StatusConflict
		}
		if err.Error() == "parent product not found" {
			status = fiber.StatusNotFound
		}
		if err.Error() == "variant label is required when parent_id is set" {
			status = fiber.StatusBadRequest
		}
		return utils.Error(context, status, err.Error())
	}

	return utils.Success(context, "Product created", product)
}

func (handler *ProductHandler) Update(context *fiber.Ctx) error {
	productID, err := strconv.ParseUint(context.Params("id"), 10, 32)
	if err != nil {
		return utils.Error(context, fiber.StatusBadRequest, "Invalid product ID")
	}

	var input services.UpdateProductInput
	if err := context.BodyParser(&input); err != nil {
		return utils.Error(context, fiber.StatusBadRequest, "Invalid request body")
	}

	payload := context.Locals("user").(*utils.TokenPayload)
	userID := payload.UserID

	product, err := handler.productService.Update(uint(productID), input, &userID)
	if err != nil {
		status := fiber.StatusInternalServerError
		if err.Error() == "product not found" {
			status = fiber.StatusNotFound
		}
		return utils.Error(context, status, err.Error())
	}

	return utils.Success(context, "Product updated", product)
}

func (handler *ProductHandler) UpdateImage(context *fiber.Ctx) error {
	productID, err := strconv.ParseUint(context.Params("id"), 10, 32)
	if err != nil {
		return utils.Error(context, fiber.StatusBadRequest, "Invalid product ID")
	}

	var body struct {
		Image string `json:"image"`
	}
	if err := context.BodyParser(&body); err != nil {
		return utils.Error(context, fiber.StatusBadRequest, "Invalid request body")
	}

	if body.Image == "" {
		return utils.Error(context, fiber.StatusBadRequest, "Image data is required")
	}

	product, err := handler.productService.UpdateImage(uint(productID), body.Image)
	if err != nil {
		status := fiber.StatusInternalServerError
		if err.Error() == "product not found" {
			status = fiber.StatusNotFound
		}
		return utils.Error(context, status, err.Error())
	}

	return utils.Success(context, "Product image updated", product)
}

func (handler *ProductHandler) Delete(context *fiber.Ctx) error {
	productID, err := strconv.ParseUint(context.Params("id"), 10, 32)
	if err != nil {
		return utils.Error(context, fiber.StatusBadRequest, "Invalid product ID")
	}

	if err := handler.productService.Delete(uint(productID)); err != nil {
		status := fiber.StatusInternalServerError
		if err.Error() == "product not found" {
			status = fiber.StatusNotFound
		}
		return utils.Error(context, status, err.Error())
	}

	return utils.Success(context, "Product deleted", nil)
}

func (handler *ProductHandler) UploadExcel(context *fiber.Ctx) error {
	file, err := context.FormFile("file")
	if err != nil {
		return utils.Error(context, fiber.StatusBadRequest, "No file uploaded")
	}

	result, err := handler.productService.UploadExcel(file)
	if err != nil {
		return utils.Error(context, fiber.StatusBadRequest, err.Error())
	}

	return utils.Success(context, fmt.Sprintf("Imported %d products", result.Created), result)
}

func (handler *ProductHandler) GetTemplate(context *fiber.Ctx) error {
	templateData, err := handler.productService.GetTemplate()
	if err != nil {
		return utils.Error(context, fiber.StatusInternalServerError, "Could not generate template")
	}

	context.Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	context.Set("Content-Disposition", "attachment; filename=products_template.xlsx")
	return context.Send(templateData)
}

func (handler *ProductHandler) GetLowStock(context *fiber.Ctx) error {
	products, err := handler.productService.GetLowStock()
	if err != nil {
		return utils.Error(context, fiber.StatusInternalServerError, err.Error())
	}

	return utils.Success(context, "Low stock products", products)
}