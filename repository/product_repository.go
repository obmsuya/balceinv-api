package repository

import (
	"github.com/chrisostomemataba/balceinv-api/models"
	"gorm.io/gorm"
)

type ProductRepository struct {
	database *gorm.DB
}

func NewProductRepository(database *gorm.DB) *ProductRepository {
	return &ProductRepository{database: database}
}

func (repository *ProductRepository) FindAll(search, category string) ([]models.Product, error) {
	query := repository.database.Order("created_at DESC")

	if search != "" {
		query = query.Where("name LIKE ? OR sku LIKE ?", "%"+search+"%", "%"+search+"%")
	}

	if category != "" {
		query = query.Where("category = ?", category)
	}

	var products []models.Product
	err := query.Find(&products).Error
	return products, err
}

func (repository *ProductRepository) FindByID(id uint) (*models.Product, error) {
	var product models.Product
	err := repository.database.
		Preload("Barcodes").
		Preload("Addons", "is_active = ?", true).
		Preload("Variants").
		Preload("StockMovements", func(database *gorm.DB) *gorm.DB {
			return database.Order("created_at DESC").Limit(10)
		}).
		First(&product, id).Error
	if err != nil {
		return nil, err
	}
	return &product, nil
}

func (repository *ProductRepository) FindBySKU(sku string) (*models.Product, error) {
	var product models.Product
	err := repository.database.Where("sku = ?", sku).First(&product).Error
	if err != nil {
		return nil, err
	}
	return &product, nil
}

func (repository *ProductRepository) FindVariantsByParentID(parentID uint) ([]models.Product, error) {
	var variants []models.Product
	err := repository.database.
		Where("parent_id = ?", parentID).
		Order("variant_label ASC").
		Find(&variants).Error
	return variants, err
}

func (repository *ProductRepository) Create(product *models.Product) error {
	return repository.database.Create(product).Error
}

func (repository *ProductRepository) Update(product *models.Product) error {
	return repository.database.Save(product).Error
}

func (repository *ProductRepository) Delete(id uint) error {
	return repository.database.Delete(&models.Product{}, id).Error
}

func (repository *ProductRepository) FindLowStock() ([]models.Product, error) {
	var products []models.Product
	err := repository.database.Where("quantity <= min_stock").Order("quantity ASC").Find(&products).Error
	return products, err
}

func (repository *ProductRepository) CreateStockMovement(movement *models.StockMovement) error {
	return repository.database.Create(movement).Error
}

func (repository *ProductRepository) CreatePriceHistory(history *models.PriceHistory) error {
	return repository.database.Create(history).Error
}