package settings

import (
	"errors"
	"io"

	"github.com/chrisostomemataba/balceinv-api/internal/common/httpx"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/chrisostomemataba/balceinv-api/internal/media"
	"github.com/gofiber/fiber/v2"
)

type Handler struct {
	service *Service
}

func NewHandler(service *Service) *Handler {
	return &Handler{
		service: service,
	}
}

func (handler *Handler) Get(c *fiber.Ctx) error {
	principal := httpx.CurrentPrincipal(c)

	settingsView, getError := handler.service.Get(c.UserContext(), httpx.RequestQuerier(c), principal.CompanyId)
	if getError != nil {
		return respondWithServiceError(c, getError)
	}
	return response.Success(c, "Settings", settingsView)
}

func (handler *Handler) Update(c *fiber.Ctx) error {
	request := UpdateSettingsRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	settingsView, updateError := handler.service.Update(c.UserContext(), httpx.RequestQuerier(c), httpx.CurrentPrincipal(c), request)
	if updateError != nil {
		return respondWithServiceError(c, updateError)
	}
	return response.Success(c, "Settings saved", settingsView)
}

func (handler *Handler) UploadLogo(c *fiber.Ctx) error {
	principal := httpx.CurrentPrincipal(c)

	uploadedFile, formFileError := c.FormFile("file")
	if formFileError != nil {
		return response.Error(c, fiber.StatusBadRequest, "bad_request", "Attach the logo as a file field named file")
	}
	isTooLarge := uploadedFile.Size > MaximumLogoBytes
	if isTooLarge {
		return respondWithServiceError(c, media.ErrImageTooLarge)
	}

	openedFile, openError := uploadedFile.Open()
	if openError != nil {
		return response.Error(c, fiber.StatusBadRequest, "bad_request", "The logo file could not be read")
	}
	defer openedFile.Close()

	logoBytes, readError := io.ReadAll(io.LimitReader(openedFile, MaximumLogoBytes+1))
	if readError != nil {
		return response.Error(c, fiber.StatusBadRequest, "bad_request", "The logo file could not be read")
	}

	settingsView, uploadError := handler.service.UploadLogo(c.UserContext(), httpx.RequestQuerier(c), principal.CompanyId, logoBytes)
	if uploadError != nil {
		return respondWithServiceError(c, uploadError)
	}
	return response.Success(c, "Logo updated", settingsView)
}

func respondWithServiceError(c *fiber.Ctx, serviceError error) error {
	switch {
	case errors.Is(serviceError, ErrSettingsNotFound):
		return response.Error(c, fiber.StatusNotFound, "not_found", serviceError.Error())
	case errors.Is(serviceError, ErrCurrencyLocked):
		return response.Error(c, fiber.StatusConflict, "currency_locked", serviceError.Error())
	case errors.Is(serviceError, ErrInvalidEfdEndpoint), errors.Is(serviceError, ErrInvalidEmail), errors.Is(serviceError, ErrReceiptFormat):
		return response.Error(c, fiber.StatusBadRequest, "invalid_setting", serviceError.Error())
	case errors.Is(serviceError, media.ErrEmptyImage), errors.Is(serviceError, media.ErrUnsupportedImage):
		return response.Error(c, fiber.StatusBadRequest, "invalid_logo", serviceError.Error())
	case errors.Is(serviceError, media.ErrImageTooLarge):
		return response.Error(c, fiber.StatusRequestEntityTooLarge, "logo_too_large", "The logo must be 1 MB or smaller")
	default:
		return serviceError
	}
}
