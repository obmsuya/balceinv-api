package services

import (
	"errors"
	"time"

	"github.com/chrisostomemataba/balceinv-api/models"
	"github.com/chrisostomemataba/balceinv-api/repository"
	"gorm.io/gorm"
)

type StockMovementService struct {
	repo        *repository.StockMovementRepository
	productRepo *repository.ProductRepository
}

func NewStockMovementService(
	repo *repository.StockMovementRepository,
	productRepo *repository.ProductRepository,
) *StockMovementService {
	return &StockMovementService{repo: repo, productRepo: productRepo}
}

type CreateMovementInput struct {
	ProductID uint   `json:"productId"`
	Change    int    `json:"change"`
	Reason    string `json:"reason"`
	Reference string `json:"reference"`
}

func (s *StockMovementService) GetAll(filters repository.MovementFilters) ([]models.StockMovement, error) {
	return s.repo.FindAll(filters)
}

func (s *StockMovementService) GetByID(id uint) (*models.StockMovement, error) {
	movement, err := s.repo.FindByID(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("stock movement not found")
	}
	return movement, err
}

func (s *StockMovementService) GetByProduct(productID uint) ([]models.StockMovement, error) {
	return s.repo.FindByProduct(productID)
}

func (s *StockMovementService) GetByDateRange(start, end time.Time) ([]models.StockMovement, error) {
	return s.repo.FindByDateRange(start, end)
}

// Create handles a manual stock adjustment — used by managers to correct stock counts,
// record damage, or log a purchase that came in without going through the sales flow.
// It validates that the resulting quantity cannot go negative, then writes both the
// product update and the movement record atomically.
func (s *StockMovementService) Create(input CreateMovementInput, userID uint) (*models.StockMovement, error) {
	product, err := s.productRepo.FindByID(input.ProductID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("product not found")
	}
	if err != nil {
		return nil, err
	}

	newQuantity := product.Quantity + input.Change
	if newQuantity < 0 {
		return nil, errors.New("cannot reduce stock below zero")
	}

	var ref *string
	if input.Reference != "" {
		ref = &input.Reference
	}

	movement := &models.StockMovement{
		ProductID:   input.ProductID,
		Change:      input.Change,
		NewQuantity: newQuantity,
		Reason:      input.Reason,
		Reference:   ref,
		UserID:      &userID,
	}

	if err := s.repo.CreateWithProductUpdate(movement, input.ProductID, newQuantity); err != nil {
		return nil, err
	}

	// Re-fetch with associations so the response includes product and user details
	return s.repo.FindByID(movement.ID)
}

// GetSummary counts movements by reason and computes the net stock change overall.
// This is what your dashboard and reports pages read to show the movement breakdown.
func (s *StockMovementService) GetSummary() (map[string]interface{}, error) {
	movements, err := s.repo.FindAllRaw()
	if err != nil {
		return nil, err
	}

	summary := map[string]interface{}{
		"total_movements": len(movements),
		"by_sale":         0,
		"by_purchase":     0,
		"by_adjustment":   0,
		"by_damage":       0,
		"net_change":      0,
	}

	netChange := 0
	bySale, byPurchase, byAdjustment, byDamage := 0, 0, 0, 0

	for _, m := range movements {
		netChange += m.Change
		switch m.Reason {
		case "sale":
			bySale++
		case "purchase":
			byPurchase++
		case "adjust":
			byAdjustment++
		case "damage":
			byDamage++
		}
	}

	summary["by_sale"] = bySale
	summary["by_purchase"] = byPurchase
	summary["by_adjustment"] = byAdjustment
	summary["by_damage"] = byDamage
	summary["net_change"] = netChange

	return summary, nil
}