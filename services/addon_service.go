package services

import (
	"errors"

	"github.com/chrisostomemataba/balceinv-api/models"
	"github.com/chrisostomemataba/balceinv-api/repository"
	"gorm.io/gorm"
)

type AddonService struct {
	addonRepository   *repository.AddonRepository
	productRepository *repository.ProductRepository
}

func NewAddonService(
	addonRepository *repository.AddonRepository,
	productRepository *repository.ProductRepository,
) *AddonService {
	return &AddonService{
		addonRepository:   addonRepository,
		productRepository: productRepository,
	}
}

type CreateAddonInput struct {
	Name  string  `json:"name"`
	Price float64 `json:"price"`
}

type UpdateAddonInput struct {
	Name     string  `json:"name"`
	Price    float64 `json:"price"`
	IsActive bool    `json:"is_active"`
}

func (service *AddonService) GetByProduct(productID uint) ([]models.ProductAddon, error) {
	_, err := service.productRepository.FindByID(productID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("product not found")
	}
	if err != nil {
		return nil, err
	}

	return service.addonRepository.FindByProductID(productID)
}

func (service *AddonService) Create(productID uint, input CreateAddonInput) (*models.ProductAddon, error) {
	if input.Name == "" {
		return nil, errors.New("addon name is required")
	}
	if input.Price < 0 {
		return nil, errors.New("addon price cannot be negative")
	}

	_, err := service.productRepository.FindByID(productID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("product not found")
	}
	if err != nil {
		return nil, err
	}

	addon := &models.ProductAddon{
		ProductID: productID,
		Name:      input.Name,
		Price:     input.Price,
		IsActive:  true,
	}

	if err := service.addonRepository.Create(addon); err != nil {
		return nil, err
	}

	return addon, nil
}

func (service *AddonService) Update(id uint, input UpdateAddonInput) (*models.ProductAddon, error) {
	if input.Name == "" {
		return nil, errors.New("addon name is required")
	}
	if input.Price < 0 {
		return nil, errors.New("addon price cannot be negative")
	}

	addon, err := service.addonRepository.FindByID(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("addon not found")
	}
	if err != nil {
		return nil, err
	}

	addon.Name = input.Name
	addon.Price = input.Price
	addon.IsActive = input.IsActive

	if err := service.addonRepository.Update(addon); err != nil {
		return nil, err
	}

	return addon, nil
}

func (service *AddonService) Delete(id uint) error {
	_, err := service.addonRepository.FindByID(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New("addon not found")
	}
	if err != nil {
		return err
	}

	return service.addonRepository.Delete(id)
}