package discounts

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/identity"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/google/uuid"
)

const maximumPercentBasisPoints = 10000

var (
	ErrDiscountNotFound = errors.New("discount not found")
	ErrProductNotFound  = errors.New("product not found")
	ErrPercentTooLarge  = errors.New("a percentage discount can be at most 100%")
	ErrEndsBeforeStart  = errors.New("the discount must end after it starts")
)

type Service struct {
	repository *Repository
}

func NewService(repository *Repository) *Service {
	return &Service{
		repository: repository,
	}
}

func (service *Service) List(ctx context.Context, querier database.Querier, companyId uuid.UUID, limit int, offset int) (response.Page[DiscountView], error) {
	totalDiscounts, countError := service.repository.Count(ctx, querier, companyId)
	if countError != nil {
		return response.Page[DiscountView]{}, countError
	}

	foundDiscounts, productNames, variantLabels, listError := service.repository.List(ctx, querier, companyId, limit, offset)
	if listError != nil {
		return response.Page[DiscountView]{}, listError
	}

	now := time.Now().UTC()
	discountViews := make([]DiscountView, 0, len(foundDiscounts))
	for discountIndex, foundDiscount := range foundDiscounts {
		discountViews = append(discountViews, toView(foundDiscount, productNames[discountIndex], variantLabels[discountIndex], now))
	}

	discountPage := response.Page[DiscountView]{
		Items:  discountViews,
		Total:  totalDiscounts,
		Limit:  limit,
		Offset: offset,
	}
	return discountPage, nil
}

func (service *Service) Get(ctx context.Context, querier database.Querier, companyId uuid.UUID, discountId uuid.UUID) (DiscountView, error) {
	foundDiscount, productName, variantLabel, findError := service.repository.Find(ctx, querier, companyId, discountId)
	if findError != nil {
		return DiscountView{}, findError
	}
	if foundDiscount == nil {
		return DiscountView{}, ErrDiscountNotFound
	}
	return toView(*foundDiscount, productName, variantLabel, time.Now().UTC()), nil
}

func (service *Service) Create(ctx context.Context, querier database.Querier, principal *identity.Principal, request DiscountRequest) (DiscountView, error) {
	createdAt := time.Now().UTC()
	newDiscount := Discount{
		Id:        uuid.Must(uuid.NewV7()),
		CompanyId: principal.CompanyId,
		IsActive:  true,
		CreatedBy: &principal.UserId,
		CreatedAt: createdAt,
	}

	fillError := service.fill(ctx, querier, &newDiscount, request)
	if fillError != nil {
		return DiscountView{}, fillError
	}

	insertError := service.repository.Insert(ctx, querier, newDiscount)
	if insertError != nil {
		return DiscountView{}, insertError
	}

	return service.Get(ctx, querier, principal.CompanyId, newDiscount.Id)
}

func (service *Service) Update(ctx context.Context, querier database.Querier, principal *identity.Principal, discountId uuid.UUID, request DiscountRequest) (DiscountView, error) {
	existingDiscount, _, _, findError := service.repository.Find(ctx, querier, principal.CompanyId, discountId)
	if findError != nil {
		return DiscountView{}, findError
	}
	if existingDiscount == nil {
		return DiscountView{}, ErrDiscountNotFound
	}

	changedDiscount := *existingDiscount
	fillError := service.fill(ctx, querier, &changedDiscount, request)
	if fillError != nil {
		return DiscountView{}, fillError
	}
	if request.IsActive != nil {
		changedDiscount.IsActive = *request.IsActive
	}

	updateError := service.repository.Update(ctx, querier, changedDiscount)
	if updateError != nil {
		return DiscountView{}, updateError
	}

	return service.Get(ctx, querier, principal.CompanyId, discountId)
}

func (service *Service) Stop(ctx context.Context, querier database.Querier, principal *identity.Principal, discountId uuid.UUID) (DiscountView, error) {
	existingDiscount, _, _, findError := service.repository.Find(ctx, querier, principal.CompanyId, discountId)
	if findError != nil {
		return DiscountView{}, findError
	}
	if existingDiscount == nil {
		return DiscountView{}, ErrDiscountNotFound
	}

	stoppedDiscount := *existingDiscount
	stoppedDiscount.IsActive = false
	stoppedDiscount.UpdatedAt = time.Now().UTC()

	updateError := service.repository.Update(ctx, querier, stoppedDiscount)
	if updateError != nil {
		return DiscountView{}, updateError
	}

	return service.Get(ctx, querier, principal.CompanyId, discountId)
}

func (service *Service) Applicable(ctx context.Context, querier database.Querier, companyId uuid.UUID, productIds []uuid.UUID, now time.Time) ([]Discount, error) {
	return service.repository.ListApplicable(ctx, querier, companyId, productIds, now)
}

func (service *Service) fill(ctx context.Context, querier database.Querier, discount *Discount, request DiscountRequest) error {
	isPercentTooLarge := request.Kind == KindPercent && request.Value > maximumPercentBasisPoints
	if isPercentTooLarge {
		return ErrPercentTooLarge
	}
	startsAt := request.StartsAt.UTC()
	endsAt := request.EndsAt.UTC()
	if !endsAt.After(startsAt) {
		return ErrEndsBeforeStart
	}

	var productId *uuid.UUID
	if request.ProductId != nil {
		parsedProductId := uuid.MustParse(*request.ProductId)
		isActiveProduct, findError := service.repository.IsActiveProduct(ctx, querier, discount.CompanyId, parsedProductId)
		if findError != nil {
			return findError
		}
		if !isActiveProduct {
			return ErrProductNotFound
		}
		productId = &parsedProductId
	}

	discount.Name = strings.TrimSpace(request.Name)
	discount.ProductId = productId
	discount.Kind = request.Kind
	discount.Value = request.Value
	discount.StartsAt = startsAt
	discount.EndsAt = endsAt
	discount.UpdatedAt = time.Now().UTC()
	return nil
}

func toView(discount Discount, productName *string, variantLabel *string, now time.Time) DiscountView {
	return DiscountView{
		Id:           discount.Id,
		Name:         discount.Name,
		ProductId:    discount.ProductId,
		ProductName:  productName,
		VariantLabel: variantLabel,
		Kind:         discount.Kind,
		Value:        discount.Value,
		StartsAt:     discount.StartsAt,
		EndsAt:       discount.EndsAt,
		IsActive:     discount.IsActive,
		Status:       statusAt(discount, now),
		CreatedAt:    discount.CreatedAt,
		UpdatedAt:    discount.UpdatedAt,
	}
}
