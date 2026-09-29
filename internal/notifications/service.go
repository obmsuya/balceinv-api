package notifications

import (
	"context"
	"errors"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/identity"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/google/uuid"
)

var (
	ErrNoActiveShop         = errors.New("choose a shop first")
	ErrNotificationNotFound = errors.New("notification not found")
)

type Service struct {
	repository *Repository
}

func NewService(repository *Repository) *Service {
	return &Service{
		repository: repository,
	}
}

func (service *Service) List(ctx context.Context, querier database.Querier, principal *identity.Principal, unreadOnly bool, limit int, offset int) (response.Page[NotificationView], error) {
	if principal.ShopId == nil {
		return response.Page[NotificationView]{}, ErrNoActiveShop
	}

	totalNotifications, countError := service.repository.Count(ctx, querier, principal.CompanyId, *principal.ShopId, unreadOnly)
	if countError != nil {
		return response.Page[NotificationView]{}, countError
	}

	notificationViews, listError := service.repository.List(ctx, querier, principal.CompanyId, *principal.ShopId, unreadOnly, limit, offset)
	if listError != nil {
		return response.Page[NotificationView]{}, listError
	}

	notificationPage := response.Page[NotificationView]{
		Items:  notificationViews,
		Total:  totalNotifications,
		Limit:  limit,
		Offset: offset,
	}
	return notificationPage, nil
}

func (service *Service) UnreadCount(ctx context.Context, querier database.Querier, principal *identity.Principal) (UnreadCountView, error) {
	if principal.ShopId == nil {
		return UnreadCountView{}, nil
	}

	unreadCount, countError := service.repository.Count(ctx, querier, principal.CompanyId, *principal.ShopId, true)
	if countError != nil {
		return UnreadCountView{}, countError
	}
	return UnreadCountView{Count: unreadCount}, nil
}

func (service *Service) MarkRead(ctx context.Context, querier database.Querier, principal *identity.Principal, notificationId uuid.UUID) (ChangedCountView, error) {
	if principal.ShopId == nil {
		return ChangedCountView{}, ErrNoActiveShop
	}

	notificationExists, findError := service.repository.Exists(ctx, querier, principal.CompanyId, *principal.ShopId, notificationId)
	if findError != nil {
		return ChangedCountView{}, findError
	}
	if !notificationExists {
		return ChangedCountView{}, ErrNotificationNotFound
	}

	changedCount, markError := service.repository.MarkRead(ctx, querier, principal.CompanyId, *principal.ShopId, notificationId)
	if markError != nil {
		return ChangedCountView{}, markError
	}
	return ChangedCountView{Changed: changedCount}, nil
}

func (service *Service) MarkAllRead(ctx context.Context, querier database.Querier, principal *identity.Principal) (ChangedCountView, error) {
	if principal.ShopId == nil {
		return ChangedCountView{}, ErrNoActiveShop
	}

	changedCount, markError := service.repository.MarkAllRead(ctx, querier, principal.CompanyId, *principal.ShopId)
	if markError != nil {
		return ChangedCountView{}, markError
	}
	return ChangedCountView{Changed: changedCount}, nil
}

func (service *Service) ClearRead(ctx context.Context, querier database.Querier, principal *identity.Principal) (ChangedCountView, error) {
	if principal.ShopId == nil {
		return ChangedCountView{}, ErrNoActiveShop
	}

	removedCount, clearError := service.repository.DeleteRead(ctx, querier, principal.CompanyId, *principal.ShopId)
	if clearError != nil {
		return ChangedCountView{}, clearError
	}
	return ChangedCountView{Changed: removedCount}, nil
}
