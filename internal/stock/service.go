package stock

import (
	"context"
	"errors"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/notifications"
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
	repository              *Repository
	notificationsRepository *notifications.Repository
}

func NewService(repository *Repository, notificationsRepository *notifications.Repository) *Service {
	return &Service{
		repository:              repository,
		notificationsRepository: notificationsRepository,
	}
}

func (service *Service) RecordMovement(ctx context.Context, querier database.Querier, request MovementRequest) (Movement, error) {
	ensureError := service.repository.EnsureShopStock(ctx, querier, request.CompanyId, request.ShopId, request.ProductId, DefaultMinimumStock)
	if ensureError != nil {
		return Movement{}, ensureError
	}

	quantityAfter, minimumStock, wasApplied, changeError := service.repository.ChangeQuantity(ctx, querier, request.CompanyId, request.ShopId, request.ProductId, request.Change)
	if changeError != nil {
		return Movement{}, changeError
	}
	if !wasApplied {
		return Movement{}, ErrInsufficientStock
	}

	recordedAt := time.Now().UTC()
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
		CreatedAt:     recordedAt,
	}

	insertError := service.repository.InsertMovement(ctx, querier, recordedMovement)
	if insertError != nil {
		return Movement{}, insertError
	}

	quantityBefore := quantityAfter - request.Change
	crossedKind := crossedThreshold(quantityBefore, quantityAfter, minimumStock)
	if crossedKind == "" {
		return recordedMovement, nil
	}

	stockNotification := notifications.Notification{
		Id:        uuid.Must(uuid.NewV7()),
		CompanyId: request.CompanyId,
		ShopId:    request.ShopId,
		ProductId: request.ProductId,
		Kind:      crossedKind,
		Quantity:  quantityAfter,
		MinStock:  minimumStock,
		CreatedAt: recordedAt,
	}
	notifyError := service.notificationsRepository.Insert(ctx, querier, stockNotification)
	if notifyError != nil {
		return Movement{}, notifyError
	}

	return recordedMovement, nil
}

func crossedThreshold(quantityBefore int, quantityAfter int, minimumStock int) string {
	isNowOut := quantityAfter == 0 && quantityBefore > 0
	if isNowOut {
		return notifications.KindOutOfStock
	}

	isNowLow := quantityAfter > 0 && quantityAfter <= minimumStock && quantityBefore > minimumStock
	if isNowLow {
		return notifications.KindLowStock
	}

	return ""
}
