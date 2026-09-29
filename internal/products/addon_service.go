package products

import (
	"context"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/identity"
	"github.com/google/uuid"
)

func (service *Service) ListAddons(ctx context.Context, querier database.Querier, principal *identity.Principal, productId uuid.UUID) ([]AddonView, error) {
	existingProduct, findError := service.repository.Find(ctx, querier, principal.CompanyId, nil, productId)
	if findError != nil {
		return nil, findError
	}
	if existingProduct == nil {
		return nil, ErrProductNotFound
	}

	productAddons, listError := service.repository.ListAddons(ctx, querier, principal.CompanyId, productId)
	if listError != nil {
		return nil, listError
	}

	addonViews := make([]AddonView, 0, len(productAddons))
	for _, productAddon := range productAddons {
		addonViews = append(addonViews, toAddonView(productAddon))
	}
	return addonViews, nil
}

func (service *Service) CreateAddon(ctx context.Context, querier database.Querier, principal *identity.Principal, productId uuid.UUID, request AddonRequest) (AddonView, error) {
	existingProduct, findError := service.repository.Find(ctx, querier, principal.CompanyId, nil, productId)
	if findError != nil {
		return AddonView{}, findError
	}
	if existingProduct == nil {
		return AddonView{}, ErrProductNotFound
	}

	createdAt := time.Now().UTC()
	isActive := request.IsActive == nil || *request.IsActive
	newAddon := Addon{
		Id:        uuid.Must(uuid.NewV7()),
		CompanyId: principal.CompanyId,
		ProductId: productId,
		Name:      strings.TrimSpace(request.Name),
		Price:     *request.Price,
		IsActive:  isActive,
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}

	insertError := service.repository.InsertAddon(ctx, querier, newAddon)
	if insertError != nil {
		if database.IsUniqueViolation(insertError) {
			return AddonView{}, ErrAddonNameTaken
		}
		return AddonView{}, insertError
	}

	return toAddonView(newAddon), nil
}

func (service *Service) UpdateAddon(ctx context.Context, querier database.Querier, principal *identity.Principal, addonId uuid.UUID, request AddonRequest) (AddonView, error) {
	existingAddon, findError := service.repository.FindAddon(ctx, querier, principal.CompanyId, addonId)
	if findError != nil {
		return AddonView{}, findError
	}
	if existingAddon == nil {
		return AddonView{}, ErrAddonNotFound
	}

	changedAddon := *existingAddon
	changedAddon.Name = strings.TrimSpace(request.Name)
	changedAddon.Price = *request.Price
	if request.IsActive != nil {
		changedAddon.IsActive = *request.IsActive
	}

	updateError := service.repository.UpdateAddon(ctx, querier, changedAddon)
	if updateError != nil {
		if database.IsUniqueViolation(updateError) {
			return AddonView{}, ErrAddonNameTaken
		}
		return AddonView{}, updateError
	}

	return toAddonView(changedAddon), nil
}

func (service *Service) DeleteAddon(ctx context.Context, querier database.Querier, principal *identity.Principal, addonId uuid.UUID) error {
	existingAddon, findError := service.repository.FindAddon(ctx, querier, principal.CompanyId, addonId)
	if findError != nil {
		return findError
	}
	if existingAddon == nil {
		return ErrAddonNotFound
	}

	return service.repository.DeleteAddon(ctx, querier, principal.CompanyId, addonId)
}

func toAddonView(productAddon Addon) AddonView {
	return AddonView{
		Id:        productAddon.Id,
		ProductId: productAddon.ProductId,
		Name:      productAddon.Name,
		Price:     productAddon.Price,
		IsActive:  productAddon.IsActive,
		CreatedAt: productAddon.CreatedAt,
		UpdatedAt: productAddon.UpdatedAt,
	}
}
