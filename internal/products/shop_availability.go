package products

import (
	"context"
	"errors"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/identity"
	"github.com/chrisostomemataba/balceinv-api/internal/stock"
	"github.com/google/uuid"
)

var (
	ErrProductNotInShop  = errors.New("this product is not sold in this shop")
	ErrShopNotInBusiness = errors.New("that shop is not in this business")
	ErrShopStillHasStock = errors.New("a shop you removed still has stock of this product; send or sell it first")
)

type ShopAvailabilityRequest struct {
	ProductIds []string `json:"product_ids" validate:"required,min=1,max=500,dive,uuid"`
	ShopId     string   `json:"shop_id" validate:"required,uuid"`
	Available  *bool    `json:"available" validate:"required"`
}

type KeptProductView struct {
	Id   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}

type ShopAvailabilityView struct {
	Changed       int               `json:"changed"`
	KeptWithStock []KeptProductView `json:"kept_with_stock"`
}

func (service *Service) SetShopAvailability(ctx context.Context, querier database.Querier, principal *identity.Principal, request ShopAvailabilityRequest) (ShopAvailabilityView, error) {
	shopId := uuid.MustParse(request.ShopId)
	companyShopIds, shopsError := service.repository.OpenShopIds(ctx, querier, principal.CompanyId)
	if shopsError != nil {
		return ShopAvailabilityView{}, shopsError
	}
	if !companyShopIds[shopId] {
		return ShopAvailabilityView{}, ErrShopNotInBusiness
	}

	isAvailable := *request.Available
	stockRepository := stock.NewRepository()
	availabilityView := ShopAvailabilityView{KeptWithStock: []KeptProductView{}}
	for _, rawProductId := range request.ProductIds {
		productId := uuid.MustParse(rawProductId)
		familyIds, familyError := service.repository.FamilyIds(ctx, querier, principal.CompanyId, productId)
		if familyError != nil {
			return ShopAvailabilityView{}, familyError
		}
		if len(familyIds) == 0 {
			continue
		}
		if !isAvailable {
			stockedProducts, stockedError := service.repository.StockedInShop(ctx, querier, principal.CompanyId, shopId, familyIds)
			if stockedError != nil {
				return ShopAvailabilityView{}, stockedError
			}
			if len(stockedProducts) > 0 {
				availabilityView.KeptWithStock = append(availabilityView.KeptWithStock, KeptProductView{Id: productId, Name: stockedProducts[0].Name})
				continue
			}
		}
		listError := stockRepository.SetListed(ctx, querier, principal.CompanyId, shopId, familyIds, isAvailable)
		if listError != nil {
			return ShopAvailabilityView{}, listError
		}
		availabilityView.Changed++
	}
	return availabilityView, nil
}

func (service *Service) applyShopIds(ctx context.Context, querier database.Querier, companyId uuid.UUID, productId uuid.UUID, requestedShopIds []string) error {
	companyShopIds, shopsError := service.repository.OpenShopIds(ctx, querier, companyId)
	if shopsError != nil {
		return shopsError
	}
	availableShopIds := map[uuid.UUID]bool{}
	for _, rawShopId := range requestedShopIds {
		shopId := uuid.MustParse(rawShopId)
		if !companyShopIds[shopId] {
			return ErrShopNotInBusiness
		}
		availableShopIds[shopId] = true
	}

	familyIds, familyError := service.repository.FamilyIds(ctx, querier, companyId, productId)
	if familyError != nil {
		return familyError
	}
	stockRepository := stock.NewRepository()
	for shopId := range companyShopIds {
		isAvailable := availableShopIds[shopId]
		if !isAvailable {
			stockedProducts, stockedError := service.repository.StockedInShop(ctx, querier, companyId, shopId, familyIds)
			if stockedError != nil {
				return stockedError
			}
			if len(stockedProducts) > 0 {
				return ErrShopStillHasStock
			}
		}
		listError := stockRepository.SetListed(ctx, querier, companyId, shopId, familyIds, isAvailable)
		if listError != nil {
			return listError
		}
	}
	return nil
}

func (service *Service) inheritHiddenShops(ctx context.Context, querier database.Querier, companyId uuid.UUID, parentId uuid.UUID, variantId uuid.UUID) error {
	hiddenShopIds, hiddenError := service.repository.HiddenShopIds(ctx, querier, companyId, parentId)
	if hiddenError != nil {
		return hiddenError
	}
	stockRepository := stock.NewRepository()
	for _, shopId := range hiddenShopIds {
		listError := stockRepository.SetListed(ctx, querier, companyId, shopId, []uuid.UUID{variantId}, false)
		if listError != nil {
			return listError
		}
	}
	return nil
}
