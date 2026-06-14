package services

import (
	"encoding/json"
	"errors"
	"fmt"
	"os"

	"github.com/chrisostomemataba/balceinv-api/models"
	"github.com/chrisostomemataba/balceinv-api/utils"
	"gorm.io/gorm"
)

type SetupService struct {
	db *gorm.DB
}

func NewSetupService(db *gorm.DB) *SetupService {
	return &SetupService{db: db}
}

type SetupInput struct {
	BusinessName  string  `json:"business_name"`
	BusinessType  string  `json:"business_type"`
	Phone         *string `json:"phone"`
	Address       *string `json:"address"`
	TIN           *string `json:"tin"`
	OwnerName     string  `json:"owner_name"`
	OwnerEmail    string  `json:"owner_email"`
	OwnerPassword string  `json:"owner_password"`
}

func (s *SetupService) IsConfigured() bool {
	var count int64
	s.db.Model(&models.Company{}).Count(&count)
	return count > 0
}

func (s *SetupService) Run(input SetupInput) error {
	if s.IsConfigured() {
		return errors.New("system already configured")
	}

	var company models.Company
	var adminRoleID uint

	err := s.db.Transaction(func(tx *gorm.DB) error {
		// 1. Create the company record
		company = models.Company{
			Name:         input.BusinessName,
			BusinessType: input.BusinessType,
			Phone:        input.Phone,
			Address:      input.Address,
			TIN:          input.TIN,
		}
		if err := tx.Create(&company).Error; err != nil {
			return err
		}

		// 2. Find the existing "Admin" role OR create it.
		//    Using FirstOrCreate avoids a duplicate if the seed script already inserted it.
		adminRole := models.Role{Name: "Admin"}
		if err := tx.Where("name = ?", "Admin").FirstOrCreate(&adminRole).Error; err != nil {
			return err
		}
		adminRoleID = adminRole.ID

		// 3. Create the owner user and link it to the Admin role
		hash, err := utils.HashPassword(input.OwnerPassword)
		if err != nil {
			return err
		}
		owner := models.User{
			Name:         input.OwnerName,
			Email:        input.OwnerEmail,
			PasswordHash: hash,
			RoleID:       adminRoleID,
			CompanyID:    company.ID,
		}
		if err := tx.Create(&owner).Error; err != nil {
			return err
		}

		// 4. Create default company settings
		settings := models.Settings{
			CompanyID:           company.ID,
			TaxRate:             18.0,
			Currency:            "TZS",
			CurrencySymbol:      "TZS",
			DateFormat:          "DD/MM/YYYY",
			ReceiptNumberFormat: "SALE-{DATE}-{COUNTER}",
			EFDEnabled:          false,
			LowStockThreshold:   5,
			AlertSoundEnabled:   true,
			AlertOnLowStock:     true,
			AlertOnOutOfStock:   true,
			ShowTaxOnReceipt:    true,
		}
		if err := tx.Create(&settings).Error; err != nil {
			return err
		}

		return nil
	})

	if err != nil {
		return err
	}

	// 5. Grant ALL existing permissions to the Admin role.
	//    This runs after the transaction so permissions seeded by the seed script are included.
	//    The owner (Admin) becomes a true super-admin with full system access.
	if err := s.assignAllPermissionsToRole(adminRoleID); err != nil {
		// Log but don't fail — the company was created. Admin can assign permissions via UI.
		fmt.Printf("warn: could not assign permissions to Admin role: %v\n", err)
	}

	if err := s.seedCatalog(company.ID, company.BusinessType); err != nil {
		fmt.Printf("catalog seed skipped for %s: %v\n", company.BusinessType, err)
	}

	return s.db.Model(&company).Update("is_seeded", true).Error
}


func (s *SetupService) assignAllPermissionsToRole(roleID uint) error {
	// Load every permission that currently exists
	var perms []models.Permission
	if err := s.db.Find(&perms).Error; err != nil {
		return err
	}
	if len(perms) == 0 {
		return nil
	}

	// Remove any previous assignments for this role
	if err := s.db.Where("role_id = ?", roleID).Delete(&models.RolePermission{}).Error; err != nil {
		return err
	}

	// Build and insert one row per permission
	rows := make([]models.RolePermission, len(perms))
	for i, p := range perms {
		rows[i] = models.RolePermission{RoleID: roleID, PermissionID: p.ID}
	}

	return s.db.Create(&rows).Error
}

func (s *SetupService) seedCatalog(companyID uint, businessType string) error {
	filePath := fmt.Sprintf("seeds/%s.json", businessType)
	data, err := os.ReadFile(filePath)
	if err != nil {
		return nil
	}

	var items []models.CatalogProduct
	if err := json.Unmarshal(data, &items); err != nil {
		return err
	}

	for i := range items {
		items[i].BusinessType = businessType
	}

	return s.db.CreateInBatches(items, 50).Error
}