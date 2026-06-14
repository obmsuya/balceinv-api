package repository

import (
	"github.com/chrisostomemataba/balceinv-api/models"
	"gorm.io/gorm"
)

type SettingsRepository struct {
	db *gorm.DB
}

func NewSettingsRepository(db *gorm.DB) *SettingsRepository {
	return &SettingsRepository{db: db}
}

func (r *SettingsRepository) GetSettings() (*models.Settings, error) {
	var settings models.Settings
	err := r.db.Preload("Company").First(&settings).Error
	if err != nil {
		return nil, err
	}
	return &settings, nil
}

func (r *SettingsRepository) CreateDefaults(companyID uint) (*models.Settings, error) {
	settings := &models.Settings{
		CompanyID:           companyID,
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

	if err := r.db.Create(settings).Error; err != nil {
		return nil, err
	}
	return settings, nil
}

func (r *SettingsRepository) UpdateSettings(id uint, updates map[string]interface{}) (*models.Settings, error) {
	if err := r.db.Model(&models.Settings{}).Where("id = ?", id).Updates(updates).Error; err != nil {
		return nil, err
	}
	return r.GetSettings()
}

func (r *SettingsRepository) UpdateCompany(id uint, updates map[string]interface{}) error {
	return r.db.Model(&models.Company{}).Where("id = ?", id).Updates(updates).Error
}