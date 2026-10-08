package sales

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/google/uuid"
)

const saleViewColumns = `
	s.id, s.receipt_number, s.client_ref, s.shop_id, sh.name, s.user_id, u.name, s.subtotal, s.discount_total, s.total,
	s.tax_total, s.tax_rate_basis_points, s.amount_paid, s.change_given, s.currency_code, s.currency_decimals, s.note, s.created_at,
	s.customer_id, c.name, c.phone,
	COALESCE((SELECT p.amount FROM sale_payments p WHERE p.company_id = s.company_id AND p.sale_id = s.id AND p.method = 'credit'), 0),
	(SELECT o.number FROM customer_orders o WHERE o.company_id = s.company_id AND o.sale_id = s.id),
	s.voided_at, s.void_reason, vu.name
`

const saleViewJoins = `
	FROM sales s
	JOIN shops sh ON sh.company_id = s.company_id AND sh.id = s.shop_id
	JOIN users u ON u.company_id = s.company_id AND u.id = s.user_id
	LEFT JOIN customers c ON c.company_id = s.company_id AND c.id = s.customer_id
	LEFT JOIN users vu ON vu.company_id = s.company_id AND vu.id = s.voided_by
`

func (repository *Repository) FindHashByClientRef(ctx context.Context, querier database.Querier, companyId uuid.UUID, clientRef string) (*uuid.UUID, string, error) {
	query := `SELECT id, request_hash FROM sales WHERE company_id = $1 AND client_ref = $2`

	saleId := uuid.UUID{}
	requestHash := ""
	scanError := querier.QueryRowContext(ctx, query, companyId, clientRef).Scan(&saleId, &requestHash)
	if errors.Is(scanError, sql.ErrNoRows) {
		return nil, "", nil
	}
	if scanError != nil {
		return nil, "", fmt.Errorf("failed to look up the sale reference: %w", scanError)
	}

	return &saleId, requestHash, nil
}

func (repository *Repository) InsertSale(ctx context.Context, querier database.Querier, newSale Sale) error {
	query := `
		INSERT INTO sales (id, company_id, shop_id, user_id, client_ref, request_hash, receipt_number, subtotal, discount_total, total,
		                   tax_total, tax_rate_basis_points, amount_paid, change_given, currency_code, currency_decimals, note, created_at, customer_id)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
	`

	_, insertError := querier.ExecContext(ctx, query,
		newSale.Id,
		newSale.CompanyId,
		newSale.ShopId,
		newSale.UserId,
		newSale.ClientRef,
		newSale.RequestHash,
		newSale.ReceiptNumber,
		newSale.Subtotal,
		newSale.DiscountTotal,
		newSale.Total,
		newSale.TaxTotal,
		newSale.TaxRateBasisPoints,
		newSale.AmountPaid,
		newSale.ChangeGiven,
		newSale.CurrencyCode,
		newSale.CurrencyDecimals,
		newSale.Note,
		newSale.CreatedAt,
		newSale.CustomerId,
	)
	if insertError != nil {
		return fmt.Errorf("failed to insert sale: %w", insertError)
	}

	return nil
}

func (repository *Repository) InsertLines(ctx context.Context, querier database.Querier, companyId uuid.UUID, saleId uuid.UUID, pricedLines []PricedLine) error {
	for position, pricedLine := range pricedLines {
		itemId := uuid.Must(uuid.NewV7())
		itemQuery := `
			INSERT INTO sale_items (id, company_id, sale_id, position, product_id, product_name, variant_label, sku, unit, quantity,
			                        unit_price, unit_cost, is_wholesale, addons_unit_total, discount_id, discount_name, discount_amount,
			                        manual_discount_amount, line_total)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19)
		`
		_, itemError := querier.ExecContext(ctx, itemQuery,
			itemId,
			companyId,
			saleId,
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
			pricedLine.AddonsUnitTotal,
			pricedLine.DiscountId,
			pricedLine.DiscountName,
			pricedLine.DiscountAmount,
			pricedLine.ManualDiscount,
			pricedLine.LineTotal,
		)
		if itemError != nil {
			return fmt.Errorf("failed to insert sale item: %w", itemError)
		}

		for _, addon := range pricedLine.Addons {
			addonQuery := `
				INSERT INTO sale_item_addons (company_id, sale_item_id, addon_id, name, unit_price)
				VALUES ($1, $2, $3, $4, $5)
			`
			_, addonError := querier.ExecContext(ctx, addonQuery, companyId, itemId, addon.Id, addon.Name, addon.Price)
			if addonError != nil {
				return fmt.Errorf("failed to insert sale item add-on: %w", addonError)
			}
		}
	}

	return nil
}

