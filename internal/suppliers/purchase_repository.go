package suppliers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/media"
	"github.com/google/uuid"
)

const purchaseViewColumns = `
	pu.id, pu.purchase_number, pu.shop_id, sh.name, pu.supplier_id, su.name, pu.supplier_invoice_number, pu.invoice_date,
	pu.received_at, pu.status, pu.prices_include_vat, pu.subtotal, pu.vat_total, pu.total, pu.note, pu.attachment_key,
	pu.purchase_order_id, po.order_number, pu.client_ref, cu.name, pu.created_at, xu.name, pu.cancelled_at, pu.cancel_reason,
	(SELECT COUNT(*) FROM purchase_lines pl WHERE pl.company_id = pu.company_id AND pl.purchase_id = pu.id)
`

const purchaseViewJoins = `
	FROM purchases pu
	JOIN shops sh ON sh.company_id = pu.company_id AND sh.id = pu.shop_id
	LEFT JOIN suppliers su ON su.company_id = pu.company_id AND su.id = pu.supplier_id
	LEFT JOIN purchase_orders po ON po.company_id = pu.company_id AND po.id = pu.purchase_order_id
	LEFT JOIN users cu ON cu.company_id = pu.company_id AND cu.id = pu.created_by
	LEFT JOIN users xu ON xu.company_id = pu.company_id AND xu.id = pu.cancelled_by
`

const purchaseFilter = `
	pu.company_id = $1
	AND ($2 = '' OR pu.supplier_id = $3)
	AND ($4 = '' OR pu.purchase_order_id = $5)
	AND ($6 = '' OR pu.status = $6)
`

func documentFilterArguments(companyId uuid.UUID, filter DocumentFilter) []any {
	supplierText := ""
	supplierArgument := any(nil)
	if filter.SupplierId != nil {
		supplierText = filter.SupplierId.String()
		supplierArgument = *filter.SupplierId
	}
	orderText := ""
	orderArgument := any(nil)
	if filter.PurchaseOrderId != nil {
		orderText = filter.PurchaseOrderId.String()
		orderArgument = *filter.PurchaseOrderId
	}
	return []any{companyId, supplierText, supplierArgument, orderText, orderArgument, filter.Status}
}

func (repository *Repository) FindPurchaseIdByClientRef(ctx context.Context, querier database.Querier, companyId uuid.UUID, clientRef string) (*uuid.UUID, error) {
	query := `SELECT id FROM purchases WHERE company_id = $1 AND client_ref = $2`

	purchaseId := uuid.UUID{}
	scanError := querier.QueryRowContext(ctx, query, companyId, clientRef).Scan(&purchaseId)
	if errors.Is(scanError, sql.ErrNoRows) {
		return nil, nil
	}
	if scanError != nil {
		return nil, fmt.Errorf("failed to look up the stock arrival reference: %w", scanError)
	}

	return &purchaseId, nil
}

func (repository *Repository) InsertPurchase(ctx context.Context, querier database.Querier, newPurchase Purchase, pricedLines []PricedLine) error {
	purchaseQuery := `
		INSERT INTO purchases (id, company_id, purchase_number, shop_id, supplier_id, supplier_invoice_number, invoice_date, received_at,
		                       status, prices_include_vat, subtotal, vat_total, total, note, purchase_order_id, client_ref, created_by, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, 'received', $9, $10, $11, $12, $13, $14, $15, $16, $17)
	`

	_, purchaseError := querier.ExecContext(ctx, purchaseQuery,
		newPurchase.Id,
		newPurchase.CompanyId,
		newPurchase.PurchaseNumber,
		newPurchase.ShopId,
		newPurchase.SupplierId,
		newPurchase.SupplierInvoiceNumber,
		newPurchase.InvoiceDate,
		newPurchase.ReceivedAt,
		newPurchase.PricesIncludeVat,
		newPurchase.Subtotal,
		newPurchase.VatTotal,
		newPurchase.Total,
		newPurchase.Note,
		newPurchase.PurchaseOrderId,
		newPurchase.ClientRef,
		newPurchase.CreatedBy,
		newPurchase.CreatedAt,
	)
	if purchaseError != nil {
		return fmt.Errorf("failed to insert stock arrival: %w", purchaseError)
	}

	for position, pricedLine := range pricedLines {
		lineQuery := `
			INSERT INTO purchase_lines (id, company_id, purchase_id, position, product_id, quantity, unit_cost, vat_amount, line_total)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		`
		_, lineError := querier.ExecContext(ctx, lineQuery,
			uuid.Must(uuid.NewV7()),
			newPurchase.CompanyId,
			newPurchase.Id,
			position,
			pricedLine.ProductId,
			pricedLine.Quantity,
			pricedLine.UnitCost,
			pricedLine.VatAmount,
			pricedLine.LineTotal,
		)
		if lineError != nil {
			return fmt.Errorf("failed to insert stock arrival line: %w", lineError)
		}
	}

	return nil
}

