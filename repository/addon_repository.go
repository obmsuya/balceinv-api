package repository

import (
	"github.com/chrisostomemataba/balceinv-api/models"
	"gorm.io/gorm"
)

type AddonRepository struct {
	database *gorm.DB
}

func NewAddonRepository(database *gorm.DB) *AddonRepository {
	return &AddonRepository{database: database}
}

func (repository *AddonRepository) FindByProductID(productID uint) ([]models.ProductAddon, error) {
	var addons []models.ProductAddon
	err := repository.database.
		Where("product_id = ? AND is_active = ?", productID, true).
		Order("created_at ASC").
		Find(&addons).Error
	return addons, err
}

func (repository *AddonRepository) FindByID(id uint) (*models.ProductAddon, error) {
	var addon models.ProductAddon
	err := repository.database.First(&addon, id).Error
	if err != nil {
		return nil, err
	}
	return &addon, nil
}

func (repository *AddonRepository) Create(addon *models.ProductAddon) error {
	return repository.database.Create(addon).Error
}

func (repository *AddonRepository) Update(addon *models.ProductAddon) error {
	return repository.database.Save(addon).Error
}

func (repository *AddonRepository) Delete(id uint) error {
	return repository.database.
		Model(&models.ProductAddon{}).
		Where("id = ?", id).
		Update("is_active", false).Error
}