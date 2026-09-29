package tenancy

import (
	"errors"

	"github.com/chrisostomemataba/balceinv-api/internal/common/httpx"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/gofiber/fiber/v2"
)

type Handler struct {
	service         *Service
	isPostgres      bool
	oldDatabasePath string
}

func NewHandler(service *Service, isPostgres bool, oldDatabasePath string) *Handler {
	return &Handler{
		service:         service,
		isPostgres:      isPostgres,
		oldDatabasePath: oldDatabasePath,
	}
}

func (handler *Handler) Status(c *fiber.Ctx) error {
	if handler.isPostgres {
		return response.Success(c, "Setup status", SetupStatusView{Configured: true})
	}

	isConfigured, checkError := handler.service.IsConfigured(c.UserContext(), httpx.RequestQuerier(c))
	if checkError != nil {
		return checkError
	}
	return response.Success(c, "Setup status", SetupStatusView{
		Configured:   isConfigured,
		OldDataFound: !isConfigured && OldDesktopDataExists(handler.oldDatabasePath),
	})
}

func (handler *Handler) RunFirstSetup(c *fiber.Ctx) error {
	if handler.isPostgres {
		return response.Error(c, fiber.StatusNotFound, "not_found", "Companies are created by the platform administrator")
	}

	request := SetupRequest{}
	isValid, bindResponseError := httpx.BindAndValidate(c, &request)
	if !isValid {
		return bindResponseError
	}

	setupResult, setupError := handler.service.RunFirstSetup(c.UserContext(), httpx.RequestQuerier(c), handler.isPostgres, request)
	if setupError != nil {
		if errors.Is(setupError, ErrAlreadyConfigured) {
			return response.Error(c, fiber.StatusConflict, "already_configured", setupError.Error())
		}
		if errors.Is(setupError, ErrEmailTaken) {
			return response.Error(c, fiber.StatusConflict, "email_taken", setupError.Error())
		}
		return setupError
	}

	return response.Created(c, "Setup complete. You can now sign in.", setupResult)
}