func scanPurchase(row rowScanner) (PurchaseView, error) {
	purchaseView := PurchaseView{}
	var attachmentKey *string
	scanError := row.Scan(
		&purchaseView.Id,
		&purchaseView.PurchaseNumber,
		&purchaseView.ShopId,
		&purchaseView.ShopName,
		&purchaseView.SupplierId,
		&purchaseView.SupplierName,
		&purchaseView.SupplierInvoiceNumber,
		&purchaseView.InvoiceDate,
		&purchaseView.ReceivedAt,
		&purchaseView.Status,
		&purchaseView.PricesIncludeVat,
		&purchaseView.Subtotal,
		&purchaseView.VatTotal,
		&purchaseView.Total,
		&purchaseView.Note,
		&attachmentKey,
		&purchaseView.PurchaseOrderId,
		&purchaseView.PurchaseOrderNumber,
		&purchaseView.ClientRef,
		&purchaseView.CreatedByName,
		&purchaseView.CreatedAt,
		&purchaseView.CancelledByName,
		&purchaseView.CancelledAt,
		&purchaseView.CancelReason,
		&purchaseView.LineCount,
	)
	purchaseView.AttachmentUrl = media.PublicUrl(attachmentKey)
	return purchaseView, scanError
}

func (repository *Repository) FindPurchase(ctx context.Context, querier database.Querier, companyId uuid.UUID, purchaseId uuid.UUID) (*PurchaseView, error) {
	query := `SELECT ` + purchaseViewColumns + purchaseViewJoins + ` WHERE pu.company_id = $1 AND pu.id = $2`

	purchaseView, scanError := scanPurchase(querier.QueryRowContext(ctx, query, companyId, purchaseId))
	if errors.Is(scanError, sql.ErrNoRows) {
		return nil, nil
	}
	if scanError != nil {
		return nil, fmt.Errorf("failed to find stock arrival: %w", scanError)
	}

	return &purchaseView, nil
}

func (repository *Repository) CountPurchases(ctx context.Context, querier database.Querier, companyId uuid.UUID, filter DocumentFilter) (int64, error) {
	query := `SELECT COUNT(*) FROM purchases pu WHERE ` + purchaseFilter

	purchaseCount := int64(0)
	scanError := querier.QueryRowContext(ctx, query, documentFilterArguments(companyId, filter)...).Scan(&purchaseCount)
	if scanError != nil {
		return 0, fmt.Errorf("failed to count stock arrivals: %w", scanError)
	}

	return purchaseCount, nil
}

func (repository *Repository) ListPurchases(ctx context.Context, querier database.Querier, companyId uuid.UUID, filter DocumentFilter, limit int, offset int) ([]PurchaseView, error) {
	query := `
		SELECT ` + purchaseViewColumns + purchaseViewJoins + `
		WHERE ` + purchaseFilter + `
		ORDER BY pu.received_at DESC, pu.purchase_number DESC
		LIMIT $7 OFFSET $8
	`

	queryArguments := append(documentFilterArguments(companyId, filter), limit, offset)
	purchaseRows, queryError := querier.QueryContext(ctx, query, queryArguments...)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list stock arrivals: %w", queryError)
	}
	defer purchaseRows.Close()

	purchaseViews := []PurchaseView{}
	for purchaseRows.Next() {
		purchaseView, scanError := scanPurchase(purchaseRows)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan stock arrival: %w", scanError)
		}
		purchaseViews = append(purchaseViews, purchaseView)
	}

	return purchaseViews, purchaseRows.Err()
}

