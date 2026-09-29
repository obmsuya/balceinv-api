package legacyimport

import (
	"errors"

	"github.com/chrisostomemataba/balceinv-api/internal/common/httpx"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/chrisostomemataba/balceinv-api/internal/tenancy"
	"github.com/gofiber/fiber/v2"
)

type Handler struct {
	service         *Service
	oldDatabasePath string
}

func NewHandler(service *Service, oldDatabasePath string) *Handler {
	return &Handler{
		service:         service,
		oldDatabasePath: oldDatabasePath,
	}
}

func (handler *Handler) Preview(c *fiber.Ctx) error {
	preview, previewError := handler.service.Preview(c.UserContext(), httpx.RequestQuerier(c), handler.oldDatabasePath)
	if previewError != nil {
		return respondWithImportError(c, previewError)
	}
	return response.Success(c, "Old data preview", preview)
}

func (handler *Handler) Import(c *fiber.Ctx) error {
	request := ImportRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	importResult, importError := handler.service.Import(c.UserContext(), httpx.RequestQuerier(c), handler.oldDatabasePath, request)
	if errors.Is(importError, ErrImportMismatch) {
		return response.ErrorWithDetails(c, fiber.StatusUnprocessableEntity, "import_mismatch", importError.Error(), importResult.SelfCheck)
	}
	if importError != nil {
		return respondWithImportError(c, importError)
	}

	return response.Created(c, "Your old data is here. You can now sign in.", importResult)
}

func respondWithImportError(c *fiber.Ctx, importError error) error {
	switch {
	case errors.Is(importError, tenancy.ErrAlreadyConfigured):
		return response.Error(c, fiber.StatusConflict, "already_configured", importError.Error())
	case errors.Is(importError, ErrOldDataNotFound):
		return response.Error(c, fiber.StatusNotFound, "old_data_not_found", ErrOldDataNotFound.Error())
	case errors.Is(importError, ErrOldDataUnreadable):
		return response.Error(c, fiber.StatusBadRequest, "old_data_unreadable", ErrOldDataUnreadable.Error())
	case errors.Is(importError, ErrNoOldUsers):
		return response.Error(c, fiber.StatusBadRequest, "no_old_users", importError.Error())
	case errors.Is(importError, ErrUnknownOwner):
		return response.Error(c, fiber.StatusBadRequest, "unknown_owner", importError.Error())
	case errors.Is(importError, ErrBusinessNameMissing):
		return response.Error(c, fiber.StatusBadRequest, "business_name_missing", importError.Error())
	case errors.Is(importError, tenancy.ErrEmailTaken):
		return response.Error(c, fiber.StatusConflict, "email_taken", importError.Error())
	}
	return importError
}
