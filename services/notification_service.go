package services

import (
	"github.com/chrisostomemataba/balceinv-api/models"
	"github.com/chrisostomemataba/balceinv-api/repository"
)

type NotificationService struct {
	repo         *repository.NotificationRepository
	settingsRepo *repository.SettingsRepository
	productRepo  *repository.ProductRepository
}

func NewNotificationService(
	repo *repository.NotificationRepository,
	settingsRepo *repository.SettingsRepository,
	productRepo *repository.ProductRepository,
) *NotificationService {
	return &NotificationService{
		repo:         repo,
		settingsRepo: settingsRepo,
		productRepo:  productRepo,
	}
}

func (s *NotificationService) GetAll(includeSeen bool) ([]models.StockAlert, error) {
	return s.repo.FindAll(includeSeen)
}

func (s *NotificationService) GetCount() (int64, error) {
	return s.repo.CountUnseen()
}

func (s *NotificationService) MarkAsSeen(id uint) error {
	return s.repo.MarkAsSeen(id)
}

func (s *NotificationService) MarkAllAsSeen() error {
	return s.repo.MarkAllAsSeen()
}

func (s *NotificationService) Delete(id uint) error {
	return s.repo.Delete(id)
}

func (s *NotificationService) ClearSeen() error {
	return s.repo.DeleteSeen()
}

// CheckStockLevels is the key function that makes notifications automatic.
// Call this after every sale with the list of product IDs that were just sold.
// For each product it checks whether an alert should exist (low/out stock)
// or be removed (stock back to normal), then creates or deletes accordingly.
// This is a direct translation of your TypeScript checkStockLevels but scoped
// to only the affected products rather than scanning the entire catalogue.
func (s *NotificationService) CheckStockLevels(productIDs []uint) error {
	settings, _ := s.settingsRepo.GetSettings()

	defaultThreshold := 5
	if settings != nil {
		defaultThreshold = settings.LowStockThreshold
	}

	for _, productID := range productIDs {
		product, err := s.productRepo.FindByID(productID)
		if err != nil {
			continue
		}

		threshold := product.MinStock
		if threshold == 0 {
			threshold = defaultThreshold
		}

		// Decide what kind of alert this product needs right now
		var alertType string
		if product.Quantity == 0 {
			alertType = "out"
		} else if product.Quantity <= threshold {
			alertType = "low"
		}

		existingAlert, err := s.repo.FindExistingUnseen(productID)
		alertExists := err == nil && existingAlert != nil

		if alertType != "" {
			// Stock is low or out — create an alert only if one does not already exist
			if !alertExists {
				s.repo.Create(&models.StockAlert{
					ProductID:       productID,
					CurrentQuantity: product.Quantity,
					Threshold:       threshold,
					AlertType:       alertType,
					IsSeen:          false,
				})
			}
		} else {
			// Stock is back to a healthy level — remove any existing alert
			if alertExists {
				s.repo.DeleteByProduct(productID)
			}
		}
	}

	return nil
}