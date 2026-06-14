package services

import (
	"errors"
	"time"

	"github.com/chrisostomemataba/balceinv-api/models"
	"github.com/chrisostomemataba/balceinv-api/repository"
	"gorm.io/gorm"
)

type DiscountService struct {
	discountRepository *repository.DiscountRepository
}

func NewDiscountService(discountRepository *repository.DiscountRepository) *DiscountService {
	return &DiscountService{discountRepository: discountRepository}
}

type CreateDiscountInput struct {
	Name         string    `json:"name"`
	ProductID    *uint     `json:"product_id"`
	DiscountType string    `json:"discount_type"`
	Value        float64   `json:"value"`
	StartsAt     time.Time `json:"starts_at"`
	EndsAt       time.Time `json:"ends_at"`
}

type UpdateDiscountInput struct {
	Name         string    `json:"name"`
	ProductID    *uint     `json:"product_id"`
	DiscountType string    `json:"discount_type"`
	Value        float64   `json:"value"`
	StartsAt     time.Time `json:"starts_at"`
	EndsAt       time.Time `json:"ends_at"`
	IsActive     bool      `json:"is_active"`
}

type AppliedDiscount struct {
	DiscountID   uint    `json:"discount_id"`
	Name         string  `json:"name"`
	DiscountType string  `json:"discount_type"`
	Value        float64 `json:"value"`
	FinalPrice   float64 `json:"final_price"`
}

func (service *DiscountService) GetAll() ([]models.Discount, error) {
	return service.discountRepository.FindAll()
}

func (service *DiscountService) GetByID(id uint) (*models.Discount, error) {
	discount, err := service.discountRepository.FindByID(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("discount not found")
	}
	return discount, err
}

func (service *DiscountService) GetActiveForProduct(productID uint, originalPrice float64) (*AppliedDiscount, error) {
	activeDiscounts, err := service.discountRepository.FindActiveForProduct(productID)
	if err != nil {
		return nil, err
	}

	if len(activeDiscounts) == 0 {
		return nil, nil
	}

	bestDiscount := activeDiscounts[0]

	finalPrice := service.calculateDiscountedPrice(originalPrice, bestDiscount.DiscountType, bestDiscount.Value)

	return &AppliedDiscount{
		DiscountID:   bestDiscount.ID,
		Name:         bestDiscount.Name,
		DiscountType: bestDiscount.DiscountType,
		Value:        bestDiscount.Value,
		FinalPrice:   finalPrice,
	}, nil
}

func (service *DiscountService) calculateDiscountedPrice(originalPrice float64, discountType string, value float64) float64 {
	switch discountType {
	case "percent":
		discountAmount := originalPrice * (value / 100)
		return originalPrice - discountAmount
	case "fixed":
		discountedPrice := originalPrice - value
		if discountedPrice < 0 {
			return 0
		}
		return discountedPrice
	default:
		return originalPrice
	}
}

func (service *DiscountService) Create(input CreateDiscountInput, createdBy uint) (*models.Discount, error) {
	if err := service.validateDiscountInput(input.Name, input.DiscountType, input.Value, input.StartsAt, input.EndsAt); err != nil {
		return nil, err
	}

	discount := &models.Discount{
		Name:         input.Name,
		ProductID:    input.ProductID,
		DiscountType: input.DiscountType,
		Value:        input.Value,
		StartsAt:     input.StartsAt,
		EndsAt:       input.EndsAt,
		IsActive:     true,
		CreatedBy:    createdBy,
	}

	if err := service.discountRepository.Create(discount); err != nil {
		return nil, err
	}

	return service.discountRepository.FindByID(discount.ID)
}

func (service *DiscountService) Update(id uint, input UpdateDiscountInput) (*models.Discount, error) {
	if err := service.validateDiscountInput(input.Name, input.DiscountType, input.Value, input.StartsAt, input.EndsAt); err != nil {
		return nil, err
	}

	discount, err := service.discountRepository.FindByID(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("discount not found")
	}
	if err != nil {
		return nil, err
	}

	discount.Name = input.Name
	discount.ProductID = input.ProductID
	discount.DiscountType = input.DiscountType
	discount.Value = input.Value
	discount.StartsAt = input.StartsAt
	discount.EndsAt = input.EndsAt
	discount.IsActive = input.IsActive

	if err := service.discountRepository.Update(discount); err != nil {
		return nil, err
	}

	return service.discountRepository.FindByID(id)
}

func (service *DiscountService) Delete(id uint) error {
	_, err := service.discountRepository.FindByID(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New("discount not found")
	}
	if err != nil {
		return err
	}

	return service.discountRepository.Delete(id)
}

func (service *DiscountService) Deactivate(id uint) error {
	_, err := service.discountRepository.FindByID(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New("discount not found")
	}
	if err != nil {
		return err
	}

	return service.discountRepository.Deactivate(id)
}

func (service *DiscountService) validateDiscountInput(name string, discountType string, value float64, startsAt time.Time, endsAt time.Time) error {
	if name == "" {
		return errors.New("discount name is required")
	}
	if discountType != "percent" && discountType != "fixed" {
		return errors.New("discount type must be either 'percent' or 'fixed'")
	}
	if value <= 0 {
		return errors.New("discount value must be greater than zero")
	}
	if discountType == "percent" && value > 100 {
		return errors.New("percentage discount cannot exceed 100")
	}
	if endsAt.Before(startsAt) {
		return errors.New("end date must be after start date")
	}
	if endsAt.Before(time.Now()) {
		return errors.New("end date cannot be in the past")
	}
	return nil
}