func (repository *Repository) InsertPayments(ctx context.Context, querier database.Querier, companyId uuid.UUID, saleId uuid.UUID, payments []Payment) error {
	for _, payment := range payments {
		query := `INSERT INTO sale_payments (company_id, sale_id, method, amount) VALUES ($1, $2, $3, $4)`
		_, insertError := querier.ExecContext(ctx, query, companyId, saleId, payment.Method, payment.Amount)
		if insertError != nil {
			return fmt.Errorf("failed to insert sale payment: %w", insertError)
		}
	}
	return nil
}

func (repository *Repository) MarkVoided(ctx context.Context, querier database.Querier, companyId uuid.UUID, saleId uuid.UUID, voidedBy uuid.UUID, reason string, voidedAt time.Time) (bool, error) {
	query := `
		UPDATE sales
		SET voided_at = $3, voided_by = $4, void_reason = $5
		WHERE company_id = $1 AND id = $2 AND voided_at IS NULL
	`

	updateResult, updateError := querier.ExecContext(ctx, query, companyId, saleId, voidedAt, voidedBy, reason)
	if updateError != nil {
		return false, fmt.Errorf("failed to void the sale: %w", updateError)
	}
	updatedRows, rowsError := updateResult.RowsAffected()
	if rowsError != nil {
		return false, fmt.Errorf("failed to read the voided sale: %w", rowsError)
	}
	return updatedRows == 1, nil
}

func (repository *Repository) FindView(ctx context.Context, querier database.Querier, companyId uuid.UUID, saleId uuid.UUID) (*SaleView, error) {
	query := `SELECT ` + saleViewColumns + saleViewJoins + ` WHERE s.company_id = $1 AND s.id = $2`

	saleView := SaleView{}
	orderNumber := sql.NullInt64{}
	scanError := querier.QueryRowContext(ctx, query, companyId, saleId).Scan(
		&saleView.Id,
		&saleView.ReceiptNumber,
		&saleView.ClientRef,
		&saleView.ShopId,
		&saleView.ShopName,
		&saleView.UserId,
		&saleView.CashierName,
		&saleView.Subtotal,
		&saleView.DiscountTotal,
		&saleView.Total,
		&saleView.TaxTotal,
		&saleView.TaxRateBasisPoints,
		&saleView.AmountPaid,
		&saleView.ChangeGiven,
		&saleView.CurrencyCode,
		&saleView.CurrencyDecimals,
		&saleView.Note,
		&saleView.CreatedAt,
		&saleView.CustomerId,
		&saleView.CustomerName,
		&saleView.CustomerPhone,
		&saleView.CreditAmount,
		&orderNumber,
		&saleView.VoidedAt,
		&saleView.VoidReason,
		&saleView.VoidedByName,
	)
	if errors.Is(scanError, sql.ErrNoRows) {
		return nil, nil
	}
	if scanError != nil {
		return nil, fmt.Errorf("failed to find sale: %w", scanError)
	}
	if orderNumber.Valid {
		formattedOrderNumber := FormatOrderNumber(orderNumber.Int64)
		saleView.OrderNumber = &formattedOrderNumber
	}

	return &saleView, nil
}