func (repository *Repository) ListPurchaseLines(ctx context.Context, querier database.Querier, companyId uuid.UUID, purchaseId uuid.UUID) ([]PurchaseLineView, error) {
	query := `
		SELECT pl.product_id, p.name, p.variant_label, p.sku, p.unit, pl.quantity, pl.unit_cost, pl.vat_amount, pl.line_total
		FROM purchase_lines pl
		JOIN products p ON p.company_id = pl.company_id AND p.id = pl.product_id
		WHERE pl.company_id = $1 AND pl.purchase_id = $2
		ORDER BY pl.position
	`

	lineRows, queryError := querier.QueryContext(ctx, query, companyId, purchaseId)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list stock arrival lines: %w", queryError)
	}
	defer lineRows.Close()

	lineViews := []PurchaseLineView{}
	for lineRows.Next() {
		lineView := PurchaseLineView{}
		scanError := lineRows.Scan(&lineView.ProductId, &lineView.ProductName, &lineView.VariantLabel, &lineView.Sku, &lineView.Unit,
			&lineView.Quantity, &lineView.UnitCost, &lineView.VatAmount, &lineView.LineTotal)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan stock arrival line: %w", scanError)
		}
		lineViews = append(lineViews, lineView)
	}

	return lineViews, lineRows.Err()
}

func (repository *Repository) CancelPurchase(ctx context.Context, querier database.Querier, companyId uuid.UUID, purchaseId uuid.UUID, userId uuid.UUID, reason string, cancelledAt time.Time) error {
	query := `
		UPDATE purchases
		SET status = 'cancelled', cancelled_by = $3, cancelled_at = $4, cancel_reason = $5
		WHERE company_id = $1 AND id = $2 AND status = 'received'
	`

	_, updateError := querier.ExecContext(ctx, query, companyId, purchaseId, userId, cancelledAt, reason)
	if updateError != nil {
		return fmt.Errorf("failed to cancel stock arrival: %w", updateError)
	}

	return nil
}

func (repository *Repository) SetAttachmentKey(ctx context.Context, querier database.Querier, companyId uuid.UUID, purchaseId uuid.UUID, attachmentKey string) error {
	query := `UPDATE purchases SET attachment_key = $3 WHERE company_id = $1 AND id = $2`

	_, updateError := querier.ExecContext(ctx, query, companyId, purchaseId, attachmentKey)
	if updateError != nil {
		return fmt.Errorf("failed to attach the invoice photo: %w", updateError)
	}

	return nil
}

func (repository *Repository) FindLastCost(ctx context.Context, querier database.Querier, companyId uuid.UUID, productId uuid.UUID, supplierId *uuid.UUID) (LastCostView, error) {
	query := `
		SELECT pl.unit_cost, pu.received_at
		FROM purchase_lines pl
		JOIN purchases pu ON pu.company_id = pl.company_id AND pu.id = pl.purchase_id
		WHERE pl.company_id = $1 AND pl.product_id = $2 AND pu.status = 'received'
		ORDER BY CASE WHEN pu.supplier_id = $3 THEN 0 ELSE 1 END, pu.received_at DESC, pu.purchase_number DESC
		LIMIT 1
	`

	lastCostView := LastCostView{ProductId: productId}
	scanError := querier.QueryRowContext(ctx, query, companyId, productId, supplierId).Scan(&lastCostView.UnitCost, &lastCostView.ReceivedAt)
	if errors.Is(scanError, sql.ErrNoRows) {
		return lastCostView, nil
	}
	if scanError != nil {
		return LastCostView{}, fmt.Errorf("failed to find the last cost: %w", scanError)
	}

	return lastCostView, nil
}

const paymentViewColumns = `
	sp.id, sp.payment_number, sp.supplier_id, su.name, sp.purchase_id, pu.purchase_number, sp.shop_id, sh.name, sp.amount, sp.method,
	sp.reference, sp.paid_at, cu.name, sp.created_at, vu.name, sp.voided_at, sp.void_reason
`

const paymentViewJoins = `
	FROM supplier_payments sp
	JOIN shops sh ON sh.company_id = sp.company_id AND sh.id = sp.shop_id
	LEFT JOIN suppliers su ON su.company_id = sp.company_id AND su.id = sp.supplier_id
	LEFT JOIN purchases pu ON pu.company_id = sp.company_id AND pu.id = sp.purchase_id
	LEFT JOIN users cu ON cu.company_id = sp.company_id AND cu.id = sp.created_by
	LEFT JOIN users vu ON vu.company_id = sp.company_id AND vu.id = sp.voided_by
`

