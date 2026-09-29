package orders

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/sales"
	"github.com/google/uuid"
)

const orderViewQuery = `
	SELECT o.id, o.number, o.status, o.customer_id, c.name, c.phone, o.shop_id, sh.name, o.due_date, o.note,
	       o.subtotal, o.discount_total, o.total, o.tax_total, o.tax_rate_basis_points,
	       CAST(COALESCE((
	           SELECT SUM(CASE WHEN p.kind = 'deposit' THEN p.amount ELSE -p.amount END)
	           FROM customer_order_payments p
	           WHERE p.company_id = o.company_id AND p.order_id = o.id
	       ), 0) AS BIGINT),
	       (SELECT COUNT(*) FROM customer_order_lines l WHERE l.company_id = o.company_id AND l.order_id = o.id),
	       o.sale_id, s.receipt_number, creator.name, o.created_at, o.ready_at, o.collected_at, o.cancelled_at,
	       canceller.name, o.cancel_reason
	FROM customer_orders o
	JOIN customers c ON c.company_id = o.company_id AND c.id = o.customer_id
	JOIN shops sh ON sh.company_id = o.company_id AND sh.id = o.shop_id
	JOIN users creator ON creator.company_id = o.company_id AND creator.id = o.created_by
	LEFT JOIN users canceller ON canceller.company_id = o.company_id AND canceller.id = o.cancelled_by
	LEFT JOIN sales s ON s.company_id = o.company_id AND s.id = o.sale_id
`

type Repository struct{}

func NewRepository() *Repository {
	return &Repository{}
}

func (repository *Repository) NextNumber(ctx context.Context, querier database.Querier, companyId uuid.UUID) (int64, error) {
	nextNumber := int64(0)
	scanError := querier.QueryRowContext(ctx, `SELECT COALESCE(MAX(number), 0) + 1 FROM customer_orders WHERE company_id = $1`, companyId).Scan(&nextNumber)
	if scanError != nil {
		return 0, fmt.Errorf("failed to take an order number: %w", scanError)
	}
	return nextNumber, nil
}

func (repository *Repository) Insert(ctx context.Context, querier database.Querier, newOrder Order, pricedLines []sales.PricedLine) error {
	orderQuery := `
		INSERT INTO customer_orders (id, company_id, shop_id, customer_id, number, status, due_date, note, subtotal, discount_total, total,
		                             tax_total, tax_rate_basis_points, created_by, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15)
	`
	_, orderError := querier.ExecContext(ctx, orderQuery,
		newOrder.Id,
		newOrder.CompanyId,
		newOrder.ShopId,
		newOrder.CustomerId,
		newOrder.Number,
		newOrder.Status,
		newOrder.DueDate,
		newOrder.Note,
		newOrder.Subtotal,
		newOrder.DiscountTotal,
		newOrder.Total,
		newOrder.TaxTotal,
		newOrder.TaxRateBasisPoints,
		newOrder.CreatedBy,
		newOrder.CreatedAt,
	)
	if orderError != nil {
		return fmt.Errorf("failed to insert order: %w", orderError)
	}

	for position, pricedLine := range pricedLines {
		lineQuery := `
			INSERT INTO customer_order_lines (id, company_id, order_id, position, product_id, product_name, variant_label, sku, unit, quantity,
			                                  unit_price, unit_cost, is_wholesale, discount_id, discount_name, discount_amount, line_total)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
		`
		_, lineError := querier.ExecContext(ctx, lineQuery,
			uuid.Must(uuid.NewV7()),
			newOrder.CompanyId,
			newOrder.Id,
			position,
			pricedLine.Product.Id,
			pricedLine.Product.Name,
			pricedLine.Product.VariantLabel,
			pricedLine.Product.Sku,
			pricedLine.Product.Unit,
			pricedLine.Quantity,
			pricedLine.UnitPrice,
			pricedLine.Product.CostPrice,
			pricedLine.IsWholesale,
			pricedLine.DiscountId,
			pricedLine.DiscountName,
			pricedLine.DiscountAmount,
			pricedLine.LineTotal,
		)
		if lineError != nil {
			return fmt.Errorf("failed to insert order line: %w", lineError)
		}
	}

	return nil
}

func (repository *Repository) InsertPayment(ctx context.Context, querier database.Querier, orderPayment OrderPayment) error {
	query := `
		INSERT INTO customer_order_payments (id, company_id, order_id, kind, method, amount, created_by, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`
	_, insertError := querier.ExecContext(ctx, query,
		orderPayment.Id,
		orderPayment.CompanyId,
		orderPayment.OrderId,
		orderPayment.Kind,
		orderPayment.Method,
		orderPayment.Amount,
		orderPayment.CreatedBy,
		orderPayment.CreatedAt,
	)
	if insertError != nil {
		return fmt.Errorf("failed to record order payment: %w", insertError)
	}
	return nil
}

