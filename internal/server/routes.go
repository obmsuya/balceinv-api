package server

import (
	"github.com/chrisostomemataba/balceinv-api/internal/features"
	"path/filepath"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/access"
	"github.com/chrisostomemataba/balceinv-api/internal/accounting"
	"github.com/chrisostomemataba/balceinv-api/internal/auth"
	"github.com/chrisostomemataba/balceinv-api/internal/backup"
	"github.com/chrisostomemataba/balceinv-api/internal/catalog"
	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/httpx"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/chrisostomemataba/balceinv-api/internal/common/storage"
	"github.com/chrisostomemataba/balceinv-api/internal/config"
	"github.com/chrisostomemataba/balceinv-api/internal/customers"
	"github.com/chrisostomemataba/balceinv-api/internal/discounts"
	"github.com/chrisostomemataba/balceinv-api/internal/invoices"
	"github.com/chrisostomemataba/balceinv-api/internal/legacyimport"
	"github.com/chrisostomemataba/balceinv-api/internal/licensing"
	"github.com/chrisostomemataba/balceinv-api/internal/media"
	"github.com/chrisostomemataba/balceinv-api/internal/notifications"
	"github.com/chrisostomemataba/balceinv-api/internal/orders"
	"github.com/chrisostomemataba/balceinv-api/internal/phoneupload"
	"github.com/chrisostomemataba/balceinv-api/internal/platform"
	"github.com/chrisostomemataba/balceinv-api/internal/printing"
	"github.com/chrisostomemataba/balceinv-api/internal/products"
	"github.com/chrisostomemataba/balceinv-api/internal/rates"
	"github.com/chrisostomemataba/balceinv-api/internal/reports"
	"github.com/chrisostomemataba/balceinv-api/internal/sales"
	"github.com/chrisostomemataba/balceinv-api/internal/settings"
	"github.com/chrisostomemataba/balceinv-api/internal/shops"
	"github.com/chrisostomemataba/balceinv-api/internal/stock"
	"github.com/chrisostomemataba/balceinv-api/internal/suppliers"
	"github.com/chrisostomemataba/balceinv-api/internal/support"
	"github.com/chrisostomemataba/balceinv-api/internal/tenancy"
	"github.com/chrisostomemataba/balceinv-api/internal/transfers"
	"github.com/chrisostomemataba/balceinv-api/internal/users"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/limiter"
)