func (repository *Repository) InsertPayment(ctx context.Context, querier database.Querier, newPayment Payment) error {
	query := `
		INSERT INTO supplier_payments (id, company_id, payment_number, supplier_id, purchase_id, shop_id, amount, method, reference, paid_at, created_by, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`

	_, insertError := querier.ExecContext(ctx, query,
		newPayment.Id,
		newPayment.CompanyId,
		newPayment.PaymentNumber,
		newPayment.SupplierId,
		newPayment.PurchaseId,
		newPayment.ShopId,
		newPayment.Amount,
		newPayment.Method,
		newPayment.Reference,
		newPayment.PaidAt,
		newPayment.CreatedBy,
		newPayment.CreatedAt,
	)
	if insertError != nil {
		return fmt.Errorf("failed to insert supplier payment: %w", insertError)
	}

	return nil
}

func scanPayment(row rowScanner) (PaymentView, error) {
	paymentView := PaymentView{}
	scanError := row.Scan(
		&paymentView.Id,
		&paymentView.PaymentNumber,
		&paymentView.SupplierId,
		&paymentView.SupplierName,
		&paymentView.PurchaseId,
		&paymentView.PurchaseNumber,
		&paymentView.ShopId,
		&paymentView.ShopName,
		&paymentView.Amount,
		&paymentView.Method,
		&paymentView.Reference,
		&paymentView.PaidAt,
		&paymentView.CreatedByName,
		&paymentView.CreatedAt,
		&paymentView.VoidedByName,
		&paymentView.VoidedAt,
		&paymentView.VoidReason,
	)
	paymentView.IsVoided = paymentView.VoidedAt != nil
	return paymentView, scanError
}

func (repository *Repository) queryPayments(ctx context.Context, querier database.Querier, query string, queryArguments ...any) ([]PaymentView, error) {
	paymentRows, queryError := querier.QueryContext(ctx, query, queryArguments...)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list supplier payments: %w", queryError)
	}
	defer paymentRows.Close()

	paymentViews := []PaymentView{}
	for paymentRows.Next() {
		paymentView, scanError := scanPayment(paymentRows)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan supplier payment: %w", scanError)
		}
		paymentViews = append(paymentViews, paymentView)
	}

	return paymentViews, paymentRows.Err()
}

func (repository *Repository) FindPayment(ctx context.Context, querier database.Querier, companyId uuid.UUID, paymentId uuid.UUID) (*PaymentView, error) {
	query := `SELECT ` + paymentViewColumns + paymentViewJoins + ` WHERE sp.company_id = $1 AND sp.id = $2`

	paymentView, scanError := scanPayment(querier.QueryRowContext(ctx, query, companyId, paymentId))
	if errors.Is(scanError, sql.ErrNoRows) {
		return nil, nil
	}
	if scanError != nil {
		return nil, fmt.Errorf("failed to find supplier payment: %w", scanError)
	}

	return &paymentView, nil
}

func (repository *Repository) CountPayments(ctx context.Context, querier database.Querier, companyId uuid.UUID, supplierId *uuid.UUID) (int64, error) {
	query := `SELECT COUNT(*) FROM supplier_payments WHERE company_id = $1 AND supplier_id IS NOT NULL AND ($2 = '' OR supplier_id = $3)`

	supplierText, supplierArgument := optionalIdArguments(supplierId)
	paymentCount := int64(0)
	scanError := querier.QueryRowContext(ctx, query, companyId, supplierText, supplierArgument).Scan(&paymentCount)
	if scanError != nil {
		return 0, fmt.Errorf("failed to count supplier payments: %w", scanError)
	}

	return paymentCount, nil
}

func (repository *Repository) ListPayments(ctx context.Context, querier database.Querier, companyId uuid.UUID, supplierId *uuid.UUID, limit int, offset int) ([]PaymentView, error) {
	query := `
		SELECT ` + paymentViewColumns + paymentViewJoins + `
		WHERE sp.company_id = $1 AND sp.supplier_id IS NOT NULL AND ($2 = '' OR sp.supplier_id = $3)
		ORDER BY sp.paid_at DESC, sp.payment_number DESC
		LIMIT $4 OFFSET $5
	`

	supplierText, supplierArgument := optionalIdArguments(supplierId)
	return repository.queryPayments(ctx, querier, query, companyId, supplierText, supplierArgument, limit, offset)
}

