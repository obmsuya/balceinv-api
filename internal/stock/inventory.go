package stock

import (
	"context"
	"errors"
	"strings"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/identity"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/google/uuid"
)

var (
	ErrNoActiveShop      = errors.New("choose a shop first")
	ErrProductNotFound   = errors.New("product not found")
	ErrWrongDirection    = errors.New("stock received or returned must go up, and damaged stock must go down")
	ErrInvalidFilter     = errors.New("the filter is not valid")
	knownMovementReasons = map[string]bool{
		"opening": true, "sale": true, "return": true, "purchase": true,
		"adjustment": true, "damage": true, "transfer_in": true, "transfer_out": true,
	}
)

func (service *Service) Levels(ctx context.Context, querier database.Querier, principal *identity.Principal, filter LevelFilter, limit int, offset int) (response.Page[LevelView], error) {
	if principal.ShopId == nil {
		return response.Page[LevelView]{}, ErrNoActiveShop
	}
	isKnownStatus := filter.Status == "" || filter.Status == StatusLow || filter.Status == StatusOut
	if !isKnownStatus {
		return response.Page[LevelView]{}, ErrInvalidFilter
	}

	totalLevels, countError := service.repository.CountLevels(ctx, querier, principal.CompanyId, *principal.ShopId, filter)
	if countError != nil {
		return response.Page[LevelView]{}, countError
	}

	levelViews, listError := service.repository.ListLevels(ctx, querier, principal.CompanyId, *principal.ShopId, filter, limit, offset)
	if listError != nil {
		return response.Page[LevelView]{}, listError
	}

	levelPage := response.Page[LevelView]{
		Items:  levelViews,
		Total:  totalLevels,
		Limit:  limit,
		Offset: offset,
	}
	return levelPage, nil
}

func (service *Service) Summary(ctx context.Context, querier database.Querier, principal *identity.Principal) (SummaryView, error) {
	if principal.ShopId == nil {
		return SummaryView{}, ErrNoActiveShop
	}
	return service.repository.Summarize(ctx, querier, principal.CompanyId, *principal.ShopId)
}

func (service *Service) Movements(ctx context.Context, querier database.Querier, principal *identity.Principal, filter MovementFilter, limit int, offset int) (response.Page[MovementView], error) {
	if principal.ShopId == nil {
		return response.Page[MovementView]{}, ErrNoActiveShop
	}
	isKnownReason := filter.Reason == "" || knownMovementReasons[filter.Reason]
	if !isKnownReason {
		return response.Page[MovementView]{}, ErrInvalidFilter
	}

	totalMovements, countError := service.repository.CountMovements(ctx, querier, principal.CompanyId, *principal.ShopId, filter)
	if countError != nil {
		return response.Page[MovementView]{}, countError
	}

	movementViews, listError := service.repository.ListMovements(ctx, querier, principal.CompanyId, *principal.ShopId, filter, limit, offset)
	if listError != nil {
		return response.Page[MovementView]{}, listError
	}

	movementPage := response.Page[MovementView]{
		Items:  movementViews,
		Total:  totalMovements,
		Limit:  limit,
		Offset: offset,
	}
	return movementPage, nil
}

func (service *Service) Adjust(ctx context.Context, querier database.Querier, principal *identity.Principal, request AdjustmentRequest) (MovementView, error) {
	if principal.ShopId == nil {
		return MovementView{}, ErrNoActiveShop
	}

	mustIncrease := request.Reason == "purchase" || request.Reason == "return"
	mustDecrease := request.Reason == "damage"
	isWrongDirection := (mustIncrease && request.Change < 0) || (mustDecrease && request.Change > 0)
	if isWrongDirection {
		return MovementView{}, ErrWrongDirection
	}

	productId := uuid.MustParse(request.ProductId)
	isActiveProduct, findError := service.repository.IsActiveProduct(ctx, querier, principal.CompanyId, productId)
	if findError != nil {
		return MovementView{}, findError
	}
	if !isActiveProduct {
		return MovementView{}, ErrProductNotFound
	}

	movementRequest := MovementRequest{
		CompanyId: principal.CompanyId,
		ShopId:    *principal.ShopId,
		ProductId: productId,
		Change:    request.Change,
		Reason:    request.Reason,
		Reference: trimmedOrNil(request.Reference),
		UserId:    &principal.UserId,
	}
	recordedMovement, recordError := service.RecordMovement(ctx, querier, movementRequest)
	if recordError != nil {
		return MovementView{}, recordError
	}

	movementFilter := MovementFilter{
		ProductId: &productId,
	}
	latestMovements, listError := service.repository.ListMovements(ctx, querier, principal.CompanyId, *principal.ShopId, movementFilter, 1, 0)
	if listError != nil {
		return MovementView{}, listError
	}
	for _, latestMovement := range latestMovements {
		if latestMovement.Id == recordedMovement.Id {
			return latestMovement, nil
		}
	}
	return MovementView{}, errors.New("failed to read back the recorded stock movement")
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
