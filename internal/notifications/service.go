package notifications

import (
	"context"
	"errors"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/identity"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/chrisostomemataba/balceinv-api/internal/common/shopscope"
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

func (service *Service) List(ctx context.Context, querier database.Querier, principal *identity.Principal, requestedShop string, unreadOnly bool, limit int, offset int) (response.Page[NotificationView], error) {
	scope, scopeError := shopscope.Resolve(ctx, querier, principal, requestedShop)
	if scopeError != nil {
		return response.Page[NotificationView]{}, scopeError
	}

	totalNotifications, countError := service.repository.Count(ctx, querier, principal.CompanyId, scope, unreadOnly)
	if countError != nil {
		return response.Page[NotificationView]{}, countError
	}

	notificationViews, listError := service.repository.List(ctx, querier, principal.CompanyId, scope, unreadOnly, limit, offset)
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

func (service *Service) UnreadCount(ctx context.Context, querier database.Querier, principal *identity.Principal, requestedShop string) (UnreadCountView, error) {
	if principal.ShopId == nil && requestedShop == "" {
		return UnreadCountView{}, nil
	}
	scope, scopeError := shopscope.Resolve(ctx, querier, principal, requestedShop)
	if scopeError != nil {
		return UnreadCountView{}, scopeError
	}

	unreadCount, countError := service.repository.Count(ctx, querier, principal.CompanyId, scope, true)
	if countError != nil {
		return UnreadCountView{}, countError
	}
	return UnreadCountView{Count: unreadCount}, nil
}

func (service *Service) MarkRead(ctx context.Context, querier database.Querier, principal *identity.Principal, requestedShop string, notificationId uuid.UUID) (ChangedCountView, error) {
	scope, scopeError := shopscope.Resolve(ctx, querier, principal, requestedShop)
	if scopeError != nil {
		return ChangedCountView{}, scopeError
	}

	notificationExists, findError := service.repository.Exists(ctx, querier, principal.CompanyId, scope, notificationId)
	if findError != nil {
		return ChangedCountView{}, findError
	}
	if !notificationExists {
		return ChangedCountView{}, ErrNotificationNotFound
	}

	changedCount, markError := service.repository.MarkRead(ctx, querier, principal.CompanyId, scope, notificationId)
	if markError != nil {
		return ChangedCountView{}, markError
	}
	return ChangedCountView{Changed: changedCount}, nil
}

func (service *Service) MarkAllRead(ctx context.Context, querier database.Querier, principal *identity.Principal, requestedShop string) (ChangedCountView, error) {
	scope, scopeError := shopscope.Resolve(ctx, querier, principal, requestedShop)
	if scopeError != nil {
		return ChangedCountView{}, scopeError
	}

	changedCount, markError := service.repository.MarkAllRead(ctx, querier, principal.CompanyId, scope)
	if markError != nil {
		return ChangedCountView{}, markError
	}
	return ChangedCountView{Changed: changedCount}, nil
}

func (service *Service) ClearRead(ctx context.Context, querier database.Querier, principal *identity.Principal, requestedShop string) (ChangedCountView, error) {
	scope, scopeError := shopscope.Resolve(ctx, querier, principal, requestedShop)
	if scopeError != nil {
		return ChangedCountView{}, scopeError
	}

	removedCount, clearError := service.repository.DeleteRead(ctx, querier, principal.CompanyId, scope)
	if clearError != nil {
		return ChangedCountView{}, clearError
	}
	return ChangedCountView{Changed: removedCount}, nil
}
