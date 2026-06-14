package main

import (
	"log"

	"github.com/chrisostomemataba/balceinv-api/config"
	"github.com/chrisostomemataba/balceinv-api/database"
	"github.com/chrisostomemataba/balceinv-api/models"
	"github.com/chrisostomemataba/balceinv-api/routes"
	"github.com/gofiber/fiber/v2"
	"github.com/gofiber/fiber/v2/middleware/cors"
	"github.com/gofiber/fiber/v2/middleware/logger"
	"github.com/gofiber/fiber/v2/middleware/recover"
	"gorm.io/gorm"
)


func main() {
	cfg := config.Load()

	db, err := database.Connect(cfg.DBPath)
	if err != nil {
		log.Fatalf("Failed to connect to database: %v", err)
	}

	if err := database.Migrate(db); err != nil {
		log.Fatalf("Failed to run migrations: %v", err)
	}

	// Seed permissions if not already seeded
	seedPermissions(db)

	app := fiber.New(fiber.Config{
		AppName: "BalceInv API",
	})

	app.Use(recover.New())
	app.Use(logger.New())

	app.Use(cors.New(cors.Config{
		AllowOrigins:     "http://localhost:3000",
		AllowMethods:     "GET,POST,PUT,DELETE,OPTIONS",
		AllowHeaders:     "Origin,Content-Type,Accept,Authorization",
		AllowCredentials: true,
		ExposeHeaders:    "Set-Cookie",
	}))

	app.Get("/health", func(c *fiber.Ctx) error {
		return c.JSON(fiber.Map{
			"status":  "ok",
			"version": "1.0.0",
		})
	})

	routes.Setup(app, db, cfg)

	log.Printf("Server starting on port %s", cfg.Port)
	if err := app.Listen(":" + cfg.Port); err != nil {
		log.Fatalf("Failed to start server: %v", err)
	}
}

func seedPermissions(db *gorm.DB) {
	// Check if permissions already exist — if yes skip entirely
	var count int64
	db.Model(&models.Permission{}).Count(&count)
	if count > 0 {
		log.Println("Permissions already seeded, skipping.")
		return
	}

	resources := []string{
		"products", "sales", "users", "roles",
		"stock_movements", "reports", "settings", "notifications",
	}
	actions := []string{"view", "create", "edit", "delete"}

	for _, resource := range resources {
		for _, action := range actions {
			name := resource + ":" + action
			perm := models.Permission{
				Name:     name,
				Resource: resource,
				Action:   action,
			}
			db.Where("name = ?", name).FirstOrCreate(&perm)
		}
	}

	log.Println("Permissions seeded.")

	// Assign to Admin role only if it exists
	var adminRole models.Role
	if err := db.Where("name = ?", "Admin").First(&adminRole).Error; err != nil {
		log.Println("Admin role not found — will be assigned after /api/setup is run.")
		return
	}

	var allPerms []models.Permission
	db.Find(&allPerms)

	for _, perm := range allPerms {
		rp := models.RolePermission{
			RoleID:       adminRole.ID,
			PermissionID: perm.ID,
		}
		db.Where("role_id = ? AND permission_id = ?", rp.RoleID, rp.PermissionID).FirstOrCreate(&rp)
	}

	log.Println("Admin permissions assigned.")
}