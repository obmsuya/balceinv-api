package products

import (
	"errors"
	"io"

	"github.com/chrisostomemataba/balceinv-api/internal/common/httpx"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/chrisostomemataba/balceinv-api/internal/media"
	"github.com/chrisostomemataba/balceinv-api/internal/stock"
	"github.com/gofiber/fiber/v2"
)

const spreadsheetContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"

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
	listFilter := ListFilter{
		SearchText:      c.Query("q"),
		Category:        c.Query("category"),
		IncludeArchived: c.QueryBool("include_archived", false),
	}

	productPage, listError := handler.service.List(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), listFilter, pagination.Limit, pagination.Offset)
	if listError != nil {
		return respondWithServiceError(c, listError)
	}
	return response.Paged(c, "Products", productPage)
}

func (handler *Handler) Categories(c *fiber.Ctx) error {
	principal := httpx.CurrentPrincipal(c)

	categories, listError := handler.service.ListCategories(c.UserContext(), httpx.RequestQuerier(c), principal.CompanyId)
	if listError != nil {
		return respondWithServiceError(c, listError)
	}
	return response.Success(c, "Categories", categories)
}

func (handler *Handler) Get(c *fiber.Ctx) error {
	productId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrProductNotFound)
	}

	productView, getError := handler.service.Get(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), productId)
	if getError != nil {
		return respondWithServiceError(c, getError)
	}
	return response.Success(c, "Product", productView)
}

func (handler *Handler) Lookup(c *fiber.Ctx) error {
	lookupView, lookupError := handler.service.Lookup(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), c.Query("code"))
	if lookupError != nil {
		return respondWithServiceError(c, lookupError)
	}
	return response.Success(c, "Product", lookupView)
}

func (handler *Handler) Variants(c *fiber.Ctx) error {
	parentId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrProductNotFound)
	}

	variantViews, listError := handler.service.ListVariants(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), parentId)
	if listError != nil {
		return respondWithServiceError(c, listError)
	}
	return response.Success(c, "Variants", variantViews)
}

func (handler *Handler) Create(c *fiber.Ctx) error {
	request := CreateProductRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	productView, createError := handler.service.Create(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), request)
	if createError != nil {
		return respondWithServiceError(c, createError)
	}
	return response.Created(c, "Product created", productView)
}

func (handler *Handler) Update(c *fiber.Ctx) error {
	productId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrProductNotFound)
	}

	request := UpdateProductRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	productView, updateError := handler.service.Update(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), productId, request)
	if updateError != nil {
		return respondWithServiceError(c, updateError)
	}
	return response.Success(c, "Product updated", productView)
}

func (handler *Handler) Archive(c *fiber.Ctx) error {
	productId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrProductNotFound)
	}

	archiveError := handler.service.Archive(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), productId)
	if archiveError != nil {
		return respondWithServiceError(c, archiveError)
	}
	return response.Success(c, "Product archived", nil)
}

func (handler *Handler) Restore(c *fiber.Ctx) error {
	productId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrProductNotFound)
	}

	productView, restoreError := handler.service.Restore(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), productId)
	if restoreError != nil {
		return respondWithServiceError(c, restoreError)
	}
	return response.Success(c, "Product restored", productView)
}

func (handler *Handler) UploadImage(c *fiber.Ctx) error {
	productId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrProductNotFound)
	}

	imageBytes, isRead, uploadResponseError := readUpload(c, "image", MaximumImageBytes)
	if !isRead {
		return uploadResponseError
	}

	productView, uploadError := handler.service.UploadImage(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), productId, imageBytes)
	if uploadError != nil {
		return respondWithServiceError(c, uploadError)
	}
	return response.Success(c, "Image updated", productView)
}

func (handler *Handler) ImportTemplate(c *fiber.Ctx) error {
	templateBytes, templateError := handler.service.ImportTemplate()
	if templateError != nil {
		return templateError
	}

	c.Set(fiber.HeaderContentType, spreadsheetContentType)
	c.Set(fiber.HeaderContentDisposition, `attachment; filename="products-template.xlsx"`)
	return c.Send(templateBytes)
}

func (handler *Handler) Import(c *fiber.Ctx) error {
	uploadedFile, formFileError := c.FormFile("file")
	if formFileError != nil {
		return response.Error(c, fiber.StatusBadRequest, "bad_request", "Attach the spreadsheet as a file field named file")
	}
	isTooLarge := uploadedFile.Size > MaximumImportBytes
	if isTooLarge {
		return response.Error(c, fiber.StatusRequestEntityTooLarge, "file_too_large", "The file must be 5 MB or smaller")
	}

	openedFile, openError := uploadedFile.Open()
	if openError != nil {
		return response.Error(c, fiber.StatusBadRequest, "bad_request", "The file could not be read")
	}
	defer openedFile.Close()

	importResult, importError := handler.service.ImportProducts(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), uploadedFile.Filename, openedFile)
	if errors.Is(importError, ErrImportRejected) {
		return response.ErrorWithDetails(c, fiber.StatusUnprocessableEntity, "import_rejected", importError.Error(), importResult)
	}
	if importError != nil {
		return respondWithServiceError(c, importError)
	}
	return response.Created(c, "Products imported", importResult)
}

