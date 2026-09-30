package businessmove

import (
	"errors"
	"io"
	"log/slog"

	"github.com/chrisostomemataba/balceinv-api/internal/common/httpx"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/chrisostomemataba/balceinv-api/internal/documents"
	"github.com/gofiber/fiber/v2"
)

const PackageSizeLimit = 64 << 20

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{service: service}
}

func (handler *Handler) DownloadPackage(c *fiber.Ctx) error {
	if !httpx.CurrentPrincipal(c).IsOwner {
		return response.Error(c, fiber.StatusForbidden, "move_owner_only", ErrOwnerOnly.Error())
	}
	packageBytes, manifest, buildError := handler.service.BuildPackage(c.UserContext())
	if buildError != nil {
		return buildError
	}
	fileName := documents.SafeFileName(manifest.BusinessName+"-move-online") + ".balce"
	return httpx.SendFile(c, fileName, "application/zip", packageBytes)
}

func (handler *Handler) ImportPackage(c *fiber.Ctx) error {
	uploadedFile, formError := c.FormFile("file")
	if formError != nil {
		return response.Error(c, fiber.StatusBadRequest, "move_file_missing", "Choose the moving file from the Balce desktop app.")
	}
	if uploadedFile.Size > PackageSizeLimit {
		return response.Error(c, fiber.StatusRequestEntityTooLarge, "move_file_too_large", "The moving file is too large to upload.")
	}
	openedFile, openError := uploadedFile.Open()
	if openError != nil {
		return response.Error(c, fiber.StatusBadRequest, "move_file_invalid", ErrInvalidPackage.Error())
	}
	defer openedFile.Close()
	packageBytes, readError := io.ReadAll(io.LimitReader(openedFile, PackageSizeLimit+1))
	if readError != nil {
		return response.Error(c, fiber.StatusBadRequest, "move_file_invalid", ErrInvalidPackage.Error())
	}

	moveResult, importError := handler.service.ImportPackage(c.UserContext(), packageBytes)
	switch {
	case errors.Is(importError, ErrAlreadyMoved):
		return response.Error(c, fiber.StatusConflict, "move_already_done", importError.Error())
	case errors.Is(importError, ErrEmailAlreadyUsed):
		return response.Error(c, fiber.StatusConflict, "move_email_taken", importError.Error())
	case errors.Is(importError, ErrNewerPackage):
		return response.Error(c, fiber.StatusUnprocessableEntity, "move_file_newer", ErrNewerPackage.Error())
	case errors.Is(importError, ErrInvalidPackage):
		slog.Warn("moving file refused", "error", importError)
		return response.Error(c, fiber.StatusBadRequest, "move_file_invalid", ErrInvalidPackage.Error())
	case importError != nil:
		return importError
	}
	slog.Info("business moved online", "business", moveResult.BusinessName, "mediaCount", moveResult.MediaCount)
	return response.Success(c, "Business moved online", moveResult)
}