func (repository *Repository) ListPurchasePayments(ctx context.Context, querier database.Querier, companyId uuid.UUID, purchaseId uuid.UUID) ([]PaymentView, error) {
	query := `
		SELECT ` + paymentViewColumns + paymentViewJoins + `
		WHERE sp.company_id = $1 AND sp.purchase_id = $2
		ORDER BY sp.paid_at, sp.payment_number
	`
	return repository.queryPayments(ctx, querier, query, companyId, purchaseId)
}

func (repository *Repository) VoidPayment(ctx context.Context, querier database.Querier, companyId uuid.UUID, paymentId uuid.UUID, userId uuid.UUID, reason string, voidedAt time.Time) error {
	query := `
		UPDATE supplier_payments
		SET voided_by = $3, voided_at = $4, void_reason = $5
		WHERE company_id = $1 AND id = $2 AND voided_at IS NULL
	`

	_, updateError := querier.ExecContext(ctx, query, companyId, paymentId, userId, voidedAt, reason)
	if updateError != nil {
		return fmt.Errorf("failed to void supplier payment: %w", updateError)
	}

	return nil
}

func optionalIdArguments(optionalId *uuid.UUID) (string, any) {
	if optionalId == nil {
		return "", nil
	}
	return optionalId.String(), *optionalId
}

const returnViewColumns = `
	sr.id, sr.return_number, sr.supplier_id, su.name, sr.shop_id, sh.name, sr.total, sr.note, sr.returned_at, cu.name, sr.created_at,
	(SELECT COUNT(*) FROM supplier_return_lines rl WHERE rl.company_id = sr.company_id AND rl.return_id = sr.id)
`

const returnViewJoins = `
	FROM supplier_returns sr
	JOIN shops sh ON sh.company_id = sr.company_id AND sh.id = sr.shop_id
	JOIN suppliers su ON su.company_id = sr.company_id AND su.id = sr.supplier_id
	LEFT JOIN users cu ON cu.company_id = sr.company_id AND cu.id = sr.created_by
`

func (repository *Repository) InsertReturn(ctx context.Context, querier database.Querier, newReturn SupplierReturn, returnLines []ReturnLine) error {
	returnQuery := `
		INSERT INTO supplier_returns (id, company_id, return_number, supplier_id, shop_id, total, note, returned_at, created_by, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`

	_, returnError := querier.ExecContext(ctx, returnQuery,
		newReturn.Id,
		newReturn.CompanyId,
		newReturn.ReturnNumber,
		newReturn.SupplierId,
		newReturn.ShopId,
		newReturn.Total,
		newReturn.Note,
		newReturn.ReturnedAt,
		newReturn.CreatedBy,
		newReturn.CreatedAt,
	)
	if returnError != nil {
		return fmt.Errorf("failed to insert supplier return: %w", returnError)
	}

	for position, returnLine := range returnLines {
		lineQuery := `
			INSERT INTO supplier_return_lines (id, company_id, return_id, position, product_id, quantity, unit_cost, line_total)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
		`
		_, lineError := querier.ExecContext(ctx, lineQuery,
			uuid.Must(uuid.NewV7()),
			newReturn.CompanyId,
			newReturn.Id,
			position,
			returnLine.ProductId,
			returnLine.Quantity,
			returnLine.UnitCost,
			int64(returnLine.Quantity)*returnLine.UnitCost,
		)
		if lineError != nil {
			return fmt.Errorf("failed to insert supplier return line: %w", lineError)
		}
	}

	return nil
}

func scanReturn(row rowScanner) (ReturnView, error) {
	returnView := ReturnView{}
	scanError := row.Scan(
		&returnView.Id,
		&returnView.ReturnNumber,
		&returnView.SupplierId,
		&returnView.SupplierName,
		&returnView.ShopId,
		&returnView.ShopName,
		&returnView.Total,
		&returnView.Note,
		&returnView.ReturnedAt,
		&returnView.CreatedByName,
		&returnView.CreatedAt,
		&returnView.LineCount,
	)
	return returnView, scanError
}

