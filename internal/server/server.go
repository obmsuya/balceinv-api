package server

import (
	"github.com/chrisostomemataba/balceinv-api/internal/businessmove"
	"strings"

	"github.com/chrisostomemataba/balceinv-api/internal/backup"
	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/httpx"
	"github.com/chrisostomemataba/balceinv-api/internal/common/storage"
	"github.com/chrisostomemataba/balceinv-api/internal/config"
	"github.com/chrisostomemataba/balceinv-api/internal/lan"
	"github.com/chrisostomemataba/balceinv-api/internal/licensing"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/helmet"
	"github.com/gofiber/fiber/v2/middleware/recover"
)

type Desktop struct {
	Backups *backup.Store
	Network *lan.Controller
}

func New(loadedConfig *config.Config, openDatabase *database.Database, objectStore storage.Store, writeLogSeparator func(), desktop Desktop) *fiber.App {
	application := fiber.New(fiber.Config{
		AppName:               "Balce API",
		ErrorHandler:          httpx.ErrorHandler,
		DisableStartupMessage: true,
		BodyLimit:             businessmove.PackageSizeLimit + 1<<20,

		EnableTrustedProxyCheck: true,
		TrustedProxies:          loadedConfig.TrustedProxies,
		ProxyHeader:             loadedConfig.ProxyHeader,
	})

	application.Use(httpx.RequestLogging(writeLogSeparator))
	application.Use(recover.New())
	application.Use(helmet.New())
	application.Use(cors.New(cors.Config{
		AllowOrigins:     strings.Join(loadedConfig.AllowedOrigins, ","),
		AllowMethods:     "GET,POST,PUT,PATCH,DELETE,OPTIONS",
		AllowHeaders:     "Origin,Content-Type,Accept,Authorization," + httpx.SupportPasscodeHeader + "," + httpx.DesktopClientHeader,
		AllowCredentials: true,
		ExposeHeaders:    "X-Request-Id",
	}))

	if loadedConfig.IsDesktop() {
		application.Use(httpx.DesktopHostGuard())
	}
	application.Use(httpx.OriginGuard(loadedConfig.AllowedOrigins))
	if loadedConfig.EnforceLicense {
		application.Use(licensing.Enforce())
	}

	registerRoutes(application, loadedConfig, openDatabase, objectStore, desktop)

	return application
}
