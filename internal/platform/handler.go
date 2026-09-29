package platform

import (
	"context"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/gofiber/fiber/v2"
)

type Handler struct {
	openDatabase *database.Database
}

func NewHandler(openDatabase *database.Database) *Handler {
	return &Handler{
		openDatabase: openDatabase,
	}
}

func (handler *Handler) Health(c *fiber.Ctx) error {
	pingContext, cancelPing := context.WithTimeout(c.UserContext(), 2*time.Second)
	defer cancelPing()

	pingError := handler.openDatabase.Writer.PingContext(pingContext)
	if pingError != nil {
		return response.Error(c, fiber.StatusServiceUnavailable, "database_unreachable", "Database is not reachable")
	}

	return response.Success(c, "ok", fiber.Map{
		"engine": handler.openDatabase.Engine,
	})
}

func (handler *Handler) Describe(c *fiber.Ctx) error {
	platformMode := "desktop"
	if handler.openDatabase.IsPostgres() {
		platformMode = "cloud"
	}

	return response.Success(c, "Platform", fiber.Map{
		"mode":        platformMode,
		"lan_enabled": false,
		"lan_urls":    []string{},
	})
}
