package server

import (
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/access"
	"github.com/chrisostomemataba/balceinv-api/internal/auth"
	"github.com/chrisostomemataba/balceinv-api/internal/catalog"
	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/httpx"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/chrisostomemataba/balceinv-api/internal/common/storage"
	"github.com/chrisostomemataba/balceinv-api/internal/discounts"
	"github.com/chrisostomemataba/balceinv-api/internal/media"
	"github.com/chrisostomemataba/balceinv-api/internal/notifications"
	"github.com/chrisostomemataba/balceinv-api/internal/platform"
	"github.com/chrisostomemataba/balceinv-api/internal/products"
	"github.com/chrisostomemataba/balceinv-api/internal/settings"
	"github.com/chrisostomemataba/balceinv-api/internal/shops"
	"github.com/chrisostomemataba/balceinv-api/internal/stock"
	"github.com/chrisostomemataba/balceinv-api/internal/tenancy"
	"github.com/chrisostomemataba/balceinv-api/internal/transfers"
	"github.com/chrisostomemataba/balceinv-api/internal/users"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
)

func registerRoutes(application *fiber.App, openDatabase *database.Database, objectStore storage.Store, supportPasscodeHash string) {
	isPostgres := openDatabase.IsPostgres()

	accessRepository := access.NewRepository()
	usersRepository := users.NewRepository()
	tenancyRepository := tenancy.NewRepository()
	settingsRepository := settings.NewRepository()

	accessService := access.NewService(accessRepository)
	usersService := users.NewService(usersRepository, accessRepository)
	tenancyService := tenancy.NewService(tenancyRepository, accessService, usersRepository, settingsRepository)
	settingsService := settings.NewService(settingsRepository, objectStore)
	stockService := stock.NewService(stock.NewRepository(), notifications.NewRepository())
	productsService := products.NewService(products.NewRepository(), stockService, objectStore)
	authService := auth.NewService(openDatabase, auth.NewRepository(), usersRepository, accessRepository, tenancyRepository)

	platformHandler := platform.NewHandler(openDatabase)
	tenancyHandler := tenancy.NewHandler(tenancyService, isPostgres)
	authHandler := auth.NewHandler(authService, isPostgres)
	usersHandler := users.NewHandler(usersService)
	accessHandler := access.NewHandler(accessService)
	settingsHandler := settings.NewHandler(settingsService)
	mediaHandler := media.NewHandler(objectStore)
	productsHandler := products.NewHandler(productsService)
	catalogHandler := catalog.NewHandler(catalog.NewService(catalog.NewRepository()))
	shopsHandler := shops.NewHandler(shops.NewService(shops.NewRepository()))
	discountsService := discounts.NewService(discounts.NewRepository())
	discountsHandler := discounts.NewHandler(discountsService)
	stockHandler := stock.NewHandler(stockService)
	transfersHandler := transfers.NewHandler(transfers.NewService(transfers.NewRepository(), stockService))
	notificationsHandler := notifications.NewHandler(notifications.NewService(notifications.NewRepository()))

	requestTransaction := httpx.RequestTransaction(openDatabase)
	authenticate := authHandler.Authenticate()
	supportPasscode := httpx.RequireSupportPasscode(supportPasscodeHash)

	signedIn := func(routeHandler fiber.Handler) []fiber.Handler {
		return []fiber.Handler{authenticate, requestTransaction, routeHandler}
	}
	permitted := func(permissionId string, routeHandler fiber.Handler) []fiber.Handler {
		return []fiber.Handler{authenticate, requestTransaction, httpx.RequirePermission(permissionId), routeHandler}
	}
	supportTeam := func(routeHandler fiber.Handler) []fiber.Handler {
		return []fiber.Handler{authenticate, supportPasscode, requestTransaction, routeHandler}
	}

	application.Get("/health", platformHandler.Health)
	application.Get("/api/platform", platformHandler.Describe)

	application.Get("/api/setup/status", requestTransaction, tenancyHandler.Status)
	application.Post("/api/setup", requestTransaction, tenancyHandler.RunFirstSetup)

	application.Post("/api/auth/login", newLoginLimiter(), authHandler.Login)
	application.Post("/api/auth/logout", signedIn(authHandler.Logout)...)
	application.Get("/api/auth/me", signedIn(authHandler.Me)...)
	application.Post("/api/auth/switch-shop", signedIn(authHandler.SwitchShop)...)

	application.Get("/api/users", permitted("users:view", usersHandler.List)...)
	application.Post("/api/users/update-password", signedIn(usersHandler.ChangePassword)...)
	application.Get("/api/users/:id", permitted("users:view", usersHandler.Get)...)
	application.Post("/api/users", permitted("users:create", usersHandler.Create)...)
	application.Put("/api/users/:id", permitted("users:edit", usersHandler.Update)...)
	application.Delete("/api/users/:id", permitted("users:delete", usersHandler.Deactivate)...)

	application.Get("/api/roles", signedIn(accessHandler.ListRoles)...)
	application.Post("/api/roles/assign", permitted("users:edit", usersHandler.AssignRole)...)
	application.Get("/api/roles/:id", signedIn(accessHandler.GetRole)...)
	application.Post("/api/roles", permitted("roles:create", accessHandler.CreateRole)...)
	application.Put("/api/roles/:id", permitted("roles:edit", accessHandler.RenameRole)...)
	application.Delete("/api/roles/:id", permitted("roles:delete", accessHandler.DeleteRole)...)

	application.Get("/api/settings", permitted("settings:view", settingsHandler.Get)...)
	application.Put("/api/settings", permitted("settings:edit", settingsHandler.Update)...)
	application.Post("/api/settings/upload-logo", permitted("settings:edit", settingsHandler.UploadLogo)...)
	application.Get("/api/media/:folder/:companyId/:fileName", mediaHandler.Serve)

	application.Get("/api/shops", permitted("shops:view", shopsHandler.List)...)
	application.Get("/api/shops/:id", permitted("shops:view", shopsHandler.Get)...)
	application.Post("/api/shops", permitted("shops:create", shopsHandler.Create)...)
	application.Put("/api/shops/:id", permitted("shops:edit", shopsHandler.Update)...)
	application.Delete("/api/shops/:id", permitted("shops:delete", shopsHandler.Close)...)

	application.Get("/api/products", permitted("products:view", productsHandler.List)...)
	application.Get("/api/products/categories", permitted("products:view", productsHandler.Categories)...)
	application.Get("/api/products/template", permitted("products:create", productsHandler.ImportTemplate)...)
	application.Post("/api/products/upload", permitted("products:create", productsHandler.Import)...)
	application.Get("/api/products/:id", permitted("products:view", productsHandler.Get)...)
	application.Get("/api/products/:id/variants", permitted("products:view", productsHandler.Variants)...)
	application.Post("/api/products", permitted("products:create", productsHandler.Create)...)
	application.Put("/api/products/:id", permitted("products:edit", productsHandler.Update)...)
	application.Delete("/api/products/:id", permitted("products:delete", productsHandler.Archive)...)
	application.Post("/api/products/:id/restore", permitted("products:edit", productsHandler.Restore)...)
	application.Post("/api/products/:id/image", permitted("products:edit", productsHandler.UploadImage)...)
	application.Get("/api/products/:id/addons", permitted("products:view", productsHandler.ListAddons)...)
	application.Post("/api/products/:id/addons", permitted("products:edit", productsHandler.CreateAddon)...)
	application.Put("/api/addons/:id", permitted("products:edit", productsHandler.UpdateAddon)...)
	application.Delete("/api/addons/:id", permitted("products:edit", productsHandler.DeleteAddon)...)

	application.Get("/api/catalog", permitted("products:create", catalogHandler.List)...)
	application.Get("/api/catalog/team/summary", supportTeam(catalogHandler.TeamSummary)...)
	application.Get("/api/catalog/team/items", supportTeam(catalogHandler.TeamItems)...)
	application.Get("/api/catalog/team/template", supportTeam(catalogHandler.TeamTemplate)...)
	application.Post("/api/catalog/team/import", supportTeam(catalogHandler.TeamImport)...)
	application.Delete("/api/catalog/team", supportTeam(catalogHandler.TeamClear)...)

	application.Get("/api/discounts", permitted("discounts:view", discountsHandler.List)...)
	application.Get("/api/discounts/:id", permitted("discounts:view", discountsHandler.Get)...)
	application.Post("/api/discounts", permitted("discounts:create", discountsHandler.Create)...)
	application.Put("/api/discounts/:id", permitted("discounts:edit", discountsHandler.Update)...)
	application.Delete("/api/discounts/:id", permitted("discounts:delete", discountsHandler.Stop)...)

	application.Get("/api/stock", permitted("stock_movements:view", stockHandler.Levels)...)
	application.Get("/api/stock/summary", permitted("stock_movements:view", stockHandler.Summary)...)
	application.Get("/api/stock-movements", permitted("stock_movements:view", stockHandler.Movements)...)
	application.Post("/api/stock-movements", permitted("stock_movements:create", stockHandler.Adjust)...)

	application.Get("/api/stock-transfers", permitted("stock_movements:view", transfersHandler.List)...)
	application.Get("/api/stock-transfers/:id", permitted("stock_movements:view", transfersHandler.Get)...)
	application.Post("/api/stock-transfers", permitted("stock_movements:create", transfersHandler.Create)...)

	application.Get("/api/notifications", permitted("notifications:view", notificationsHandler.List)...)
	application.Get("/api/notifications/unread-count", permitted("notifications:view", notificationsHandler.UnreadCount)...)
	application.Post("/api/notifications/read-all", permitted("notifications:view", notificationsHandler.MarkAllRead)...)
	application.Post("/api/notifications/:id/read", permitted("notifications:view", notificationsHandler.MarkRead)...)
	application.Delete("/api/notifications/read", permitted("notifications:view", notificationsHandler.ClearRead)...)

	application.Get("/api/permissions", signedIn(accessHandler.ListPermissions)...)
	application.Get("/api/permissions/role/:id", permitted("roles:view", accessHandler.ListRolePermissions)...)
	application.Get("/api/permissions/user/:id", signedIn(accessHandler.ListUserPermissions)...)
	application.Post("/api/permissions/assign-role", permitted("roles:edit", accessHandler.AssignRolePermissions)...)
	application.Post("/api/permissions/assign-user", permitted("users:edit", accessHandler.AssignUserPermissions)...)
}

func newLoginLimiter() fiber.Handler {
	return limiter.New(limiter.Config{
		Max:                    5,
		Expiration:             time.Minute,
		SkipSuccessfulRequests: true,
		KeyGenerator: func(c *fiber.Ctx) string {
			loginRequest := auth.LoginRequest{}
			parseError := c.BodyParser(&loginRequest)
			if parseError != nil {
				return c.IP()
			}
			return c.IP() + "|" + strings.ToLower(strings.TrimSpace(loginRequest.Email))
		},
		LimitReached: func(c *fiber.Ctx) error {
			return response.Error(c, fiber.StatusTooManyRequests, "rate_limited", "Too many sign-in attempts. Try again in a minute")
		},
	})
}
