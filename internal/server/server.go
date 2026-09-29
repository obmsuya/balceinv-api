package server

import (
	"strings"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/httpx"
	"github.com/chrisostomemataba/balceinv-api/internal/config"
	"github.com/chrisostomemataba/balceinv-api/internal/platform"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/helmet"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

func New(loadedConfig *config.Config, openDatabase *database.Database, writeLogSeparator func()) *fiber.App {
	application := fiber.New(fiber.Config{
		AppName:               "Balce API",
		ErrorHandler:          httpx.ErrorHandler,
		DisableStartupMessage: true,
		BodyLimit:             8 * 1024 * 1024,
	})

	application.Use(httpx.RequestLogging(writeLogSeparator))
	application.Use(recover.New())
	application.Use(helmet.New())
	application.Use(cors.New(cors.Config{
		AllowOrigins:     strings.Join(loadedConfig.AllowedOrigins, ","),
		AllowMethods:     "GET,POST,PUT,PATCH,DELETE,OPTIONS",
		AllowHeaders:     "Origin,Content-Type,Accept,Authorization",
		AllowCredentials: true,
		ExposeHeaders:    "X-Request-Id",
	}))

	platformHandler := platform.NewHandler(openDatabase)
	application.Get("/health", platformHandler.Health)

	return application
}
