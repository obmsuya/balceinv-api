package catalog

import (
	"errors"

	"github.com/chrisostomemataba/balceinv-api/internal/common/httpx"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
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
	principal := httpx.CurrentPrincipal(c)

	catalogViews, listError := handler.service.ListForCompany(c.UserContext(), httpx.RequestQuerier(c), principal.CompanyId)
	if listError != nil {
		return respondWithServiceError(c, listError)
	}
	return response.Success(c, "Common products", catalogViews)
}

func (handler *Handler) TeamSummary(c *fiber.Ctx) error {
	principal := httpx.CurrentPrincipal(c)

	summary, summaryError := handler.service.Summary(c.UserContext(), httpx.RequestQuerier(c), principal.CompanyId)
	if summaryError != nil {
		return respondWithServiceError(c, summaryError)
	}
	return response.Success(c, "Common product lists", summary)
}

func (handler *Handler) TeamItems(c *fiber.Ctx) error {
	catalogViews, listError := handler.service.Items(c.UserContext(), httpx.RequestQuerier(c), c.Query("business_type"))
	if listError != nil {
		return respondWithServiceError(c, listError)
	}
	return response.Success(c, "Common products", catalogViews)
}

func (handler *Handler) TeamTemplate(c *fiber.Ctx) error {
	templateBytes, templateError := handler.service.Template()
	if templateError != nil {
		return templateError
	}

	c.Set(fiber.HeaderContentType, spreadsheetContentType)
	c.Set(fiber.HeaderContentDisposition, `attachment; filename="common-products-template.xlsx"`)
	return c.Send(templateBytes)
}

func (handler *Handler) TeamImport(c *fiber.Ctx) error {
	uploadedFile, formFileError := c.FormFile("file")
	if formFileError != nil {
		return response.Error(c, fiber.StatusBadRequest, "bad_request", "Attach the list as a file field named file")
	}
	isTooLarge := uploadedFile.Size > MaximumImportBytes
	if isTooLarge {
		return response.Error(c, fiber.StatusRequestEntityTooLarge, "file_too_large", "This file is too big. Keep it under 4 MB")
	}

	openedFile, openError := uploadedFile.Open()
	if openError != nil {
		return response.Error(c, fiber.StatusBadRequest, "bad_request", "The file could not be read")
	}
	defer openedFile.Close()

	importResult, importError := handler.service.Import(c.UserContext(), httpx.RequestQuerier(c), c.FormValue("business_type"), c.FormValue("mode"), uploadedFile.Filename, openedFile)
	if errors.Is(importError, ErrNoValidRows) {
		return response.ErrorWithDetails(c, fiber.StatusUnprocessableEntity, "import_rejected", importError.Error(), importResult)
	}
	if importError != nil {
		return respondWithServiceError(c, importError)
	}
	return response.Success(c, "List saved", importResult)
}

func (handler *Handler) TeamClear(c *fiber.Ctx) error {
	clearResult, clearError := handler.service.Clear(c.UserContext(), httpx.RequestQuerier(c), c.Query("business_type"))
	if clearError != nil {
		return respondWithServiceError(c, clearError)
	}
	return response.Success(c, "List cleared", clearResult)
}

func respondWithServiceError(c *fiber.Ctx, serviceError error) error {
	switch {
	case errors.Is(serviceError, ErrBusinessTypeInvalid), errors.Is(serviceError, ErrImportModeInvalid):
		return response.Error(c, fiber.StatusBadRequest, "invalid_request", serviceError.Error())
	case IsImportFileProblem(serviceError):
		return response.Error(c, fiber.StatusBadRequest, "invalid_import", serviceError.Error())
	default:
		return serviceError
	}
}