func (repository *Repository) Lock(ctx context.Context, querier database.Querier, companyId uuid.UUID, shopId uuid.UUID, orderId uuid.UUID) (bool, error) {
	lockResult, lockError := querier.ExecContext(ctx, `UPDATE customer_orders SET status = status WHERE company_id = $1 AND shop_id = $2 AND id = $3`, companyId, shopId, orderId)
	if lockError != nil {
		return false, fmt.Errorf("failed to lock order: %w", lockError)
	}
	lockedCount, countError := lockResult.RowsAffected()
	if countError != nil {
		return false, fmt.Errorf("failed to count locked orders: %w", countError)
	}
	return lockedCount == 1, nil
}

func (repository *Repository) MarkReady(ctx context.Context, querier database.Querier, companyId uuid.UUID, orderId uuid.UUID, readyAt time.Time) error {
	_, updateError := querier.ExecContext(ctx, `UPDATE customer_orders SET status = 'ready', ready_at = $3 WHERE company_id = $1 AND id = $2`, companyId, orderId, readyAt)
	if updateError != nil {
		return fmt.Errorf("failed to mark the order ready: %w", updateError)
	}
	return nil
}

func (repository *Repository) MarkCollected(ctx context.Context, querier database.Querier, companyId uuid.UUID, orderId uuid.UUID, saleId uuid.UUID, collectedAt time.Time) error {
	query := `UPDATE customer_orders SET status = 'collected', sale_id = $3, collected_at = $4 WHERE company_id = $1 AND id = $2`
	_, updateError := querier.ExecContext(ctx, query, companyId, orderId, saleId, collectedAt)
	if updateError != nil {
		return fmt.Errorf("failed to mark the order collected: %w", updateError)
	}
	return nil
}

func (repository *Repository) MarkCancelled(ctx context.Context, querier database.Querier, companyId uuid.UUID, orderId uuid.UUID, cancelledBy uuid.UUID, reason string, cancelledAt time.Time) error {
	query := `
		UPDATE customer_orders
		SET status = 'cancelled', cancelled_at = $5, cancelled_by = $3, cancel_reason = $4
		WHERE company_id = $1 AND id = $2
	`
	_, updateError := querier.ExecContext(ctx, query, companyId, orderId, cancelledBy, reason, cancelledAt)
	if updateError != nil {
		return fmt.Errorf("failed to cancel the order: %w", updateError)
	}
	return nil
}

func (repository *Repository) Find(ctx context.Context, querier database.Querier, companyId uuid.UUID, shopId uuid.UUID, orderId uuid.UUID) (*OrderView, error) {
	query := orderViewQuery + ` WHERE o.company_id = $1 AND o.shop_id = $2 AND o.id = $3`

	orderRows, queryError := querier.QueryContext(ctx, query, companyId, shopId, orderId)
	if queryError != nil {
		return nil, fmt.Errorf("failed to find order: %w", queryError)
	}
	orderViews, scanError := scanOrders(orderRows)
	if scanError != nil {
		return nil, scanError
	}
	if len(orderViews) == 0 {
		return nil, nil
	}
	return &orderViews[0], nil
}

func (repository *Repository) List(ctx context.Context, querier database.Querier, companyId uuid.UUID, shopId uuid.UUID, filter OrderFilter, limit int, offset int) ([]OrderView, int64, error) {
	conditions := []string{"o.company_id = $1", "o.shop_id = $2"}
	arguments := []any{companyId, shopId}
	addArgument := func(argument any) string {
		arguments = append(arguments, argument)
		return "$" + strconv.Itoa(len(arguments))
	}

	if filter.Status != "" {
		conditions = append(conditions, "o.status = "+addArgument(filter.Status))
	}
	searchText := strings.ToLower(strings.TrimSpace(filter.SearchText))
	if searchText != "" {
		searchConditions := []string{"lower(c.name) LIKE " + addArgument("%"+searchText+"%"), "c.phone LIKE " + addArgument("%"+searchText+"%")}
		orderNumberText := strings.TrimLeft(strings.TrimPrefix(searchText, "ord-"), "0")
		orderNumber, parseError := strconv.ParseInt(orderNumberText, 10, 64)
		if parseError == nil {
			searchConditions = append(searchConditions, "o.number = "+addArgument(orderNumber))
		}
		conditions = append(conditions, "("+strings.Join(searchConditions, " OR ")+")")
	}
	whereClause := " WHERE " + strings.Join(conditions, " AND ")

	totalOrders := int64(0)
	countQuery := `SELECT COUNT(*) FROM customer_orders o JOIN customers c ON c.company_id = o.company_id AND c.id = o.customer_id` + whereClause
	countError := querier.QueryRowContext(ctx, countQuery, arguments...).Scan(&totalOrders)
	if countError != nil {
		return nil, 0, fmt.Errorf("failed to count orders: %w", countError)
	}

	query := orderViewQuery + whereClause + ` ORDER BY o.created_at DESC, o.id DESC LIMIT ` + addArgument(limit) + ` OFFSET ` + addArgument(offset)
	orderRows, queryError := querier.QueryContext(ctx, query, arguments...)
	if queryError != nil {
		return nil, 0, fmt.Errorf("failed to list orders: %w", queryError)
	}
	orderViews, scanError := scanOrders(orderRows)
	if scanError != nil {
		return nil, 0, scanError
	}
	return orderViews, totalOrders, nil
}

