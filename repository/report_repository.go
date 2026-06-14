package repository

import (
	"time"

	"github.com/chrisostomemataba/balceinv-api/models"
	"gorm.io/gorm"
)

type ReportRepository struct {
	db *gorm.DB
}

func NewReportRepository(db *gorm.DB) *ReportRepository {
	return &ReportRepository{db: db}
}

type ReportDateRange struct {
	StartDate *time.Time
	EndDate   *time.Time
}

// FindSalesInRange returns all sales within the optional date range.
// When no dates are provided it returns all sales — same behaviour as your TypeScript version.
func (r *ReportRepository) FindSalesInRange(dr ReportDateRange) ([]models.Sale, error) {
	query := r.db.Preload("User").Preload("Items.Product")

	if dr.StartDate != nil {
		query = query.Where("created_at >= ?", dr.StartDate)
	}
	if dr.EndDate != nil {
		end := time.Date(dr.EndDate.Year(), dr.EndDate.Month(), dr.EndDate.Day(), 23, 59, 59, 999, dr.EndDate.Location())
		query = query.Where("created_at <= ?", end)
	}

	var sales []models.Sale
	err := query.Find(&sales).Error
	return sales, err
}

// FindAllProducts returns every product — used by the inventory report to
// compute stock value and identify dead stock.
func (r *ReportRepository) FindAllProducts() ([]models.Product, error) {
	var products []models.Product
	err := r.db.Find(&products).Error
	return products, err
}

// FindSalesSince returns sales created after a given time — used to identify
// which products have sold recently when computing dead stock.
func (r *ReportRepository) FindSalesSince(since time.Time) ([]models.Sale, error) {
	var sales []models.Sale
	err := r.db.Preload("Items").Where("created_at >= ?", since).Find(&sales).Error
	return sales, err
}