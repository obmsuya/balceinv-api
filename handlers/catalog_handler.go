package handlers

import (
	"errors"
	"fmt"
	"strings"
	"unicode"

	"github.com/chrisostomemataba/balceinv-api/services"
	"github.com/chrisostomemataba/balceinv-api/utils"
	"github.com/gofiber/fiber/v2"
)

type CatalogHandler struct {
	catalogService *services.CatalogService
}

func NewCatalogHandler(catalogService *services.CatalogService) *CatalogHandler {
	return &CatalogHandler{catalogService: catalogService}
}

var catalogInputErrors = []error{
	services.ErrCatalogBusinessTypeInvalid,
	services.ErrCatalogImportModeInvalid,
	services.ErrCatalogFileType,
	services.ErrCatalogFileUnreadable,
	services.ErrCatalogFileEmpty,
	services.ErrCatalogNameColumnMissing,
	services.ErrCatalogTooManyRows,
}

func (handler *CatalogHandler) GetAll(context *fiber.Ctx) error {
	catalogProducts, listError := handler.catalogService.ListForCompany()
	if listError != nil {
		return utils.Error(context, fiber.StatusInternalServerError, "Could not load the common products")
	}
	return utils.Success(context, "Catalog loaded", catalogProducts)
}

func (handler *CatalogHandler) TeamSummary(context *fiber.Ctx) error {
	catalogSummary, summaryError := handler.catalogService.Summary()
	if summaryError != nil {
		return utils.Error(context, fiber.StatusInternalServerError, "Could not count the common products")
	}
	return utils.Success(context, "Catalog summary loaded", catalogSummary)
}

func (handler *CatalogHandler) TeamItems(context *fiber.Ctx) error {
	catalogProducts, listError := handler.catalogService.ListForBusinessType(context.Query("business_type"))
	if listError != nil {
		return respondWithCatalogError(context, listError, "Could not load the common products")
	}
	return utils.Success(context, "Catalog loaded", catalogProducts)
}

func (handler *CatalogHandler) TeamTemplate(context *fiber.Ctx) error {
	templateBytes, templateError := handler.catalogService.Template()
	if templateError != nil {
		return utils.Error(context, fiber.StatusInternalServerError, "Could not build the template")
	}
	context.Set("Content-Type", "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet")
	context.Set("Content-Disposition", "attachment; filename=common-products-template.xlsx")
	return context.Send(templateBytes)
}

func (handler *CatalogHandler) TeamImport(context *fiber.Ctx) error {
	uploadedFile, formFileError := context.FormFile("file")
	if formFileError != nil {
		return utils.Error(context, fiber.StatusBadRequest, "Choose an Excel or CSV file")
	}
	openedFile, openError := uploadedFile.Open()
	if openError != nil {
		return utils.Error(context, fiber.StatusBadRequest, "Could not open the file")
	}
	defer openedFile.Close()

	importResult, importError := handler.catalogService.Import(
		context.FormValue("business_type"),
		context.FormValue("mode", services.CatalogImportModeMerge),
		uploadedFile.Filename,
		openedFile,
	)
	if errors.Is(importError, services.ErrCatalogNoValidRows) {
		return context.Status(fiber.StatusUnprocessableEntity).JSON(fiber.Map{
			"success": false,
			"message": sentenceCase(importError.Error()),
			"data":    importResult,
		})
	}
	if importError != nil {
		return respondWithCatalogError(context, importError, "Could not save the list")
	}
	return utils.Success(context, fmt.Sprintf("%d added, %d updated", importResult.Added, importResult.Updated), importResult)
}

func (handler *CatalogHandler) TeamClear(context *fiber.Ctx) error {
	removedCount, clearError := handler.catalogService.Clear(context.Query("business_type"))
	if clearError != nil {
		return respondWithCatalogError(context, clearError, "Could not clear the list")
	}
	return utils.Success(context, fmt.Sprintf("Removed %d common products", removedCount), fiber.Map{"removed": removedCount})
}

func respondWithCatalogError(context *fiber.Ctx, catalogError error, fallbackMessage string) error {
	for _, inputError := range catalogInputErrors {
		if errors.Is(catalogError, inputError) {
			return utils.Error(context, fiber.StatusBadRequest, sentenceCase(inputError.Error()))
		}
	}
	return utils.Error(context, fiber.StatusInternalServerError, fallbackMessage)
}

func sentenceCase(message string) string {
	if message == "" {
		return message
	}
	messageRunes := []rune(message)
	messageRunes[0] = unicode.ToUpper(messageRunes[0])
	return strings.TrimSpace(string(messageRunes))
}
