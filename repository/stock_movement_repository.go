package repository

import (
	"time"

	"github.com/chrisostomemataba/balceinv-api/models"
	"gorm.io/gorm"
)

type StockMovementRepository struct {
	db *gorm.DB
}

func NewStockMovementRepository(db *gorm.DB) *StockMovementRepository {
	return &StockMovementRepository{db: db}
}

type MovementFilters struct {
	StartDate time.Time
	EndDate   time.Time
	ProductID uint
	Reason    string
	Search    string
}

func (r *StockMovementRepository) FindAll(filters MovementFilters) ([]models.StockMovement, error) {
	query := r.db.
		Preload("Product").
		Preload("User").
		Order("created_at DESC")

	if !filters.StartDate.IsZero() {
		query = query.Where("created_at >= ?", filters.StartDate)
	}
	if !filters.EndDate.IsZero() {
		end := filters.EndDate
		end = time.Date(end.Year(), end.Month(), end.Day(), 23, 59, 59, 999, end.Location())
		query = query.Where("created_at <= ?", end)
	}
	if filters.ProductID != 0 {
		query = query.Where("product_id = ?", filters.ProductID)
	}
	if filters.Reason != "" {
		query = query.Where("reason = ?", filters.Reason)
	}

	var movements []models.StockMovement
	if err := query.Find(&movements).Error; err != nil {
		return nil, err
	}

	// Search is applied in memory after the DB query because it filters
	// on the related product name and SKU, not on the movements table itself.
	if filters.Search != "" {
		search := filters.Search
		filtered := movements[:0]
		for _, m := range movements {
			if contains(m.Product.Name, search) || contains(m.Product.SKU, search) {
				filtered = append(filtered, m)
			}
		}
		return filtered, nil
	}

	return movements, nil
}

func (r *StockMovementRepository) FindByID(id uint) (*models.StockMovement, error) {
	var movement models.StockMovement
	err := r.db.
		Preload("Product").
		Preload("User").
		First(&movement, id).Error
	if err != nil {
		return nil, err
	}
	return &movement, nil
}

func (r *StockMovementRepository) FindByProduct(productID uint) ([]models.StockMovement, error) {
	var movements []models.StockMovement
	err := r.db.
		Preload("Product").
		Preload("User").
		Where("product_id = ?", productID).
		Order("created_at DESC").
		Find(&movements).Error
	return movements, err
}

func (r *StockMovementRepository) FindByDateRange(start, end time.Time) ([]models.StockMovement, error) {
	endOfDay := time.Date(end.Year(), end.Month(), end.Day(), 23, 59, 59, 999, end.Location())
	var movements []models.StockMovement
	err := r.db.
		Preload("Product").
		Preload("User").
		Where("created_at >= ? AND created_at <= ?", start, endOfDay).
		Order("created_at DESC").
		Find(&movements).Error
	return movements, err
}

// FindAll for summary counts every movement — no filters, no preloads needed
// since we only need the reason and change fields to compute the summary.
func (r *StockMovementRepository) FindAllRaw() ([]models.StockMovement, error) {
	var movements []models.StockMovement
	err := r.db.Select("reason", "change").Find(&movements).Error
	return movements, err
}

// CreateWithProductUpdate writes the movement record and updates the product
// quantity in one transaction so the two never get out of sync.
func (r *StockMovementRepository) CreateWithProductUpdate(
	movement *models.StockMovement,
	productID uint,
	newQuantity int,
) error {
	return r.db.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&models.Product{}).
			Where("id = ?", productID).
			Update("quantity", newQuantity).Error; err != nil {
			return err
		}
		return tx.Create(movement).Error
	})
}

// contains is a case-insensitive substring check used for in-memory search.
func contains(s, substr string) bool {
	return len(s) >= len(substr) &&
		(s == substr ||
			len(substr) == 0 ||
			(len(s) > 0 && indexInsensitive(s, substr) >= 0))
}

func indexInsensitive(s, substr string) int {
	sLower := toLower(s)
	subLower := toLower(substr)
	for i := 0; i <= len(sLower)-len(subLower); i++ {
		if sLower[i:i+len(subLower)] == subLower {
			return i
		}
	}
	return -1
}

func toLower(s string) string {
	b := []byte(s)
	for i, c := range b {
		if c >= 'A' && c <= 'Z' {
			b[i] = c + 32
		}
	}
	return string(b)
}