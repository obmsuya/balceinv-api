package suppliers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/google/uuid"
)

const orderViewColumns = `
	po.id, po.order_number, po.supplier_id, su.name, po.shop_id, sh.name, po.status, po.expected_date, po.note,
	(SELECT CAST(COALESCE(SUM(ol.quantity_ordered * ol.expected_unit_cost), 0) AS BIGINT) FROM purchase_order_lines ol WHERE ol.company_id = po.company_id AND ol.purchase_order_id = po.id),
	(SELECT COUNT(*) FROM purchase_order_lines ol WHERE ol.company_id = po.company_id AND ol.purchase_order_id = po.id),
	cu.name, po.created_at, po.sent_at, xu.name, po.cancelled_at, po.cancel_reason
`

const orderViewJoins = `
	FROM purchase_orders po
	JOIN shops sh ON sh.company_id = po.company_id AND sh.id = po.shop_id
	JOIN suppliers su ON su.company_id = po.company_id AND su.id = po.supplier_id
	LEFT JOIN users cu ON cu.company_id = po.company_id AND cu.id = po.created_by
	LEFT JOIN users xu ON xu.company_id = po.company_id AND xu.id = po.cancelled_by
`

func (repository *Repository) InsertOrder(ctx context.Context, querier database.Querier, newOrder PurchaseOrder, orderLines []OrderLine) error {
	orderQuery := `
		INSERT INTO purchase_orders (id, company_id, order_number, supplier_id, shop_id, status, expected_date, note, created_by, created_at, sent_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`

	_, orderError := querier.ExecContext(ctx, orderQuery,
		newOrder.Id,
		newOrder.CompanyId,
		newOrder.OrderNumber,
		newOrder.SupplierId,
		newOrder.ShopId,
		newOrder.Status,
		newOrder.ExpectedDate,
		newOrder.Note,
		newOrder.CreatedBy,
		newOrder.CreatedAt,
		newOrder.SentAt,
	)
	if orderError != nil {
		return fmt.Errorf("failed to insert order: %w", orderError)
	}

	for position, orderLine := range orderLines {
		lineQuery := `
			INSERT INTO purchase_order_lines (id, company_id, purchase_order_id, position, product_id, quantity_ordered, expected_unit_cost)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`
		_, lineError := querier.ExecContext(ctx, lineQuery,
			uuid.Must(uuid.NewV7()),
			newOrder.CompanyId,
			newOrder.Id,
			position,
			orderLine.ProductId,
			orderLine.QuantityOrdered,
			orderLine.ExpectedUnitCost,
		)
		if lineError != nil {
			return fmt.Errorf("failed to insert order line: %w", lineError)
		}
	}

	return nil
}

func scanOrder(row rowScanner) (OrderView, error) {
	orderView := OrderView{}
	scanError := row.Scan(
		&orderView.Id,
		&orderView.OrderNumber,
		&orderView.SupplierId,
		&orderView.SupplierName,
		&orderView.ShopId,
		&orderView.ShopName,
		&orderView.Status,
		&orderView.ExpectedDate,
		&orderView.Note,
		&orderView.ExpectedTotal,
		&orderView.LineCount,
		&orderView.CreatedByName,
		&orderView.CreatedAt,
		&orderView.SentAt,
		&orderView.CancelledByName,
		&orderView.CancelledAt,
		&orderView.CancelReason,
	)
	return orderView, scanError
}

func (repository *Repository) FindOrder(ctx context.Context, querier database.Querier, companyId uuid.UUID, orderId uuid.UUID) (*OrderView, error) {
	query := `SELECT ` + orderViewColumns + orderViewJoins + ` WHERE po.company_id = $1 AND po.id = $2`

	orderView, scanError := scanOrder(querier.QueryRowContext(ctx, query, companyId, orderId))
	if errors.Is(scanError, sql.ErrNoRows) {
		return nil, nil
	}
	if scanError != nil {
		return nil, fmt.Errorf("failed to find order: %w", scanError)
	}

	return &orderView, nil
}

func (repository *Repository) CountOrders(ctx context.Context, querier database.Querier, companyId uuid.UUID, filter DocumentFilter) (int64, error) {
	query := `SELECT COUNT(*) FROM purchase_orders po WHERE po.company_id = $1 AND ($2 = '' OR po.supplier_id = $3) AND ($4 = '' OR po.status = $4)`

	supplierText, supplierArgument := optionalIdArguments(filter.SupplierId)
	orderCount := int64(0)
	scanError := querier.QueryRowContext(ctx, query, companyId, supplierText, supplierArgument, filter.Status).Scan(&orderCount)
	if scanError != nil {
		return 0, fmt.Errorf("failed to count orders: %w", scanError)
	}

	return orderCount, nil
}

