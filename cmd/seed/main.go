package main

import (
	"log"
	"os"

	"github.com/chrisostomemataba/balceinv-api/database"
	"github.com/chrisostomemataba/balceinv-api/models"
	"github.com/joho/godotenv"
	"gorm.io/gorm"
)

func main() {
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file, using system environment variables")
	}

	dbPath := os.Getenv("DB_PATH")
	if dbPath == "" {
		dbPath = "./balce.db"
	}

	db, err := database.Connect(dbPath)
	if err != nil {
		log.Fatalf("Database connection failed: %v", err)
	}

	if err := database.Migrate(db); err != nil {
		log.Fatalf("Migration failed: %v", err)
	}

	log.Println("Starting seed...")
	seedPermissions(db)
	log.Println("Seed complete.")
}

func seedPermissions(db *gorm.DB) {
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

	var adminRole models.Role
	if err := db.Where("name = ?", "Admin").First(&adminRole).Error; err != nil {
		log.Println("Admin role not found — run /api/setup first, then re-run this script.")
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