func (repository *Repository) FindReturn(ctx context.Context, querier database.Querier, companyId uuid.UUID, returnId uuid.UUID) (*ReturnView, error) {
	query := `SELECT ` + returnViewColumns + returnViewJoins + ` WHERE sr.company_id = $1 AND sr.id = $2`

	returnView, scanError := scanReturn(querier.QueryRowContext(ctx, query, companyId, returnId))
	if errors.Is(scanError, sql.ErrNoRows) {
		return nil, nil
	}
	if scanError != nil {
		return nil, fmt.Errorf("failed to find supplier return: %w", scanError)
	}

	return &returnView, nil
}

func (repository *Repository) CountReturns(ctx context.Context, querier database.Querier, companyId uuid.UUID, supplierId *uuid.UUID) (int64, error) {
	query := `SELECT COUNT(*) FROM supplier_returns WHERE company_id = $1 AND ($2 = '' OR supplier_id = $3)`

	supplierText, supplierArgument := optionalIdArguments(supplierId)
	returnCount := int64(0)
	scanError := querier.QueryRowContext(ctx, query, companyId, supplierText, supplierArgument).Scan(&returnCount)
	if scanError != nil {
		return 0, fmt.Errorf("failed to count supplier returns: %w", scanError)
	}

	return returnCount, nil
}

func (repository *Repository) ListReturns(ctx context.Context, querier database.Querier, companyId uuid.UUID, supplierId *uuid.UUID, limit int, offset int) ([]ReturnView, error) {
	query := `
		SELECT ` + returnViewColumns + returnViewJoins + `
		WHERE sr.company_id = $1 AND ($2 = '' OR sr.supplier_id = $3)
		ORDER BY sr.returned_at DESC, sr.return_number DESC
		LIMIT $4 OFFSET $5
	`

	supplierText, supplierArgument := optionalIdArguments(supplierId)
	returnRows, queryError := querier.QueryContext(ctx, query, companyId, supplierText, supplierArgument, limit, offset)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list supplier returns: %w", queryError)
	}
	defer returnRows.Close()

	returnViews := []ReturnView{}
	for returnRows.Next() {
		returnView, scanError := scanReturn(returnRows)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan supplier return: %w", scanError)
		}
		returnViews = append(returnViews, returnView)
	}

	return returnViews, returnRows.Err()
}

func (repository *Repository) ListReturnLines(ctx context.Context, querier database.Querier, companyId uuid.UUID, returnId uuid.UUID) ([]ReturnLineView, error) {
	query := `
		SELECT rl.product_id, p.name, p.variant_label, p.sku, p.unit, rl.quantity, rl.unit_cost, rl.line_total
		FROM supplier_return_lines rl
		JOIN products p ON p.company_id = rl.company_id AND p.id = rl.product_id
		WHERE rl.company_id = $1 AND rl.return_id = $2
		ORDER BY rl.position
	`

	lineRows, queryError := querier.QueryContext(ctx, query, companyId, returnId)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list supplier return lines: %w", queryError)
	}
	defer lineRows.Close()

	lineViews := []ReturnLineView{}
	for lineRows.Next() {
		lineView := ReturnLineView{}
		scanError := lineRows.Scan(&lineView.ProductId, &lineView.ProductName, &lineView.VariantLabel, &lineView.Sku, &lineView.Unit,
			&lineView.Quantity, &lineView.UnitCost, &lineView.LineTotal)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan supplier return line: %w", scanError)
		}
		lineViews = append(lineViews, lineView)
	}

	return lineViews, lineRows.Err()
}

func (repository *Repository) CountActiveProducts(ctx context.Context, querier database.Querier, companyId uuid.UUID, productIds []uuid.UUID) (int, error) {
	query := `SELECT COUNT(*) FROM products WHERE company_id = $1 AND is_active AND id IN (` + database.Placeholders(2, len(productIds)) + `)`

	queryArguments := append([]any{companyId}, database.ToArguments(productIds)...)
	matchCount := 0
	scanError := querier.QueryRowContext(ctx, query, queryArguments...).Scan(&matchCount)
	if scanError != nil {
		return 0, fmt.Errorf("failed to check products: %w", scanError)
	}

	return matchCount, nil
}
