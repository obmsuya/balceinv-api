package database

import (
	"log"

	"github.com/chrisostomemataba/balceinv-api/models"
	"gorm.io/gorm"
)

// Migrate creates any tables that do not exist yet and adds any missing columns.
// It never drops columns or tables, so running this against an existing
// database is safe — existing data is untouched.
//
// FK constraints are disabled for the duration of the migration because
// SQLite cannot ALTER a table that other tables reference. GORM handles this
// by dropping and recreating the table, which requires FK enforcement to be
// off. Constraints are re-enabled immediately after AutoMigrate completes.
func Migrate(db *gorm.DB) error {
	log.Println("Running migrations...")

	sqlDB, err := db.DB()
	if err != nil {
		return err
	}

	if _, err = sqlDB.Exec("PRAGMA foreign_keys=OFF;"); err != nil {
		log.Printf("Warning: could not disable foreign keys for migration: %v", err)
	}

	err = db.AutoMigrate(
		// Level 1 — no foreign keys, migrate first
		&models.Company{},
		&models.Role{},
		&models.Permission{},
		&models.Supplier{},

		// Level 2 — depends on Role and Company
		&models.User{},

		// Level 3 — depends on Role + Permission
		&models.RolePermission{},

		// Level 3 — depends on User + Permission
		&models.UserPermission{},

		// Level 3 — depends on User + Company
		&models.Session{},
		&models.LoginLog{},
		&models.Settings{},

		// Catalog — standalone reference data, no FK dependencies
		&models.CatalogProduct{},

		// Level 3 — depends on Supplier + User
		&models.Purchase{},

		// Level 4 — depends on Purchase + Product (Product must come first)
		// Product migrated before PurchaseItem intentionally
		&models.Product{},

		// Level 4 — depends on Product
		&models.ProductAddon{},
		&models.Barcode{},
		&models.PriceHistory{},
		&models.StockAlert{},

		// Level 4 — depends on Purchase + Product
		&models.PurchaseItem{},

		// Level 3 — depends on User
		&models.Sale{},

		// Level 5 — depends on Sale + Product
		&models.SaleItem{},

		// Level 5 — depends on Product + User
		&models.StockMovement{},

		// Discounts — depends on Product + User (both nullable FKs)
		&models.Discount{},
	)

	if _, err2 := sqlDB.Exec("PRAGMA foreign_keys=ON;"); err2 != nil {
		log.Printf("Warning: could not re-enable foreign keys after migration: %v", err2)
	}

	if err != nil {
		return err
	}

	log.Println("Migrations complete.")
	return nil
}