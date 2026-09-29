package server

import (
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/access"
	"github.com/chrisostomemataba/balceinv-api/internal/auth"
	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/httpx"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/chrisostomemataba/balceinv-api/internal/platform"
	"github.com/chrisostomemataba/balceinv-api/internal/tenancy"
	"github.com/chrisostomemataba/balceinv-api/internal/users"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
)

func registerRoutes(application *fiber.App, openDatabase *database.Database) {
	isPostgres := openDatabase.IsPostgres()

	accessRepository := access.NewRepository()
	usersRepository := users.NewRepository()
	tenancyRepository := tenancy.NewRepository()

	accessService := access.NewService(accessRepository)
	usersService := users.NewService(usersRepository, accessRepository)
	tenancyService := tenancy.NewService(tenancyRepository, accessService, usersRepository)
	authService := auth.NewService(openDatabase, auth.NewRepository(), usersRepository, accessRepository, tenancyRepository)

	platformHandler := platform.NewHandler(openDatabase)
	tenancyHandler := tenancy.NewHandler(tenancyService, isPostgres)
	authHandler := auth.NewHandler(authService, isPostgres)
	usersHandler := users.NewHandler(usersService)
	accessHandler := access.NewHandler(accessService)

	requestTransaction := httpx.RequestTransaction(openDatabase)
	authenticate := authHandler.Authenticate()

	signedIn := func(routeHandler fiber.Handler) []fiber.Handler {
		return []fiber.Handler{authenticate, requestTransaction, routeHandler}
	}
	permitted := func(permissionId string, routeHandler fiber.Handler) []fiber.Handler {
		return []fiber.Handler{authenticate, requestTransaction, httpx.RequirePermission(permissionId), routeHandler}
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
