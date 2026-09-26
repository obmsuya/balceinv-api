package repository

import (
	"strings"

	"github.com/chrisostomemataba/balceinv-api/models"
	"gorm.io/gorm"
)

type CatalogRepository struct {
	database *gorm.DB
}

type CatalogCount struct {
	BusinessType string `json:"business_type"`
	Count        int64  `json:"count"`
}

type CatalogMergeResult struct {
	Added   int
	Updated int
}

const catalogInsertBatchSize = 200

func NewCatalogRepository(database *gorm.DB) *CatalogRepository {
	return &CatalogRepository{database: database}
}

func (repository *CatalogRepository) FindByBusinessType(businessType string) ([]models.CatalogProduct, error) {
	var catalogProducts []models.CatalogProduct
	findError := repository.database.
		Where("business_type = ?", businessType).
		Order("name ASC").
		Find(&catalogProducts).Error
	return catalogProducts, findError
}

func (repository *CatalogRepository) CountByBusinessType(businessType string) (int64, error) {
	var catalogProductCount int64
	countError := repository.database.
		Model(&models.CatalogProduct{}).
		Where("business_type = ?", businessType).
		Count(&catalogProductCount).Error
	return catalogProductCount, countError
}

func (repository *CatalogRepository) CountPerBusinessType() ([]CatalogCount, error) {
	var catalogCounts []CatalogCount
	countError := repository.database.
		Model(&models.CatalogProduct{}).
		Select("business_type, COUNT(*) AS count").
		Group("business_type").
		Scan(&catalogCounts).Error
	return catalogCounts, countError
}

func (repository *CatalogRepository) CreateAll(catalogProducts []models.CatalogProduct) error {
	if len(catalogProducts) == 0 {
		return nil
	}
	return repository.database.CreateInBatches(catalogProducts, catalogInsertBatchSize).Error
}

func (repository *CatalogRepository) Replace(businessType string, catalogProducts []models.CatalogProduct) error {
	return repository.database.Transaction(func(transaction *gorm.DB) error {
		deleteError := transaction.Where("business_type = ?", businessType).Delete(&models.CatalogProduct{}).Error
		if deleteError != nil {
			return deleteError
		}
		if len(catalogProducts) == 0 {
			return nil
		}
		return transaction.CreateInBatches(catalogProducts, catalogInsertBatchSize).Error
	})
}

func (repository *CatalogRepository) Merge(businessType string, catalogProducts []models.CatalogProduct) (CatalogMergeResult, error) {
	mergeResult := CatalogMergeResult{}
	transactionError := repository.database.Transaction(func(transaction *gorm.DB) error {
		var existingCatalogProducts []models.CatalogProduct
		findError := transaction.Where("business_type = ?", businessType).Find(&existingCatalogProducts).Error
		if findError != nil {
			return findError
		}

		existingIdByName := make(map[string]uint, len(existingCatalogProducts))
		for _, existingCatalogProduct := range existingCatalogProducts {
			existingIdByName[CatalogNameKey(existingCatalogProduct.Name)] = existingCatalogProduct.ID
		}

		var newCatalogProducts []models.CatalogProduct
		for _, catalogProduct := range catalogProducts {
			existingId, alreadyInCatalog := existingIdByName[CatalogNameKey(catalogProduct.Name)]
			if !alreadyInCatalog {
				newCatalogProducts = append(newCatalogProducts, catalogProduct)
				continue
			}
			catalogProduct.ID = existingId
			saveError := transaction.Save(&catalogProduct).Error
			if saveError != nil {
				return saveError
			}
			mergeResult.Updated++
		}

		if len(newCatalogProducts) > 0 {
			createError := transaction.CreateInBatches(newCatalogProducts, catalogInsertBatchSize).Error
			if createError != nil {
				return createError
			}
		}
		mergeResult.Added = len(newCatalogProducts)
		return nil
	})
	return mergeResult, transactionError
}

func (repository *CatalogRepository) DeleteByBusinessType(businessType string) (int64, error) {
	deleteResult := repository.database.Where("business_type = ?", businessType).Delete(&models.CatalogProduct{})
	return deleteResult.RowsAffected, deleteResult.Error
}

func CatalogNameKey(name string) string {
	return strings.ToLower(strings.Join(strings.Fields(name), " "))
}
