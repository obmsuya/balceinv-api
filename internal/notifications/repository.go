package notifications

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/shopscope"
	"github.com/google/uuid"
)

type Repository struct{}

func NewRepository() *Repository {
	return &Repository{}
}

func (repository *Repository) Insert(ctx context.Context, querier database.Querier, newNotification Notification) error {
	query := `
		INSERT INTO notifications (id, company_id, shop_id, product_id, kind, quantity, min_stock, is_read, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`

	_, insertError := querier.ExecContext(ctx, query,
		newNotification.Id,
		newNotification.CompanyId,
		newNotification.ShopId,
		newNotification.ProductId,
		newNotification.Kind,
		newNotification.Quantity,
		newNotification.MinStock,
		newNotification.IsRead,
		newNotification.CreatedAt,
	)
	if insertError != nil {
		return fmt.Errorf("failed to insert notification: %w", insertError)
	}

	return nil
}

func (repository *Repository) Count(ctx context.Context, querier database.Querier, companyId uuid.UUID, scope shopscope.Scope, unreadOnly bool) (int64, error) {
	arguments := []any{companyId, unreadOnly}
	query := `
		SELECT COUNT(*)
		FROM notifications n
		WHERE n.company_id = $1 AND (NOT n.is_read OR NOT $2)` + scope.Condition("n.shop_id", &arguments)

	notificationCount := int64(0)
	scanError := querier.QueryRowContext(ctx, query, arguments...).Scan(&notificationCount)
	if scanError != nil {
		return 0, fmt.Errorf("failed to count notifications: %w", scanError)
	}

	return notificationCount, nil
}

func (repository *Repository) List(ctx context.Context, querier database.Querier, companyId uuid.UUID, scope shopscope.Scope, unreadOnly bool, limit int, offset int) ([]NotificationView, error) {
	arguments := []any{companyId, unreadOnly}
	shopCondition := scope.Condition("n.shop_id", &arguments)
	arguments = append(arguments, limit, offset)
	query := `
		SELECT n.id, n.kind, n.shop_id, sh.name, n.product_id, p.name, p.variant_label, p.sku, p.unit, n.quantity, n.min_stock,
		       COALESCE(ss.quantity, 0), n.is_read, n.created_at, n.read_at
		FROM notifications n
		JOIN shops sh ON sh.company_id = n.company_id AND sh.id = n.shop_id
		JOIN products p ON p.company_id = n.company_id AND p.id = n.product_id
		LEFT JOIN shop_stock ss ON ss.company_id = n.company_id AND ss.shop_id = n.shop_id AND ss.product_id = n.product_id
		WHERE n.company_id = $1 AND (NOT n.is_read OR NOT $2)` + shopCondition + `
		ORDER BY n.created_at DESC, n.id DESC
		LIMIT $` + strconv.Itoa(len(arguments)-1) + ` OFFSET $` + strconv.Itoa(len(arguments))

	notificationRows, queryError := querier.QueryContext(ctx, query, arguments...)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list notifications: %w", queryError)
	}
	defer notificationRows.Close()

	notificationViews := []NotificationView{}
	for notificationRows.Next() {
		notificationView := NotificationView{}
		scanError := notificationRows.Scan(
			&notificationView.Id,
			&notificationView.Kind,
			&notificationView.ShopId,
			&notificationView.ShopName,
			&notificationView.ProductId,
			&notificationView.ProductName,
			&notificationView.VariantLabel,
			&notificationView.Sku,
			&notificationView.Unit,
			&notificationView.Quantity,
			&notificationView.MinStock,
			&notificationView.CurrentQuantity,
			&notificationView.IsRead,
			&notificationView.CreatedAt,
			&notificationView.ReadAt,
		)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan notification: %w", scanError)
		}
		notificationViews = append(notificationViews, notificationView)
	}

	return notificationViews, notificationRows.Err()
}

func (repository *Repository) MarkRead(ctx context.Context, querier database.Querier, companyId uuid.UUID, scope shopscope.Scope, notificationId uuid.UUID) (int64, error) {
	arguments := []any{companyId, notificationId, time.Now().UTC()}
	query := `
		UPDATE notifications
		SET is_read = TRUE, read_at = $3
		WHERE company_id = $1 AND id = $2 AND NOT is_read` + scope.Condition("shop_id", &arguments)

	return execCount(ctx, querier, "mark notification read", query, arguments...)
}

func (repository *Repository) MarkAllRead(ctx context.Context, querier database.Querier, companyId uuid.UUID, scope shopscope.Scope) (int64, error) {
	arguments := []any{companyId, time.Now().UTC()}
	query := `
		UPDATE notifications
		SET is_read = TRUE, read_at = $2
		WHERE company_id = $1 AND NOT is_read` + scope.Condition("shop_id", &arguments)

	return execCount(ctx, querier, "mark notifications read", query, arguments...)
}

func (repository *Repository) Exists(ctx context.Context, querier database.Querier, companyId uuid.UUID, scope shopscope.Scope, notificationId uuid.UUID) (bool, error) {
	arguments := []any{companyId, notificationId}
	query := `SELECT COUNT(*) FROM notifications WHERE company_id = $1 AND id = $2` + scope.Condition("shop_id", &arguments)

	matchCount := 0
	scanError := querier.QueryRowContext(ctx, query, arguments...).Scan(&matchCount)
	if scanError != nil {
		return false, fmt.Errorf("failed to find notification: %w", scanError)
	}

	return matchCount > 0, nil
}

func (repository *Repository) DeleteRead(ctx context.Context, querier database.Querier, companyId uuid.UUID, scope shopscope.Scope) (int64, error) {
	arguments := []any{companyId}
	query := `DELETE FROM notifications WHERE company_id = $1 AND is_read` + scope.Condition("shop_id", &arguments)

	return execCount(ctx, querier, "clear read notifications", query, arguments...)
}

func execCount(ctx context.Context, querier database.Querier, action string, query string, arguments ...any) (int64, error) {
	execResult, execError := querier.ExecContext(ctx, query, arguments...)
	if execError != nil {
		return 0, fmt.Errorf("failed to %s: %w", action, execError)
	}

	changedCount, countError := execResult.RowsAffected()
	if countError != nil {
		return 0, fmt.Errorf("failed to count changed notifications: %w", countError)
	}

	return changedCount, nil
}
