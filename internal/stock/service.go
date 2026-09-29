package stock

import (
	"context"
	"errors"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/google/uuid"
)

const DefaultMinimumStock = 5

var ErrInsufficientStock = errors.New("there is not enough stock for this change")

type MovementRequest struct {
	CompanyId uuid.UUID
	ShopId    uuid.UUID
	ProductId uuid.UUID
	Change    int
	Reason    string
	Reference *string
	UserId    *uuid.UUID
}

type Service struct {
	repository *Repository
}

func NewService(repository *Repository) *Service {
	return &Service{
		repository: repository,
	}
}

func (service *Service) RecordMovement(ctx context.Context, querier database.Querier, request MovementRequest) (Movement, error) {
	ensureError := service.repository.EnsureShopStock(ctx, querier, request.CompanyId, request.ShopId, request.ProductId, DefaultMinimumStock)
	if ensureError != nil {
		return Movement{}, ensureError
	}

	quantityAfter, wasApplied, changeError := service.repository.ChangeQuantity(ctx, querier, request.CompanyId, request.ShopId, request.ProductId, request.Change)
	if changeError != nil {
		return Movement{}, changeError
	}
	if !wasApplied {
		return Movement{}, ErrInsufficientStock
	}

	recordedMovement := Movement{
		Id:            uuid.Must(uuid.NewV7()),
		CompanyId:     request.CompanyId,
		ShopId:        request.ShopId,
		ProductId:     request.ProductId,
		Change:        request.Change,
		QuantityAfter: quantityAfter,
		Reason:        request.Reason,
		Reference:     request.Reference,
		UserId:        request.UserId,
		CreatedAt:     time.Now().UTC(),
	}

	insertError := service.repository.InsertMovement(ctx, querier, recordedMovement)
	if insertError != nil {
		return Movement{}, insertError
	}

	return recordedMovement, nil
}
