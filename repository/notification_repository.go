package repository

import (
	"github.com/chrisostomemataba/balceinv-api/models"
	"gorm.io/gorm"
)

type NotificationRepository struct {
	db *gorm.DB
}

func NewNotificationRepository(db *gorm.DB) *NotificationRepository {
	return &NotificationRepository{db: db}
}

// FindAll returns stock alerts joined with their product details.
// When includeSeen is false, only unseen alerts are returned — this is the
// default because the bell icon in your UI only shows unread notifications.
func (r *NotificationRepository) FindAll(includeSeen bool) ([]models.StockAlert, error) {
	query := r.db.Preload("Product").Order("created_at DESC")

	if !includeSeen {
		query = query.Where("is_seen = ?", false)
	}

	var alerts []models.StockAlert
	err := query.Find(&alerts).Error
	return alerts, err
}

// CountUnseen returns only the number — used by the header bell badge
// so the frontend does not have to fetch all notification data just to show a count.
func (r *NotificationRepository) CountUnseen() (int64, error) {
	var count int64
	err := r.db.Model(&models.StockAlert{}).Where("is_seen = ?", false).Count(&count).Error
	return count, err
}

func (r *NotificationRepository) MarkAsSeen(id uint) error {
	return r.db.Model(&models.StockAlert{}).
		Where("id = ?", id).
		Update("is_seen", true).Error
}

func (r *NotificationRepository) MarkAllAsSeen() error {
	return r.db.Model(&models.StockAlert{}).
		Where("is_seen = ?", false).
		Update("is_seen", true).Error
}

func (r *NotificationRepository) Delete(id uint) error {
	return r.db.Delete(&models.StockAlert{}, id).Error
}

func (r *NotificationRepository) DeleteSeen() error {
	return r.db.Where("is_seen = ?", true).Delete(&models.StockAlert{}).Error
}

// FindExistingUnseen checks whether an unseen alert already exists for a product.
// This prevents duplicate alerts from being created every time a sale runs the stock check.
func (r *NotificationRepository) FindExistingUnseen(productID uint) (*models.StockAlert, error) {
	var alert models.StockAlert
	err := r.db.Where("product_id = ? AND is_seen = ?", productID, false).First(&alert).Error
	if err != nil {
		return nil, err
	}
	return &alert, nil
}

func (r *NotificationRepository) Create(alert *models.StockAlert) error {
	return r.db.Create(alert).Error
}

func (r *NotificationRepository) DeleteByProduct(productID uint) error {
	return r.db.Where("product_id = ?", productID).Delete(&models.StockAlert{}).Error
}