func (repository *Repository) ListOrders(ctx context.Context, querier database.Querier, companyId uuid.UUID, filter DocumentFilter, limit int, offset int) ([]OrderView, error) {
	query := `
		SELECT ` + orderViewColumns + orderViewJoins + `
		WHERE po.company_id = $1 AND ($2 = '' OR po.supplier_id = $3) AND ($4 = '' OR po.status = $4)
		ORDER BY po.created_at DESC, po.order_number DESC
		LIMIT $5 OFFSET $6
	`

	supplierText, supplierArgument := optionalIdArguments(filter.SupplierId)
	orderRows, queryError := querier.QueryContext(ctx, query, companyId, supplierText, supplierArgument, filter.Status, limit, offset)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list orders: %w", queryError)
	}
	defer orderRows.Close()

	orderViews := []OrderView{}
	for orderRows.Next() {
		orderView, scanError := scanOrder(orderRows)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan order: %w", scanError)
		}
		orderViews = append(orderViews, orderView)
	}

	return orderViews, orderRows.Err()
}

func (repository *Repository) ListOrderLines(ctx context.Context, querier database.Querier, companyId uuid.UUID, orderId uuid.UUID) ([]OrderLineView, error) {
	query := `
		SELECT ol.product_id, p.name, p.variant_label, p.sku, p.unit, p.cost_price, ol.quantity_ordered, ol.expected_unit_cost, ol.quantity_received
		FROM purchase_order_lines ol
		JOIN products p ON p.company_id = ol.company_id AND p.id = ol.product_id
		WHERE ol.company_id = $1 AND ol.purchase_order_id = $2
		ORDER BY ol.position
	`

	lineRows, queryError := querier.QueryContext(ctx, query, companyId, orderId)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list order lines: %w", queryError)
	}
	defer lineRows.Close()

	lineViews := []OrderLineView{}
	for lineRows.Next() {
		lineView := OrderLineView{}
		scanError := lineRows.Scan(&lineView.ProductId, &lineView.ProductName, &lineView.VariantLabel, &lineView.Sku, &lineView.Unit,
			&lineView.CostPrice, &lineView.QuantityOrdered, &lineView.ExpectedUnitCost, &lineView.QuantityReceived)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan order line: %w", scanError)
		}
		lineView.QuantityRemaining = max(lineView.QuantityOrdered-lineView.QuantityReceived, 0)
		lineViews = append(lineViews, lineView)
	}

	return lineViews, lineRows.Err()
}

func (repository *Repository) MarkOrderSent(ctx context.Context, querier database.Querier, companyId uuid.UUID, orderId uuid.UUID, sentAt time.Time) error {
	query := `UPDATE purchase_orders SET status = 'sent', sent_at = $3 WHERE company_id = $1 AND id = $2 AND status = 'draft'`

	_, updateError := querier.ExecContext(ctx, query, companyId, orderId, sentAt)
	if updateError != nil {
		return fmt.Errorf("failed to mark the order as sent: %w", updateError)
	}

	return nil
}

func (repository *Repository) CancelOrder(ctx context.Context, querier database.Querier, companyId uuid.UUID, orderId uuid.UUID, userId uuid.UUID, reason string, cancelledAt time.Time) error {
	query := `
		UPDATE purchase_orders
		SET status = 'cancelled', cancelled_by = $3, cancelled_at = $4, cancel_reason = $5
		WHERE company_id = $1 AND id = $2 AND status IN ('draft', 'sent', 'partly_received')
	`

	_, updateError := querier.ExecContext(ctx, query, companyId, orderId, userId, cancelledAt, reason)
	if updateError != nil {
		return fmt.Errorf("failed to cancel the order: %w", updateError)
	}

	return nil
}

func (repository *Repository) AddReceivedToOrder(ctx context.Context, querier database.Querier, companyId uuid.UUID, orderId uuid.UUID, productId uuid.UUID, quantityChange int) error {
	query := `
		UPDATE purchase_order_lines
		SET quantity_received = CASE WHEN quantity_received + $4 < 0 THEN 0 ELSE quantity_received + $4 END
		WHERE company_id = $1 AND purchase_order_id = $2 AND product_id = $3
	`

	_, updateError := querier.ExecContext(ctx, query, companyId, orderId, productId, quantityChange)
	if updateError != nil {
		return fmt.Errorf("failed to update the received quantity: %w", updateError)
	}

	return nil
}

func (repository *Repository) RefreshOrderStatus(ctx context.Context, querier database.Querier, companyId uuid.UUID, orderId uuid.UUID) error {
	query := `
		UPDATE purchase_orders
		SET status = CASE
			WHEN NOT EXISTS (
				SELECT 1 FROM purchase_order_lines ol
				WHERE ol.company_id = purchase_orders.company_id AND ol.purchase_order_id = purchase_orders.id AND ol.quantity_received < ol.quantity_ordered
			) THEN 'received'
			WHEN EXISTS (
				SELECT 1 FROM purchase_order_lines ol
				WHERE ol.company_id = purchase_orders.company_id AND ol.purchase_order_id = purchase_orders.id AND ol.quantity_received > 0
			) THEN 'partly_received'
			WHEN sent_at IS NULL THEN 'draft'
			ELSE 'sent'
		END
		WHERE company_id = $1 AND id = $2 AND status <> 'cancelled'
	`

	_, updateError := querier.ExecContext(ctx, query, companyId, orderId)
	if updateError != nil {
		return fmt.Errorf("failed to refresh the order status: %w", updateError)
	}

	return nil
}
