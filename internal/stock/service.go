package stock

import (
	"context"
	"errors"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/accounting"
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
	ledger                  *accounting.Ledger
}

func NewService(repository *Repository, notificationsRepository *notifications.Repository, ledger *accounting.Ledger) *Service {
	return &Service{
		repository:              repository,
		notificationsRepository: notificationsRepository,
		ledger:                  ledger,
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

func (service *Service) RecordAndBookMovement(ctx context.Context, querier database.Querier, request MovementRequest) (Movement, error) {
	recordedMovement, recordError := service.RecordMovement(ctx, querier, request)
	if recordError != nil {
		return Movement{}, recordError
	}
	postError := service.ledger.PostStockMovementById(ctx, querier, request.CompanyId, recordedMovement.Id)
	if postError != nil {
		return Movement{}, postError
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

func (service *Service) RemoveProductStock(ctx context.Context, querier database.Querier, companyId uuid.UUID, productIds []uuid.UUID, removedBy uuid.UUID) error {
	movementIds, listError := service.repository.MovementIdsOf(ctx, querier, companyId, productIds)
	if listError != nil {
		return listError
	}
	removedAt := time.Now().UTC()
	for _, movementId := range movementIds {
		movementReversal := accounting.ReversalPosting{
			CompanyId:          companyId,
			OriginalSourceType: accounting.SourceStockAdjustment,
			OriginalSourceId:   movementId,
			SourceType:         accounting.SourceReversal,
			ReversedAt:         removedAt,
			Reason:             "product deleted",
			UserId:             &removedBy,
		}
		_, reverseError := service.ledger.ReverseSource(ctx, querier, movementReversal)
		if reverseError != nil {
			return reverseError
		}
	}
	return service.repository.DeleteProductStock(ctx, querier, companyId, productIds)
}