func (repository *Repository) ListLines(ctx context.Context, querier database.Querier, companyId uuid.UUID, saleId uuid.UUID) ([]LineView, error) {
	query := `
		SELECT i.id, i.product_id, i.product_name, i.variant_label, i.sku, i.unit, i.quantity, i.unit_price, i.is_wholesale,
		       i.addons_unit_total, i.discount_name, i.discount_amount, i.manual_discount_amount, i.line_total,
		       CAST(COALESCE((SELECT SUM(rl.quantity) FROM sale_refund_lines rl WHERE rl.company_id = i.company_id AND rl.sale_item_id = i.id), 0) AS BIGINT)
		FROM sale_items i
		WHERE i.company_id = $1 AND i.sale_id = $2
		ORDER BY i.position
	`

	lineRows, queryError := querier.QueryContext(ctx, query, companyId, saleId)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list sale items: %w", queryError)
	}
	defer lineRows.Close()

	lineViews := []LineView{}
	lineIndexByItemId := map[uuid.UUID]int{}
	for lineRows.Next() {
		itemId := uuid.UUID{}
		lineView := LineView{Addons: []AddonView{}}
		scanError := lineRows.Scan(
			&itemId,
			&lineView.ProductId,
			&lineView.ProductName,
			&lineView.VariantLabel,
			&lineView.Sku,
			&lineView.Unit,
			&lineView.Quantity,
			&lineView.UnitPrice,
			&lineView.IsWholesale,
			&lineView.AddonsUnitTotal,
			&lineView.DiscountName,
			&lineView.DiscountAmount,
			&lineView.ManualDiscount,
			&lineView.LineTotal,
			&lineView.RefundedQuantity,
		)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan sale item: %w", scanError)
		}
		scannedItemId := itemId
		lineView.ItemId = &scannedItemId
		lineIndexByItemId[itemId] = len(lineViews)
		lineViews = append(lineViews, lineView)
	}
	if rowsError := lineRows.Err(); rowsError != nil {
		return nil, rowsError
	}

	addonQuery := `
		SELECT a.sale_item_id, a.addon_id, a.name, a.unit_price
		FROM sale_item_addons a
		JOIN sale_items i ON i.company_id = a.company_id AND i.id = a.sale_item_id
		WHERE a.company_id = $1 AND i.sale_id = $2
		ORDER BY a.name
	`
	addonRows, addonQueryError := querier.QueryContext(ctx, addonQuery, companyId, saleId)
	if addonQueryError != nil {
		return nil, fmt.Errorf("failed to list sale add-ons: %w", addonQueryError)
	}
	defer addonRows.Close()

	for addonRows.Next() {
		itemId := uuid.UUID{}
		addonView := AddonView{}
		scanError := addonRows.Scan(&itemId, &addonView.AddonId, &addonView.Name, &addonView.UnitPrice)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan sale add-on: %w", scanError)
		}
		lineIndex := lineIndexByItemId[itemId]
		lineViews[lineIndex].Addons = append(lineViews[lineIndex].Addons, addonView)
	}

	return lineViews, addonRows.Err()
}

func (repository *Repository) ListPayments(ctx context.Context, querier database.Querier, companyId uuid.UUID, saleId uuid.UUID) ([]PaymentView, error) {
	query := `SELECT method, amount FROM sale_payments WHERE company_id = $1 AND sale_id = $2 ORDER BY method`

	paymentRows, queryError := querier.QueryContext(ctx, query, companyId, saleId)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list sale payments: %w", queryError)
	}
	defer paymentRows.Close()

	paymentViews := []PaymentView{}
	for paymentRows.Next() {
		paymentView := PaymentView{}
		scanError := paymentRows.Scan(&paymentView.Method, &paymentView.Amount)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan sale payment: %w", scanError)
		}
		paymentViews = append(paymentViews, paymentView)
	}

	return paymentViews, paymentRows.Err()
}
