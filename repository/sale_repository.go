package repository

import (
	"time"

	"github.com/chrisostomemataba/balceinv-api/models"
	"gorm.io/gorm"
)

type SaleRepository struct {
	db *gorm.DB
}

func NewSaleRepository(db *gorm.DB) *SaleRepository {
	return &SaleRepository{db: db}
}

func (r *SaleRepository) FindAll(filters SaleFilters) ([]models.Sale, error) {
	query := r.db.Preload("User").Order("created_at DESC")

	if !filters.StartDate.IsZero() {
		query = query.Where("created_at >= ?", filters.StartDate)
	}
	if !filters.EndDate.IsZero() {
		end := filters.EndDate
		end = time.Date(end.Year(), end.Month(), end.Day(), 23, 59, 59, 999, end.Location())
		query = query.Where("created_at <= ?", end)
	}
	if filters.PaymentType != "" {
		query = query.Where("payment_type = ?", filters.PaymentType)
	}
	if filters.SaleType != "" {
		query = query.Where("sale_type = ?", filters.SaleType)
	}

	var sales []models.Sale
	err := query.Find(&sales).Error
	return sales, err
}

func (r *SaleRepository) FindByID(id uint) (*models.Sale, error) {
	var sale models.Sale
	err := r.db.
		Preload("User").
		Preload("Items.Product").
		First(&sale, id).Error
	if err != nil {
		return nil, err
	}
	return &sale, nil
}

func (r *SaleRepository) FindByDateRange(start, end time.Time) ([]models.Sale, error) {
	var sales []models.Sale
	endOfDay := time.Date(end.Year(), end.Month(), end.Day(), 23, 59, 59, 999, end.Location())
	err := r.db.
		Preload("Items.Product").
		Where("created_at >= ? AND created_at <= ?", start, endOfDay).
		Order("created_at DESC").
		Find(&sales).Error
	return sales, err
}

// CountToday returns how many sales were made today — used to generate the receipt counter.
func (r *SaleRepository) CountToday() (int64, error) {
	today := time.Now()
	startOfDay := time.Date(today.Year(), today.Month(), today.Day(), 0, 0, 0, 0, today.Location())
	var count int64
	err := r.db.Model(&models.Sale{}).Where("created_at >= ?", startOfDay).Count(&count).Error
	return count, err
}

// CreateWithItems wraps the entire sale creation in a single database transaction.
// If anything fails — stock update, sale item insert, stock movement — the whole
// transaction rolls back and nothing is written to disk. This protects your inventory
// from partial writes that would leave stock counts inconsistent.
func (r *SaleRepository) CreateWithItems(
	sale *models.Sale,
	items []models.SaleItem,
	movements []models.StockMovement,
	stockUpdates map[uint]int,
) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		// Step 1 — Insert the parent sale record to get its ID
		if err := tx.Create(sale).Error; err != nil {
			return err
		}

		// Step 2 — Insert each sale item, linking it to the sale we just created
		for i := range items {
			items[i].SaleID = sale.ID
			if err := tx.Create(&items[i]).Error; err != nil {
				return err
			}
		}

		// Step 3 — Update stock quantity for each product
		for productID, newQty := range stockUpdates {
			if err := tx.Model(&models.Product{}).
				Where("id = ?", productID).
				Update("quantity", newQty).Error; err != nil {
				return err
			}
		}

		// Step 4 — Write stock movement records for the audit trail
		for i := range movements {
			if err := tx.Create(&movements[i]).Error; err != nil {
				return err
			}
		}

		return nil
	})
}

type SaleFilters struct {
	StartDate   time.Time
	EndDate     time.Time
	PaymentType string
	SaleType    string
}