func (repository *Repository) PricedLines(ctx context.Context, querier database.Querier, companyId uuid.UUID, orderId uuid.UUID) ([]sales.PricedLine, error) {
	query := `
		SELECT product_id, product_name, variant_label, sku, unit, quantity, unit_price, unit_cost, is_wholesale,
		       discount_id, discount_name, discount_amount, line_total
		FROM customer_order_lines
		WHERE company_id = $1 AND order_id = $2
		ORDER BY position
	`

	lineRows, queryError := querier.QueryContext(ctx, query, companyId, orderId)
	if queryError != nil {
		return nil, fmt.Errorf("failed to load order lines: %w", queryError)
	}
	defer lineRows.Close()

	pricedLines := []sales.PricedLine{}
	for lineRows.Next() {
		pricedLine := sales.PricedLine{}
		scanError := lineRows.Scan(
			&pricedLine.Product.Id,
			&pricedLine.Product.Name,
			&pricedLine.Product.VariantLabel,
			&pricedLine.Product.Sku,
			&pricedLine.Product.Unit,
			&pricedLine.Quantity,
			&pricedLine.UnitPrice,
			&pricedLine.Product.CostPrice,
			&pricedLine.IsWholesale,
			&pricedLine.DiscountId,
			&pricedLine.DiscountName,
			&pricedLine.DiscountAmount,
			&pricedLine.LineTotal,
		)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan order line: %w", scanError)
		}
		pricedLine.Product.Price = pricedLine.UnitPrice
		pricedLines = append(pricedLines, pricedLine)
	}
	return pricedLines, lineRows.Err()
}

func (repository *Repository) ListPayments(ctx context.Context, querier database.Querier, companyId uuid.UUID, orderId uuid.UUID) ([]OrderPaymentView, error) {
	query := `
		SELECT p.id, p.kind, p.method, p.amount, p.created_at, u.name
		FROM customer_order_payments p
		JOIN users u ON u.company_id = p.company_id AND u.id = p.created_by
		WHERE p.company_id = $1 AND p.order_id = $2
		ORDER BY p.created_at, p.id
	`

	paymentRows, queryError := querier.QueryContext(ctx, query, companyId, orderId)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list order payments: %w", queryError)
	}
	defer paymentRows.Close()

	paymentViews := []OrderPaymentView{}
	for paymentRows.Next() {
		paymentView := OrderPaymentView{}
		scanError := paymentRows.Scan(&paymentView.Id, &paymentView.Kind, &paymentView.Method, &paymentView.Amount, &paymentView.CreatedAt, &paymentView.CreatedByName)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan order payment: %w", scanError)
		}
		paymentViews = append(paymentViews, paymentView)
	}
	return paymentViews, paymentRows.Err()
}

func scanOrders(orderRows *sql.Rows) ([]OrderView, error) {
	defer orderRows.Close()

	orderViews := []OrderView{}
	for orderRows.Next() {
		orderView := OrderView{Lines: []OrderLineView{}, Payments: []OrderPaymentView{}}
		orderNumber := int64(0)
		scanError := orderRows.Scan(
			&orderView.Id,
			&orderNumber,
			&orderView.Status,
			&orderView.CustomerId,
			&orderView.CustomerName,
			&orderView.CustomerPhone,
			&orderView.ShopId,
			&orderView.ShopName,
			&orderView.DueDate,
			&orderView.Note,
			&orderView.Subtotal,
			&orderView.DiscountTotal,
			&orderView.Total,
			&orderView.TaxTotal,
			&orderView.TaxRateBasisPoints,
			&orderView.DepositTotal,
			&orderView.LineCount,
			&orderView.SaleId,
			&orderView.SaleReceiptNumber,
			&orderView.CreatedByName,
			&orderView.CreatedAt,
			&orderView.ReadyAt,
			&orderView.CollectedAt,
			&orderView.CancelledAt,
			&orderView.CancelledByName,
			&orderView.CancelReason,
		)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan order: %w", scanError)
		}
		orderView.Number = sales.FormatOrderNumber(orderNumber)
		isAwaitingCollection := orderView.Status == StatusOpen || orderView.Status == StatusReady
		if isAwaitingCollection {
			orderView.BalanceDue = orderView.Total - orderView.DepositTotal
		}
		orderViews = append(orderViews, orderView)
	}
	return orderViews, orderRows.Err()
}