func (handler *Handler) ListAddons(c *fiber.Ctx) error {
	productId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrProductNotFound)
	}

	addonViews, listError := handler.service.ListAddons(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), productId)
	if listError != nil {
		return respondWithServiceError(c, listError)
	}
	return response.Success(c, "Add-ons", addonViews)
}

func (handler *Handler) CreateAddon(c *fiber.Ctx) error {
	productId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrProductNotFound)
	}

	request := AddonRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	addonView, createError := handler.service.CreateAddon(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), productId, request)
	if createError != nil {
		return respondWithServiceError(c, createError)
	}
	return response.Created(c, "Add-on created", addonView)
}

func (handler *Handler) UpdateAddon(c *fiber.Ctx) error {
	addonId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrAddonNotFound)
	}

	request := AddonRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	addonView, updateError := handler.service.UpdateAddon(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), addonId, request)
	if updateError != nil {
		return respondWithServiceError(c, updateError)
	}
	return response.Success(c, "Add-on updated", addonView)
}

func (handler *Handler) DeleteAddon(c *fiber.Ctx) error {
	addonId, isValidId := httpx.UuidParam(c, "id")
	if !isValidId {
		return respondWithServiceError(c, ErrAddonNotFound)
	}

	deleteError := handler.service.DeleteAddon(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), addonId)
	if deleteError != nil {
		return respondWithServiceError(c, deleteError)
	}
	return response.Success(c, "Add-on deleted", nil)
}

func readUpload(c *fiber.Ctx, fieldName string, maximumBytes int) ([]byte, bool, error) {
	uploadedFile, formFileError := c.FormFile(fieldName)
	if formFileError != nil {
		return nil, false, response.Error(c, fiber.StatusBadRequest, "bad_request", "Attach the file as a field named "+fieldName)
	}
	isTooLarge := uploadedFile.Size > int64(maximumBytes)
	if isTooLarge {
		return nil, false, respondWithServiceError(c, media.ErrImageTooLarge)
	}

	openedFile, openError := uploadedFile.Open()
	if openError != nil {
		return nil, false, response.Error(c, fiber.StatusBadRequest, "bad_request", "The file could not be read")
	}
	defer openedFile.Close()

	fileBytes, readError := io.ReadAll(io.LimitReader(openedFile, int64(maximumBytes)+1))
	if readError != nil {
		return nil, false, response.Error(c, fiber.StatusBadRequest, "bad_request", "The file could not be read")
	}

	return fileBytes, true, nil
}

func respondWithServiceError(c *fiber.Ctx, serviceError error) error {
	switch {
	case errors.Is(serviceError, ErrProductNotFound), errors.Is(serviceError, ErrParentNotFound), errors.Is(serviceError, ErrAddonNotFound):
		return response.Error(c, fiber.StatusNotFound, "not_found", serviceError.Error())
	case errors.Is(serviceError, ErrSkuTaken):
		return response.Error(c, fiber.StatusConflict, "sku_taken", serviceError.Error())
	case errors.Is(serviceError, ErrBarcodeTaken):
		return response.Error(c, fiber.StatusConflict, "barcode_taken", serviceError.Error())
	case errors.Is(serviceError, ErrAddonNameTaken):
		return response.Error(c, fiber.StatusConflict, "addon_name_taken", serviceError.Error())
	case errors.Is(serviceError, ErrNestedVariant), errors.Is(serviceError, ErrVariantLabelRequired),
		errors.Is(serviceError, ErrDuplicateBarcode), errors.Is(serviceError, ErrInvalidMetadata),
		errors.Is(serviceError, ErrNoActiveShop):
		return response.Error(c, fiber.StatusBadRequest, "invalid_product", serviceError.Error())
	case errors.Is(serviceError, ErrImportFileType), errors.Is(serviceError, ErrImportUnreadable),
		errors.Is(serviceError, ErrImportEmpty), errors.Is(serviceError, ErrImportMissingColumns),
		errors.Is(serviceError, ErrImportTooManyRows):
		return response.Error(c, fiber.StatusBadRequest, "invalid_import", serviceError.Error())
	case errors.Is(serviceError, media.ErrEmptyImage), errors.Is(serviceError, media.ErrUnsupportedImage):
		return response.Error(c, fiber.StatusBadRequest, "invalid_image", serviceError.Error())
	case errors.Is(serviceError, media.ErrImageTooLarge):
		return response.Error(c, fiber.StatusRequestEntityTooLarge, "image_too_large", "The image must be 2 MB or smaller")
	case errors.Is(serviceError, stock.ErrInsufficientStock):
		return response.Error(c, fiber.StatusConflict, "insufficient_stock", serviceError.Error())
	default:
		return serviceError
	}
}