func registerRoutes(application *fiber.App, loadedConfig *config.Config, openDatabase *database.Database, objectStore storage.Store, desktop Desktop) {
	supportPasscodeHash := loadedConfig.SupportPasscodeHash
	isPostgres := openDatabase.IsPostgres()

	accessRepository := access.NewRepository()
	usersRepository := users.NewRepository()
	tenancyRepository := tenancy.NewRepository()
	settingsRepository := settings.NewRepository()

	accessService := access.NewService(accessRepository)
	usersService := users.NewService(usersRepository, accessRepository)
	tenancyService := tenancy.NewService(tenancyRepository, accessService, usersRepository, settingsRepository)
	settingsService := settings.NewService(settingsRepository, objectStore)
	accountingRepository := accounting.NewRepository()
	ledger := accounting.NewLedger(accountingRepository)
	stockService := stock.NewService(stock.NewRepository(), notifications.NewRepository(), ledger)
	productsService := products.NewService(products.NewRepository(), stockService, objectStore)
	featuresRepository := features.NewRepository()
	authService := auth.NewService(openDatabase, auth.NewRepository(), usersRepository, accessRepository, tenancyRepository, featuresRepository)
	featuresHandler := features.NewHandler(features.NewService(featuresRepository))

	platformHandler := platform.NewHandler(openDatabase, desktop.Network)
	oldDatabasePath := ""
	oldAppDatabasePath := filepath.Join(loadedConfig.DataDirectory, "balce.db")
	isOldAppDatabaseElsewhere := oldAppDatabasePath != loadedConfig.SqlitePath
	if loadedConfig.IsDesktop() && isOldAppDatabaseElsewhere {
		oldDatabasePath = oldAppDatabasePath
	}
	tenancyHandler := tenancy.NewHandler(tenancyService, isPostgres, oldDatabasePath)
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
	salesRepository := sales.NewRepository()
	customersService := customers.NewService(customers.NewRepository(), featuresRepository, settingsRepository, ledger, objectStore)
	customersHandler := customers.NewHandler(customersService)
	salesService := sales.NewService(salesRepository, discountsService, settingsRepository, stockService, customersService, ledger)
	salesHandler := sales.NewHandler(salesService, sales.NewFiscalService(openDatabase, salesService, salesRepository, settingsRepository))
	invoicesHandler := invoices.NewHandler(invoices.NewService(salesService, objectStore))
	stockHandler := stock.NewHandler(stockService)
	ratesHandler := rates.NewHandler(rates.NewService(openDatabase, rates.NewRepository()), settingsRepository)
	reportsHandler := reports.NewHandler(reports.NewService(reports.NewRepository(isPostgres), settingsRepository, objectStore))
	transfersHandler := transfers.NewHandler(transfers.NewService(transfers.NewRepository(), stockService, ledger))
	notificationsHandler := notifications.NewHandler(notifications.NewService(notifications.NewRepository()))
	accountingHandler := accounting.NewHandler(accounting.NewService(accountingRepository, ledger, objectStore))
	suppliersHandler := suppliers.NewHandler(suppliers.NewService(suppliers.NewRepository(), featuresRepository, settingsRepository, stockService, objectStore, ledger))

	requestTransaction := httpx.RequestTransaction(openDatabase)
	authenticate := authHandler.Authenticate()
	supportPasscode := httpx.RequireSupportPasscode(supportPasscodeHash)

	signedIn := func(routeHandler fiber.Handler) []fiber.Handler {
		return []fiber.Handler{authenticate, requestTransaction, routeHandler}
	}
	permitted := func(permissionId string, routeHandler fiber.Handler) []fiber.Handler {
		return []fiber.Handler{authenticate, requestTransaction, httpx.RequirePermission(permissionId), routeHandler}
	}
	permittedAny := func(permissionIds []string, routeHandler fiber.Handler) []fiber.Handler {
		return []fiber.Handler{authenticate, requestTransaction, httpx.RequirePermission(permissionIds...), routeHandler}
	}
	books := func(permissionId string, routeHandler fiber.Handler) []fiber.Handler {
		return []fiber.Handler{authenticate, requestTransaction, httpx.RequirePermission(permissionId), accountingHandler.RequireAccounting(false), routeHandler}
	}
	fullBooks := func(permissionId string, routeHandler fiber.Handler) []fiber.Handler {
		return []fiber.Handler{authenticate, requestTransaction, httpx.RequirePermission(permissionId), accountingHandler.RequireAccounting(true), routeHandler}
	}
	supportTeam := func(routeHandler fiber.Handler) []fiber.Handler {
		return []fiber.Handler{authenticate, supportPasscode, requestTransaction, routeHandler}
	}

	application.Get("/health", platformHandler.Health)
	application.Get("/api/platform", platformHandler.Describe)

	application.Get("/api/setup/status", requestTransaction, tenancyHandler.Status)
	if loadedConfig.IsDesktop() {
		application.Post("/api/setup", licensing.IssueTrialAfterSetup(), requestTransaction, tenancyHandler.RunFirstSetup)
		legacyImportHandler := legacyimport.NewHandler(legacyimport.NewService(tenancyService, stockService), oldDatabasePath)
		application.Get("/api/setup/import-old/preview", requestTransaction, legacyImportHandler.Preview)
		application.Post("/api/setup/import-old", licensing.IssueTrialAfterSetup(), requestTransaction, legacyImportHandler.Import)
		application.Get("/api/license/status", authenticate, licensing.Status)
		application.Post("/api/license/refresh", authenticate, licensing.Refresh)
		application.Get("/api/license/hardware-id", authenticate, licensing.HardwareId)
		application.Get("/api/license/packages", authenticate, licensing.Packages)
		application.Post("/api/license/pay", authenticate, licensing.Pay)
	} else {
		application.Post("/api/setup", newSignupLimiter(), requestTransaction, tenancyHandler.RunFirstSetup)
	}

	application.Post("/api/auth/login", newLoginAddressLimiter(), newLoginLimiter(), authHandler.Login)
	application.Post("/api/auth/logout", signedIn(authHandler.Logout)...)
	application.Get("/api/auth/me", signedIn(authHandler.Me)...)
	application.Post("/api/auth/switch-shop", signedIn(authHandler.SwitchShop)...)
	application.Put("/api/auth/language", signedIn(authHandler.SetLanguage)...)
	application.Put("/api/auth/tours", signedIn(authHandler.MarkTourSeen)...)

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

	application.Get("/api/features", signedIn(featuresHandler.Get)...)
	application.Put("/api/features", permitted("settings:edit", featuresHandler.Update)...)
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
	application.Get("/api/products/lookup", permitted("products:view", productsHandler.Lookup)...)
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
	if !isPostgres {
		application.Get("/api/catalog/team/summary", supportTeam(catalogHandler.TeamSummary)...)
		application.Get("/api/catalog/team/items", supportTeam(catalogHandler.TeamItems)...)
		application.Get("/api/catalog/team/template", supportTeam(catalogHandler.TeamTemplate)...)
		application.Post("/api/catalog/team/import", supportTeam(catalogHandler.TeamImport)...)
		application.Delete("/api/catalog/team", supportTeam(catalogHandler.TeamClear)...)
	}

	application.Get("/api/discounts", permitted("discounts:view", discountsHandler.List)...)
	application.Get("/api/discounts/:id", permitted("discounts:view", discountsHandler.Get)...)
	application.Post("/api/discounts", permitted("discounts:create", discountsHandler.Create)...)
	application.Put("/api/discounts/:id", permitted("discounts:edit", discountsHandler.Update)...)
	application.Delete("/api/discounts/:id", permitted("discounts:delete", discountsHandler.Stop)...)

	sellingOrViewing := []string{"sales:create", "sales:view"}
	application.Get("/api/sales/till", permitted("sales:create", salesHandler.TillOptions)...)
	application.Post("/api/sales/quote", permitted("sales:create", salesHandler.Quote)...)
	application.Post("/api/sales", permitted("sales:create", salesHandler.Create)...)
	application.Get("/api/sales", permitted("sales:view", salesHandler.List)...)
	application.Get("/api/sales/totals", permitted("sales:view", salesHandler.Totals)...)
	application.Get("/api/sales/:id", permittedAny(sellingOrViewing, salesHandler.Get)...)
	application.Get("/api/dashboard", permitted("reports:view", reportsHandler.Dashboard)...)

	customersOn := customers.FeatureGate(featuresRepository, customers.CustomersOn)
	viewingOrSelling := []string{"customers:view", "sales:create"}
	application.Get("/api/customers", permittedAny(viewingOrSelling, customersOn(customersHandler.List))...)
	application.Post("/api/customers", permitted("customers:create", customersOn(customersHandler.Create))...)
	application.Get("/api/customers/debtors", permitted("customers:view", customersOn(customersHandler.Debtors))...)
	application.Get("/api/customers/:id", permittedAny(viewingOrSelling, customersOn(customersHandler.Get))...)
	application.Put("/api/customers/:id", permitted("customers:edit", customersOn(customersHandler.Update))...)
	application.Delete("/api/customers/:id", permitted("customers:delete", customersOn(customersHandler.Deactivate))...)
	application.Post("/api/customers/:id/restore", permitted("customers:edit", customersOn(customersHandler.Restore))...)
	application.Get("/api/customers/:id/sales", permitted("customers:view", customersOn(customersHandler.Sales))...)
	application.Get("/api/customers/:id/statement", permitted("customers:view", customersOn(customersHandler.Statement))...)
	application.Get("/api/customers/:id/payments", permitted("customers:view", customersOn(customersHandler.Payments))...)
	application.Post("/api/customers/:id/payments", permitted("customers:edit", customersOn(customersHandler.RecordPayment))...)
	application.Post("/api/customers/:id/payments/:paymentId/void", permitted("customers:edit", customersOn(customersHandler.VoidPayment))...)

	ordersOn := customers.FeatureGate(featuresRepository, customers.OrdersOn)
	ordersHandler := orders.NewHandler(orders.NewService(orders.NewRepository(), salesService, customersService, stockService, ledger))
	application.Get("/api/orders", permitted("orders:view", ordersOn(ordersHandler.List))...)
	application.Post("/api/orders", permitted("orders:create", ordersOn(ordersHandler.Create))...)
	application.Get("/api/orders/:id", permitted("orders:view", ordersOn(ordersHandler.Get))...)
	application.Post("/api/orders/:id/deposits", permitted("orders:edit", ordersOn(ordersHandler.AddDeposit))...)
	application.Post("/api/orders/:id/ready", permitted("orders:edit", ordersOn(ordersHandler.MarkReady))...)
	application.Post("/api/orders/:id/collect", permitted("orders:edit", ordersOn(ordersHandler.Collect))...)
	application.Post("/api/orders/:id/cancel", permitted("orders:delete", ordersOn(ordersHandler.Cancel))...)
	application.Get("/api/exchange-rates", signedIn(ratesHandler.Latest)...)

	if loadedConfig.IsDesktop() {
		printingHandler := printing.NewHandler(printing.NewService(openDatabase, salesService, settingsRepository, objectStore))
		application.Get("/api/print/status", authenticate, httpx.RequirePermission("sales:create", "sales:view", "settings:view"), printingHandler.Status)
		application.Get("/api/print/devices", authenticate, httpx.RequirePermission("settings:edit"), printingHandler.Devices)
		application.Post("/api/print/test", authenticate, httpx.RequirePermission("settings:edit"), printingHandler.Test)
		application.Post("/api/print/receipt", authenticate, httpx.RequirePermission("sales:create", "sales:view"), printingHandler.Receipt)
	}

	phoneUploadHandler := phoneupload.NewHandler(phoneupload.NewService(), desktop.Network, isPostgres)
	application.Post("/api/phone-uploads", authenticate, httpx.RequirePermission("products:create", "products:edit", "purchases:create", "purchases:edit", "accounting:create"), phoneUploadHandler.Create)
	application.Get("/api/phone-uploads/:token", authenticate, phoneUploadHandler.Collect)
	application.Get("/upload/:token", phoneUploadHandler.Page)
	application.Post("/upload/:token", newPhoneUploadLimiter(), phoneUploadHandler.Submit)

	if desktop.Network != nil {
		application.Put("/api/platform/network", authenticate, platformHandler.SetNetwork)
	}

	if desktop.Backups != nil {
		backupHandler := backup.NewHandler(desktop.Backups)
		withoutTransaction := func(permissionId string, routeHandler fiber.Handler) []fiber.Handler {
			return []fiber.Handler{authenticate, httpx.RequirePermission(permissionId), routeHandler}
		}
		application.Get("/api/backup/status", withoutTransaction("settings:view", backupHandler.Status)...)
		application.Post("/api/backup/local", withoutTransaction("settings:edit", backupHandler.BackupOnThisComputer)...)
		application.Post("/api/backup/local/restore", withoutTransaction("settings:edit", backupHandler.RestoreFromThisComputer)...)
		application.Post("/api/backup/export", withoutTransaction("settings:edit", backupHandler.ExportToFile)...)
		application.Post("/api/backup/import", withoutTransaction("settings:edit", backupHandler.RestoreFromFile)...)
		application.Get("/api/backup/cloud", withoutTransaction("settings:view", backupHandler.ListCloud)...)
		application.Post("/api/backup/cloud", withoutTransaction("settings:edit", backupHandler.BackupToCloud)...)
		application.Post("/api/backup/cloud/restore", withoutTransaction("settings:edit", backupHandler.RestoreFromCloud)...)
	}
	application.Get("/api/reports/summary", permitted("reports:view", reportsHandler.Summary)...)
	application.Get("/api/reports/daily", permitted("reports:view", reportsHandler.Daily)...)
	application.Get("/api/reports/products", permitted("reports:view", reportsHandler.Products)...)
	application.Get("/api/reports/cashiers", permitted("reports:view", reportsHandler.Cashiers)...)
	application.Get("/api/reports/shops", permitted("reports:view", reportsHandler.Shops)...)
	application.Get("/api/reports/inventory", permitted("reports:view", reportsHandler.Inventory)...)
	application.Get("/api/reports/:report/export", permitted("reports:view", reportsHandler.Export)...)

	application.Post("/api/sales/fiscal/send-waiting", authenticate, httpx.RequirePermission(sellingOrViewing...), salesHandler.SendWaitingToEfd)
	application.Post("/api/sales/:id/fiscal", authenticate, httpx.RequirePermission(sellingOrViewing...), salesHandler.SendToEfd)
	application.Get("/api/sales/:id/receipt", permittedAny(sellingOrViewing, salesHandler.Receipt)...)
	application.Get("/api/sales/:id/document", permittedAny(sellingOrViewing, invoicesHandler.SaleDocument)...)

	application.Get("/api/stock", permittedAny([]string{"stock_movements:view", "purchases:create", "purchases:edit"}, stockHandler.Levels)...)
	application.Get("/api/stock/summary", permitted("stock_movements:view", stockHandler.Summary)...)
	application.Get("/api/stock-movements", permitted("stock_movements:view", stockHandler.Movements)...)
	application.Post("/api/stock-movements", permitted("stock_movements:create", stockHandler.Adjust)...)

	application.Get("/api/stock-transfers", permitted("stock_movements:view", transfersHandler.List)...)
	application.Get("/api/stock-transfers/:id", permitted("stock_movements:view", transfersHandler.Get)...)
	application.Post("/api/stock-transfers", permitted("stock_movements:create", transfersHandler.Create)...)

	application.Get("/api/suppliers", permitted("suppliers:view", suppliersHandler.ListSuppliers)...)
	application.Get("/api/suppliers/aging", permitted("suppliers:view", suppliersHandler.Aging)...)
	application.Get("/api/suppliers/:id", permitted("suppliers:view", suppliersHandler.GetSupplier)...)
	application.Get("/api/suppliers/:id/statement", permitted("suppliers:view", suppliersHandler.Statement)...)
	application.Post("/api/suppliers", permitted("suppliers:create", suppliersHandler.CreateSupplier)...)
	application.Put("/api/suppliers/:id", permitted("suppliers:edit", suppliersHandler.UpdateSupplier)...)
	application.Delete("/api/suppliers/:id", permitted("suppliers:delete", suppliersHandler.DeactivateSupplier)...)

	recordingPurchases := []string{"purchases:create", "purchases:edit"}
	application.Get("/api/purchases", permitted("purchases:view", suppliersHandler.ListPurchases)...)
	application.Get("/api/purchases/last-cost", permittedAny(recordingPurchases, suppliersHandler.LastCost)...)
	application.Get("/api/purchases/vat-rate", permittedAny(recordingPurchases, suppliersHandler.VatRate)...)
	application.Get("/api/purchases/:id", permitted("purchases:view", suppliersHandler.GetPurchase)...)
	application.Post("/api/purchases", permitted("purchases:create", suppliersHandler.RecordPurchase)...)
	application.Post("/api/purchases/:id/cancel", permitted("purchases:delete", suppliersHandler.CancelPurchase)...)
	application.Post("/api/purchases/:id/attachment", permittedAny(recordingPurchases, suppliersHandler.AttachInvoicePhoto)...)

	application.Get("/api/supplier-payments", permitted("purchases:view", suppliersHandler.ListPayments)...)
	application.Post("/api/supplier-payments", permitted("purchases:edit", suppliersHandler.RecordPayment)...)
	application.Post("/api/supplier-payments/:id/void", permitted("purchases:delete", suppliersHandler.VoidPayment)...)

	application.Get("/api/supplier-returns", permitted("purchases:view", suppliersHandler.ListReturns)...)
	application.Get("/api/supplier-returns/:id", permitted("purchases:view", suppliersHandler.GetReturn)...)
	application.Post("/api/supplier-returns", permitted("purchases:edit", suppliersHandler.RecordReturn)...)

	application.Get("/api/purchase-orders", permitted("purchases:view", suppliersHandler.ListOrders)...)
	application.Get("/api/purchase-orders/:id", permitted("purchases:view", suppliersHandler.GetOrder)...)
	application.Post("/api/purchase-orders", permitted("purchases:edit", suppliersHandler.CreateOrder)...)
	application.Post("/api/purchase-orders/:id/send", permitted("purchases:edit", suppliersHandler.SendOrder)...)
	application.Post("/api/purchase-orders/:id/cancel", permitted("purchases:delete", suppliersHandler.CancelOrder)...)

	application.Get("/api/notifications", permitted("notifications:view", notificationsHandler.List)...)
	application.Get("/api/notifications/unread-count", permitted("notifications:view", notificationsHandler.UnreadCount)...)
	application.Post("/api/notifications/read-all", permitted("notifications:view", notificationsHandler.MarkAllRead)...)
	application.Post("/api/notifications/:id/read", permitted("notifications:view", notificationsHandler.MarkRead)...)
	application.Delete("/api/notifications/read", permitted("notifications:view", notificationsHandler.ClearRead)...)

	application.Get("/api/accounting/status", books("accounting:view", accountingHandler.Status)...)
	application.Post("/api/accounting/start", books("accounting:edit", accountingHandler.Start)...)
	application.Post("/api/accounting/catch-up", books("accounting:edit", accountingHandler.CatchUp)...)
	application.Get("/api/accounting/accounts", books("accounting:view", accountingHandler.Accounts)...)
	application.Post("/api/accounting/accounts", fullBooks("accounting:edit", accountingHandler.CreateAccount)...)
	application.Put("/api/accounting/accounts/:id", fullBooks("accounting:edit", accountingHandler.UpdateAccount)...)
	application.Get("/api/accounting/entries", books("accounting:view", accountingHandler.Entries)...)
	application.Get("/api/accounting/entries/:id", books("accounting:view", accountingHandler.Entry)...)
	application.Get("/api/accounting/entries/:id/receipt", books("accounting:view", accountingHandler.Receipt)...)
	application.Post("/api/accounting/entries/:id/reverse", books("accounting:delete", accountingHandler.Reverse)...)
	application.Post("/api/accounting/money", books("accounting:create", accountingHandler.RecordMoney)...)
	application.Post("/api/accounting/receipts", books("accounting:create", accountingHandler.UploadReceipt)...)
	application.Post("/api/accounting/manual", fullBooks("accounting:edit", accountingHandler.PostManual)...)
	application.Post("/api/accounting/close", fullBooks("accounting:edit", accountingHandler.ClosePeriod)...)
	application.Get("/api/accounting/overview", books("accounting:view", accountingHandler.Overview)...)
	application.Get("/api/accounting/profit-and-loss", books("accounting:view", accountingHandler.ProfitAndLoss)...)
	application.Get("/api/accounting/balance-sheet", books("accounting:view", accountingHandler.BalanceSheet)...)
	application.Get("/api/accounting/statement", books("accounting:view", accountingHandler.Statement)...)
	application.Get("/api/accounting/vat", books("accounting:view", accountingHandler.VatReport)...)
	application.Get("/api/accounting/trial-balance", fullBooks("accounting:view", accountingHandler.TrialBalance)...)
	application.Get("/api/accounting/integrity", fullBooks("accounting:view", accountingHandler.Integrity)...)

	supportHandler := support.NewHandler(support.NewService(openDatabase, loadedConfig, objectStore))
	application.Post("/api/support", authenticate, supportHandler.Submit)
	application.Get("/api/support/messages", signedIn(supportHandler.Messages)...)
	application.Get("/api/support/status", signedIn(supportHandler.Status)...)

	application.Get("/api/permissions", signedIn(accessHandler.ListPermissions)...)
	application.Get("/api/permissions/role/:id", permitted("roles:view", accessHandler.ListRolePermissions)...)
	application.Get("/api/permissions/user/:id", signedIn(accessHandler.ListUserPermissions)...)
	application.Post("/api/permissions/assign-role", permitted("roles:edit", accessHandler.AssignRolePermissions)...)
	application.Post("/api/permissions/assign-user", permitted("users:edit", accessHandler.AssignUserPermissions)...)

	if loadedConfig.StaticDirectory != "" {
		application.Get("/*", staticApp(loadedConfig.StaticDirectory))
	}
}

func newPhoneUploadLimiter() fiber.Handler {
	return limiter.New(limiter.Config{
		Max:        20,
		Expiration: time.Minute,
		LimitReached: func(c *fiber.Ctx) error {
			return response.Error(c, fiber.StatusTooManyRequests, "rate_limited", "Too many photos. Try again in a minute")
		},
	})
}

func newSignupLimiter() fiber.Handler {
	return limiter.New(limiter.Config{
		Max:        10,
		Expiration: time.Hour,
		KeyGenerator: func(c *fiber.Ctx) string {
			return c.IP()
		},
		LimitReached: func(c *fiber.Ctx) error {
			return response.Error(c, fiber.StatusTooManyRequests, "rate_limited", "Too many new businesses from this network. Try again in an hour")
		},
	})
}

func newLoginAddressLimiter() fiber.Handler {
	return limiter.New(limiter.Config{
		Max:                    30,
		Expiration:             time.Minute,
		SkipSuccessfulRequests: true,
		KeyGenerator: func(c *fiber.Ctx) string {
			return c.IP()
		},
		LimitReached: func(c *fiber.Ctx) error {
			return response.Error(c, fiber.StatusTooManyRequests, "rate_limited", "Too many sign-in attempts. Try again in a minute")
		},
	})
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
