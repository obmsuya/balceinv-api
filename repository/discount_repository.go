package repository

import (
	"time"

	"github.com/chrisostomemataba/balceinv-api/models"
	"gorm.io/gorm"
)

type DiscountRepository struct {
	database *gorm.DB
}

func NewDiscountRepository(database *gorm.DB) *DiscountRepository {
	return &DiscountRepository{database: database}
}

func (repository *DiscountRepository) FindAll() ([]models.Discount, error) {
	var discounts []models.Discount
	err := repository.database.
		Preload("Product").
		Preload("Creator").
		Order("created_at DESC").
		Find(&discounts).Error
	return discounts, err
}

func (repository *DiscountRepository) FindByID(id uint) (*models.Discount, error) {
	var discount models.Discount
	err := repository.database.
		Preload("Product").
		Preload("Creator").
		First(&discount, id).Error
	if err != nil {
		return nil, err
	}
	return &discount, nil
}

func (repository *DiscountRepository) FindActiveForProduct(productID uint) ([]models.Discount, error) {
	now := time.Now()

	var discounts []models.Discount
	err := repository.database.
		Where(
			"is_active = ? AND starts_at <= ? AND ends_at >= ? AND (product_id = ? OR product_id IS NULL)",
			true, now, now, productID,
		).
		Order("product_id DESC").
		Find(&discounts).Error

	return discounts, err
}

func (repository *DiscountRepository) Create(discount *models.Discount) error {
	return repository.database.Create(discount).Error
}

func (repository *DiscountRepository) Update(discount *models.Discount) error {
	return repository.database.Save(discount).Error
}

func (repository *DiscountRepository) Delete(id uint) error {
	return repository.database.Delete(&models.Discount{}, id).Error
}

func (repository *DiscountRepository) Deactivate(id uint) error {
	return repository.database.
		Model(&models.Discount{}).
		Where("id = ?", id).
		Update("is_active", false).Error
}