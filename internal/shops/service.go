package shops

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/google/uuid"
)

const defaultReceiptPrefix = "SALE"

var (
	ErrShopNotFound   = errors.New("shop not found")
	ErrShopNameTaken  = errors.New("another shop already has this name")
	ErrLastActiveShop = errors.New("keep at least one shop open; open another shop before closing this one")
	ErrShopInUse      = errors.New("this shop has sales, stock or other records, so it can only be closed")
	ErrShopHasStaff   = errors.New("some staff work only in this shop; give them another shop first")
	ErrShopIsCurrent  = errors.New("switch to another shop before deleting this one")
)

type Service struct {
	repository *Repository
}

func NewService(repository *Repository) *Service {
	return &Service{
		repository: repository,
	}
}

func (service *Service) List(ctx context.Context, querier database.Querier, companyId uuid.UUID, limit int, offset int) (response.Page[ShopView], error) {
	totalShops, countError := service.repository.Count(ctx, querier, companyId)
	if countError != nil {
		return response.Page[ShopView]{}, countError
	}

	foundShops, listError := service.repository.List(ctx, querier, companyId, limit, offset)
	if listError != nil {
		return response.Page[ShopView]{}, listError
	}

	shopViews := make([]ShopView, 0, len(foundShops))
	for _, foundShop := range foundShops {
		shopViews = append(shopViews, toView(foundShop))
	}

	shopPage := response.Page[ShopView]{
		Items:  shopViews,
		Total:  totalShops,
		Limit:  limit,
		Offset: offset,
	}
	return shopPage, nil
}

func (service *Service) Get(ctx context.Context, querier database.Querier, companyId uuid.UUID, shopId uuid.UUID) (ShopView, error) {
	foundShop, findError := service.repository.Find(ctx, querier, companyId, shopId)
	if findError != nil {
		return ShopView{}, findError
	}
	if foundShop == nil {
		return ShopView{}, ErrShopNotFound
	}
	return toView(*foundShop), nil
}

func (service *Service) Create(ctx context.Context, querier database.Querier, companyId uuid.UUID, request ShopRequest) (ShopView, error) {
	createdAt := time.Now().UTC()

	newShop := Shop{
		Id:            uuid.Must(uuid.NewV7()),
		CompanyId:     companyId,
		Name:          strings.TrimSpace(request.Name),
		Address:       trimmedOrNil(request.Address),
		Phone:         trimmedOrNil(request.Phone),
		ReceiptPrefix: receiptPrefixOrDefault(request.ReceiptPrefix),
		IsActive:      true,
		CreatedAt:     createdAt,
		UpdatedAt:     createdAt,
	}

	insertError := service.repository.Insert(ctx, querier, newShop)
	if database.IsUniqueViolation(insertError) {
		return ShopView{}, ErrShopNameTaken
	}
	if insertError != nil {
		return ShopView{}, insertError
	}

	return toView(newShop), nil
}

func (service *Service) Update(ctx context.Context, querier database.Querier, companyId uuid.UUID, shopId uuid.UUID, request ShopRequest) (ShopView, error) {
	existingShop, findError := service.repository.Find(ctx, querier, companyId, shopId)
	if findError != nil {
		return ShopView{}, findError
	}
	if existingShop == nil {
		return ShopView{}, ErrShopNotFound
	}

	changedShop := *existingShop
	changedShop.Name = strings.TrimSpace(request.Name)
	changedShop.Address = trimmedOrNil(request.Address)
	changedShop.Phone = trimmedOrNil(request.Phone)
	changedShop.ReceiptPrefix = receiptPrefixOrDefault(request.ReceiptPrefix)
	changedShop.UpdatedAt = time.Now().UTC()
	if request.IsActive != nil {
		changedShop.IsActive = *request.IsActive
	}

	return service.save(ctx, querier, *existingShop, changedShop)
}

func (service *Service) Close(ctx context.Context, querier database.Querier, companyId uuid.UUID, shopId uuid.UUID) (ShopView, error) {
	existingShop, findError := service.repository.Find(ctx, querier, companyId, shopId)
	if findError != nil {
		return ShopView{}, findError
	}
	if existingShop == nil {
		return ShopView{}, ErrShopNotFound
	}

	closedShop := *existingShop
	closedShop.IsActive = false
	closedShop.UpdatedAt = time.Now().UTC()

	return service.save(ctx, querier, *existingShop, closedShop)
}

func (service *Service) DeletePermanently(ctx context.Context, querier database.Querier, companyId uuid.UUID, currentShopId *uuid.UUID, shopId uuid.UUID) error {
	existingShop, findError := service.repository.Find(ctx, querier, companyId, shopId)
	if findError != nil {
		return findError
	}
	if existingShop == nil {
		return ErrShopNotFound
	}
	isCurrentShop := currentShopId != nil && *currentShopId == shopId
	if isCurrentShop {
		return ErrShopIsCurrent
	}
	if existingShop.IsActive {
		activeCount, countError := service.repository.CountActive(ctx, querier, companyId)
		if countError != nil {
			return countError
		}
		if activeCount <= 1 {
			return ErrLastActiveShop
		}
	}

	isUsed, usedError := service.repository.IsUsed(ctx, querier, companyId, shopId)
	if usedError != nil {
		return usedError
	}
	if isUsed {
		return ErrShopInUse
	}
	hasStaffOnlyHere, staffError := service.repository.HasStaffOnlyHere(ctx, querier, companyId, shopId)
	if staffError != nil {
		return staffError
	}
	if hasStaffOnlyHere {
		return ErrShopHasStaff
	}

	return service.repository.Delete(ctx, querier, companyId, shopId)
}

func (service *Service) save(ctx context.Context, querier database.Querier, existingShop Shop, changedShop Shop) (ShopView, error) {
	isClosing := existingShop.IsActive && !changedShop.IsActive
	if isClosing {
		activeCount, countError := service.repository.CountActive(ctx, querier, existingShop.CompanyId)
		if countError != nil {
			return ShopView{}, countError
		}
		if activeCount <= 1 {
			return ShopView{}, ErrLastActiveShop
		}
	}

	updateError := service.repository.Update(ctx, querier, changedShop)
	if database.IsUniqueViolation(updateError) {
		return ShopView{}, ErrShopNameTaken
	}
	if updateError != nil {
		return ShopView{}, updateError
	}

	return toView(changedShop), nil
}

func trimmedOrNil(value *string) *string {
	if value == nil {
		return nil
	}
	trimmedValue := strings.TrimSpace(*value)
	if trimmedValue == "" {
		return nil
	}
	return &trimmedValue
}

func receiptPrefixOrDefault(receiptPrefix string) string {
	upperPrefix := strings.ToUpper(strings.TrimSpace(receiptPrefix))
	if upperPrefix == "" {
		return defaultReceiptPrefix
	}
	return upperPrefix
}
