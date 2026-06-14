package routes

import (
	"github.com/chrisostomemataba/balceinv-api/config"
	"github.com/chrisostomemataba/balceinv-api/handlers"
	"github.com/chrisostomemataba/balceinv-api/middleware"
	"github.com/chrisostomemataba/balceinv-api/repository"
	"github.com/chrisostomemataba/balceinv-api/services"
	"github.com/gofiber/fiber/v2"
	"gorm.io/gorm"
)

func Setup(application *fiber.App, database *gorm.DB, configuration *config.Config) {
	protected := middleware.Protected(configuration.AccessTokenSecret)

	// --- Repositories ---
	userRepository := repository.NewUserRepository(database)
	roleRepository := repository.NewRoleRepository(database)
	permissionRepository := repository.NewPermissionRepository(database)
	productRepository := repository.NewProductRepository(database)
	saleRepository := repository.NewSaleRepository(database)
	stockRepository := repository.NewStockMovementRepository(database)
	settingsRepository := repository.NewSettingsRepository(database)
	notificationRepository := repository.NewNotificationRepository(database)
	reportRepository := repository.NewReportRepository(database)
	addonRepository := repository.NewAddonRepository(database)
	discountRepository := repository.NewDiscountRepository(database)

	// --- Auth ---
	authService := services.NewAuthService(database, configuration.AccessTokenSecret, configuration.RefreshTokenSecret)
	authHandler := handlers.NewAuthHandler(authService)

	authRoutes := application.Group("/api/auth")
	authRoutes.Post("/login", authHandler.Login)
	authRoutes.Post("/logout", authHandler.Logout)
	authRoutes.Post("/refresh", authHandler.Refresh)
	authRoutes.Get("/me", protected, authHandler.Me)

	// --- Users ---
	userService := services.NewUserService(userRepository, roleRepository)
	userHandler := handlers.NewUserHandler(userService)

	userRoutes := application.Group("/api/users", protected)
	userRoutes.Get("/", userHandler.GetAll)
	userRoutes.Post("/update-password", userHandler.UpdatePassword)
	userRoutes.Get("/:id", userHandler.GetByID)
	userRoutes.Post("/", userHandler.Create)
	userRoutes.Put("/:id", userHandler.Update)
	userRoutes.Delete("/:id", userHandler.Delete)

	// --- Roles ---
	roleService := services.NewRoleService(roleRepository, userRepository)
	roleHandler := handlers.NewRoleHandler(roleService)

	roleRoutes := application.Group("/api/roles", protected)
	roleRoutes.Get("/", roleHandler.GetAll)
	roleRoutes.Post("/assign", roleHandler.AssignRole)
	roleRoutes.Get("/:id", roleHandler.GetByID)
	roleRoutes.Post("/", roleHandler.Create)
	roleRoutes.Put("/:id", roleHandler.Update)
	roleRoutes.Delete("/:id", roleHandler.Delete)

	// --- Permissions ---
	permissionService := services.NewPermissionService(permissionRepository, roleRepository, userRepository)
	permissionHandler := handlers.NewPermissionHandler(permissionService)

	permissionRoutes := application.Group("/api/permissions", protected)
	permissionRoutes.Get("/", permissionHandler.GetAll)
	permissionRoutes.Get("/role/:id", permissionHandler.GetRolePermissions)
	permissionRoutes.Get("/user/:id", permissionHandler.GetUserPermissions)
	permissionRoutes.Post("/assign-role", permissionHandler.AssignToRole)
	permissionRoutes.Post("/assign-user", permissionHandler.AssignToUser)

	// --- Products ---
	productService := services.NewProductService(productRepository)
	productHandler := handlers.NewProductHandler(productService)

	addonService := services.NewAddonService(addonRepository, productRepository)
	addonHandler := handlers.NewAddonHandler(addonService)

	productRoutes := application.Group("/api/products", protected)
	productRoutes.Get("/", productHandler.GetAll)
	productRoutes.Get("/low-stock", productHandler.GetLowStock)
	productRoutes.Get("/template", productHandler.GetTemplate)
	productRoutes.Post("/upload", productHandler.UploadExcel)
	productRoutes.Get("/:id", productHandler.GetByID)
	productRoutes.Post("/", productHandler.Create)
	productRoutes.Put("/:id", productHandler.Update)
	productRoutes.Delete("/:id", productHandler.Delete)
	productRoutes.Post("/:id/image", productHandler.UpdateImage)
	productRoutes.Get("/:id/variants", productHandler.GetVariants)
	productRoutes.Get("/:id/addons", addonHandler.GetByProduct)
	productRoutes.Post("/:id/addons", addonHandler.Create)

	addonRoutes := application.Group("/api/addons", protected)
	addonRoutes.Put("/:id", addonHandler.Update)
	addonRoutes.Delete("/:id", addonHandler.Delete)

	// --- Discounts ---
	discountService := services.NewDiscountService(discountRepository)
	discountHandler := handlers.NewDiscountHandler(discountService)

	discountRoutes := application.Group("/api/discounts", protected)
	discountRoutes.Get("/", discountHandler.GetAll)
	discountRoutes.Get("/active", discountHandler.GetActiveForProduct)
	discountRoutes.Post("/", discountHandler.Create)
	discountRoutes.Put("/:id", discountHandler.Update)
	discountRoutes.Delete("/:id", discountHandler.Delete)
	discountRoutes.Post("/:id/deactivate", discountHandler.Deactivate)

	// --- Notifications ---
	notificationService := services.NewNotificationService(notificationRepository, settingsRepository, productRepository)
	notificationHandler := handlers.NewNotificationHandler(notificationService)

	notificationRoutes := application.Group("/api/notifications", protected)
	notificationRoutes.Get("/", notificationHandler.GetAll)
	notificationRoutes.Get("/count", notificationHandler.GetCount)
	notificationRoutes.Post("/mark-all-seen", notificationHandler.MarkAllAsSeen)
	notificationRoutes.Delete("/clear-seen", notificationHandler.ClearSeen)
	notificationRoutes.Post("/:id/mark-seen", notificationHandler.MarkAsSeen)
	notificationRoutes.Delete("/:id", notificationHandler.Delete)

	// --- Sales ---
	saleService := services.NewSaleService(saleRepository, productRepository, settingsRepository, notificationService)
	saleHandler := handlers.NewSaleHandler(saleService)

	saleRoutes := application.Group("/api/sales", protected)
	saleRoutes.Get("/", saleHandler.GetAll)
	saleRoutes.Get("/daily", saleHandler.GetDaily)
	saleRoutes.Get("/monthly", saleHandler.GetMonthly)
	saleRoutes.Get("/date-range", saleHandler.GetByDateRange)
	saleRoutes.Get("/:id", saleHandler.GetByID)
	saleRoutes.Post("/", saleHandler.Create)

	// --- Stock Movements ---
	stockService := services.NewStockMovementService(stockRepository, productRepository)
	stockHandler := handlers.NewStockMovementHandler(stockService)

	stockRoutes := application.Group("/api/stock-movements", protected)
	stockRoutes.Get("/", stockHandler.GetAll)
	stockRoutes.Get("/summary", stockHandler.GetSummary)
	stockRoutes.Get("/date-range", stockHandler.GetByDateRange)
	stockRoutes.Get("/product/:id", stockHandler.GetByProduct)
	stockRoutes.Get("/:id", stockHandler.GetByID)
	stockRoutes.Post("/", stockHandler.Create)

	// --- Settings ---
	settingsService := services.NewSettingsService(settingsRepository)
	settingsHandler := handlers.NewSettingsHandler(settingsService)

	settingsRoutes := application.Group("/api/settings", protected)
	settingsRoutes.Get("/", settingsHandler.Get)
	settingsRoutes.Put("/", settingsHandler.Update)
	settingsRoutes.Post("/test-efd", settingsHandler.TestEFD)
	settingsRoutes.Post("/upload-logo", settingsHandler.UploadLogo)

	// --- Print ---
	printService := services.NewPrintService(saleRepository, settingsRepository)
	printHandler := handlers.NewPrintHandler(printService, settingsService)

	printRoutes := application.Group("/api/print", protected)
	printRoutes.Post("/receipt", printHandler.PrintReceipt)
	printRoutes.Get("/status", printHandler.PrinterStatus)

	// --- Reports ---
	reportService := services.NewReportService(reportRepository)
	reportHandler := handlers.NewReportHandler(reportService)

	reportRoutes := application.Group("/api/reports", protected)
	reportRoutes.Get("/sales-summary", reportHandler.GetSalesSummary)
	reportRoutes.Get("/top-products", reportHandler.GetTopProducts)
	reportRoutes.Get("/sales-by-user", reportHandler.GetSalesByUser)
	reportRoutes.Get("/inventory", reportHandler.GetInventory)
	reportRoutes.Get("/financial", reportHandler.GetFinancial)
	reportRoutes.Get("/daily-trend", reportHandler.GetDailyTrend)

	// --- Dashboard ---
	dashboardHandler := handlers.NewDashboardHandler(database)
	application.Get("/api/dashboard", protected, dashboardHandler.Get)

	// --- Setup ---
	setupHandler := handlers.NewSetupHandler(services.NewSetupService(database))
	application.Get("/api/setup/status", setupHandler.Status)
	application.Post("/api/setup", setupHandler.Run)

	// --- Catalog ---
	catalogHandler := handlers.NewCatalogHandler(database)
	application.Get("/api/catalog", protected, catalogHandler.GetAll)
}
