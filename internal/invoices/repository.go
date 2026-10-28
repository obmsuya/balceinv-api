package invoices

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/shopscope"
	"github.com/google/uuid"
)

const paidTotalSql = `CAST(COALESCE((
	SELECT SUM(ip.amount) FROM invoice_payments ip
	LEFT JOIN customer_payments cp ON cp.company_id = ip.company_id AND cp.id = ip.customer_payment_id
	LEFT JOIN supplier_receipts sr ON sr.company_id = ip.company_id AND sr.id = ip.supplier_receipt_id
	WHERE ip.company_id = i.company_id AND ip.invoice_id = i.id AND ip.voided_at IS NULL AND cp.voided_at IS NULL AND sr.voided_at IS NULL
), 0) AS BIGINT)`

const invoiceSelect = `
	SELECT i.id, i.kind, i.number, i.status, i.customer_id, i.supplier_id,
	       COALESCE(c.name, s.name, ''), COALESCE(c.phone, s.phone), COALESCE(c.email, s.email), COALESCE(c.address, s.address), COALESCE(c.tin, s.tin),
	       i.shop_id, sh.name, i.issue_date, i.due_date, i.reference, i.notes, i.terms,
	       i.subtotal, i.discount_total, i.tax_total, i.tax_rate_basis_points, i.total, ` + paidTotalSql + `,
	       (SELECT COUNT(*) FROM invoice_lines il WHERE il.company_id = i.company_id AND il.invoice_id = i.id),
	       i.source_invoice_id, i.delivered_at, delivered.name, i.cancelled_at, cancelled.name, i.cancel_reason,
	       creator.name, i.created_at, i.updated_at
	FROM invoices i
	JOIN shops sh ON sh.company_id = i.company_id AND sh.id = i.shop_id
	JOIN users creator ON creator.company_id = i.company_id AND creator.id = i.created_by
	LEFT JOIN customers c ON c.company_id = i.company_id AND c.id = i.customer_id
	LEFT JOIN suppliers s ON s.company_id = i.company_id AND s.id = i.supplier_id
	LEFT JOIN users delivered ON delivered.company_id = i.company_id AND delivered.id = i.delivered_by
	LEFT JOIN users cancelled ON cancelled.company_id = i.company_id AND cancelled.id = i.cancelled_by
`

type Repository struct{}

func NewRepository() *Repository {
	return &Repository{}
}

func (repository *Repository) TakeNumber(ctx context.Context, querier database.Querier, companyId uuid.UUID, prefix string, year int) (int64, error) {
	query := `
		INSERT INTO invoice_counters (company_id, prefix, year, last_number) VALUES ($1, $2, $3, 1)
		ON CONFLICT (company_id, prefix, year) DO UPDATE SET last_number = invoice_counters.last_number + 1
		RETURNING last_number
	`
	takenNumber := int64(0)
	scanError := querier.QueryRowContext(ctx, query, companyId, prefix, year).Scan(&takenNumber)
	if scanError != nil {
		return 0, fmt.Errorf("failed to take an invoice number: %w", scanError)
	}
	return takenNumber, nil
}

func (repository *Repository) Insert(ctx context.Context, querier database.Querier, invoice Invoice, lines []Line) error {
	query := `
		INSERT INTO invoices (id, company_id, shop_id, kind, number, status, customer_id, supplier_id, issue_date, due_date,
		                      reference, notes, terms, subtotal, discount_total, total, tax_total, tax_rate_basis_points,
		                      source_invoice_id, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $21)
	`
	_, insertError := querier.ExecContext(ctx, query,
		invoice.Id, invoice.CompanyId, invoice.ShopId, invoice.Kind, invoice.Number, invoice.Status, invoice.CustomerId, invoice.SupplierId,
		invoice.IssueDate, invoice.DueDate, invoice.Reference, invoice.Notes, invoice.Terms, invoice.Subtotal, invoice.DiscountTotal,
		invoice.Total, invoice.TaxTotal, invoice.TaxRateBasisPoints, invoice.SourceInvoiceId, invoice.CreatedBy, invoice.CreatedAt,
	)
	if insertError != nil {
		return fmt.Errorf("failed to save the invoice: %w", insertError)
	}
	return repository.insertLines(ctx, querier, invoice.CompanyId, invoice.Id, lines)
}

func (repository *Repository) UpdateDraft(ctx context.Context, querier database.Querier, invoice Invoice, lines []Line) error {
	query := `
		UPDATE invoices
		SET customer_id = $3, supplier_id = $4, issue_date = $5, due_date = $6, reference = $7, notes = $8, terms = $9,
		    subtotal = $10, discount_total = $11, total = $12, tax_total = $13, tax_rate_basis_points = $14, updated_at = $15
		WHERE company_id = $1 AND id = $2 AND status = 'draft'
	`
	_, updateError := querier.ExecContext(ctx, query,
		invoice.CompanyId, invoice.Id, invoice.CustomerId, invoice.SupplierId, invoice.IssueDate, invoice.DueDate, invoice.Reference,
		invoice.Notes, invoice.Terms, invoice.Subtotal, invoice.DiscountTotal, invoice.Total, invoice.TaxTotal, invoice.TaxRateBasisPoints,
		invoice.UpdatedAt,
	)
	if updateError != nil {
		return fmt.Errorf("failed to update the draft: %w", updateError)
	}
	_, deleteError := querier.ExecContext(ctx, `DELETE FROM invoice_lines WHERE company_id = $1 AND invoice_id = $2`, invoice.CompanyId, invoice.Id)
	if deleteError != nil {
		return fmt.Errorf("failed to clear the draft lines: %w", deleteError)
	}
	return repository.insertLines(ctx, querier, invoice.CompanyId, invoice.Id, lines)
}

func (repository *Repository) insertLines(ctx context.Context, querier database.Querier, companyId uuid.UUID, invoiceId uuid.UUID, lines []Line) error {
	query := `
		INSERT INTO invoice_lines (id, company_id, invoice_id, position, product_id, description, unit, quantity, unit_price, discount_amount, line_total)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`
	for _, line := range lines {
		_, insertError := querier.ExecContext(ctx, query, line.Id, companyId, invoiceId, line.Position, line.ProductId, line.Description,
			line.Unit, line.Quantity, line.UnitPrice, line.DiscountAmount, line.LineTotal)
		if insertError != nil {
			return fmt.Errorf("failed to save an invoice line: %w", insertError)
		}
	}
	return nil
}

func (repository *Repository) Lock(ctx context.Context, querier database.Querier, companyId uuid.UUID, invoiceId uuid.UUID) (bool, error) {
	lockResult, lockError := querier.ExecContext(ctx, `UPDATE invoices SET status = status WHERE company_id = $1 AND id = $2`, companyId, invoiceId)
	if lockError != nil {
		return false, fmt.Errorf("failed to lock the invoice: %w", lockError)
	}
	lockedCount, countError := lockResult.RowsAffected()
	if countError != nil {
		return false, fmt.Errorf("failed to count locked invoices: %w", countError)
	}
	return lockedCount == 1, nil
}

func (repository *Repository) Find(ctx context.Context, querier database.Querier, companyId uuid.UUID, invoiceId uuid.UUID) (*InvoiceView, error) {
	invoiceRows, queryError := querier.QueryContext(ctx, invoiceSelect+` WHERE i.company_id = $1 AND i.id = $2`, companyId, invoiceId)
	foundInvoices, scanError := scanInvoices(invoiceRows, queryError)
	if scanError != nil {
		return nil, scanError
	}
	if len(foundInvoices) == 0 {
		return nil, nil
	}
	return &foundInvoices[0], nil
}

func (repository *Repository) List(ctx context.Context, querier database.Querier, companyId uuid.UUID, scope shopscope.Scope, filter Filter, today string, limit int, offset int) ([]InvoiceView, int64, error) {
	arguments := []any{companyId}
	addArgument := func(value any) string {
		arguments = append(arguments, value)
		return fmt.Sprintf("$%d", len(arguments))
	}
	conditions := []string{"i.company_id = $1" + scope.Condition("i.shop_id", &arguments)}
	if len(filter.Kinds) > 0 {
		kindPlaceholders := []string{}
		for _, kind := range filter.Kinds {
			kindPlaceholders = append(kindPlaceholders, addArgument(kind))
		}
		conditions = append(conditions, "i.kind IN ("+strings.Join(kindPlaceholders, ", ")+")")
	}
	if filter.Status != "" {
		conditions = append(conditions, "i.status = "+addArgument(filter.Status))
	}
	if filter.CustomerId != nil {
		conditions = append(conditions, "i.customer_id = "+addArgument(*filter.CustomerId))
	}
	if filter.SupplierId != nil {
		conditions = append(conditions, "i.supplier_id = "+addArgument(*filter.SupplierId))
	}
	if filter.SearchText != "" {
		pattern := "%" + strings.ToLower(filter.SearchText) + "%"
		patternPlaceholder := addArgument(pattern)
		conditions = append(conditions, "(lower(i.number) LIKE "+patternPlaceholder+" OR lower(COALESCE(c.name, s.name, '')) LIKE "+patternPlaceholder+" OR lower(COALESCE(i.reference, '')) LIKE "+patternPlaceholder+")")
	}
	isOpenBill := "i.kind IN ('invoice', 'supplier_invoice') AND i.status = 'issued'"
	switch filter.PaymentStatus {
	case PaymentUnpaid:
		conditions = append(conditions, isOpenBill+" AND "+paidTotalSql+" = 0")
	case PaymentPartPaid:
		conditions = append(conditions, isOpenBill+" AND "+paidTotalSql+" > 0 AND "+paidTotalSql+" < i.total")
	case PaymentPaid:
		conditions = append(conditions, isOpenBill+" AND "+paidTotalSql+" >= i.total")
	case "overdue":
		conditions = append(conditions, isOpenBill+" AND "+paidTotalSql+" < i.total AND i.due_date < "+addArgument(today))
	}
	where := " WHERE " + strings.Join(conditions, " AND ")

	countQuery := `SELECT COUNT(*) FROM invoices i LEFT JOIN customers c ON c.company_id = i.company_id AND c.id = i.customer_id LEFT JOIN suppliers s ON s.company_id = i.company_id AND s.id = i.supplier_id` + where
	totalInvoices := int64(0)
	countError := querier.QueryRowContext(ctx, countQuery, arguments...).Scan(&totalInvoices)
	if countError != nil {
		return nil, 0, fmt.Errorf("failed to count invoices: %w", countError)
	}

	pageQuery := invoiceSelect + where + ` ORDER BY i.created_at DESC, i.id DESC LIMIT ` + addArgument(limit) + ` OFFSET ` + addArgument(offset)
	invoiceRows, queryError := querier.QueryContext(ctx, pageQuery, arguments...)
	pageInvoices, scanError := scanInvoices(invoiceRows, queryError)
	if scanError != nil {
		return nil, 0, scanError
	}
	return pageInvoices, totalInvoices, nil
}

func (repository *Repository) Summary(ctx context.Context, querier database.Querier, companyId uuid.UUID, scope shopscope.Scope, today string, monthStart time.Time) (SummaryView, error) {
	arguments := []any{companyId}
	shopCondition := scope.Condition("i.shop_id", &arguments)
	arguments = append(arguments, today)
	todayPlaceholder := fmt.Sprintf("$%d", len(arguments))
	query := `
		SELECT
			CAST(COALESCE(SUM(CASE WHEN owed > 0 THEN owed ELSE 0 END), 0) AS BIGINT),
			COALESCE(SUM(CASE WHEN owed > 0 THEN 1 ELSE 0 END), 0),
			CAST(COALESCE(SUM(CASE WHEN owed > 0 AND due_date < ` + todayPlaceholder + ` THEN owed ELSE 0 END), 0) AS BIGINT),
			COALESCE(SUM(CASE WHEN owed > 0 AND due_date < ` + todayPlaceholder + ` THEN 1 ELSE 0 END), 0),
			COALESCE(SUM(CASE WHEN delivered_at IS NULL THEN 1 ELSE 0 END), 0)
		FROM (
			SELECT i.total - ` + paidTotalSql + ` AS owed, i.due_date, i.delivered_at
			FROM invoices i
			WHERE i.company_id = $1` + shopCondition + ` AND i.kind = 'invoice' AND i.status = 'issued'
		) open_invoices
	`
	summary := SummaryView{}
	scanError := querier.QueryRowContext(ctx, query, arguments...).Scan(&summary.OwedToYou, &summary.OwedCount, &summary.Overdue, &summary.OverdueCount, &summary.AwaitingCount)
	if scanError != nil {
		return SummaryView{}, fmt.Errorf("failed to total open invoices: %w", scanError)
	}

	paidArguments := []any{companyId}
	paidShopCondition := scope.Condition("i.shop_id", &paidArguments)
	paidArguments = append(paidArguments, monthStart)
	paidQuery := `
		SELECT CAST(COALESCE(SUM(ip.amount), 0) AS BIGINT)
		FROM invoice_payments ip
		JOIN invoices i ON i.company_id = ip.company_id AND i.id = ip.invoice_id
		LEFT JOIN customer_payments cp ON cp.company_id = ip.company_id AND cp.id = ip.customer_payment_id
		WHERE ip.company_id = $1` + paidShopCondition + ` AND i.kind = 'invoice' AND ip.voided_at IS NULL AND cp.voided_at IS NULL AND ip.created_at >= $` + fmt.Sprint(len(paidArguments))
	paidError := querier.QueryRowContext(ctx, paidQuery, paidArguments...).Scan(&summary.PaidThisMonth)
	if paidError != nil {
		return SummaryView{}, fmt.Errorf("failed to total invoice payments this month: %w", paidError)
	}

	quoteArguments := []any{companyId}
	quoteShopCondition := scope.Condition("i.shop_id", &quoteArguments)
	quoteQuery := `SELECT COUNT(*) FROM invoices i WHERE i.company_id = $1` + quoteShopCondition + ` AND i.kind IN ('quotation', 'proforma') AND i.status = 'issued'`
	quoteError := querier.QueryRowContext(ctx, quoteQuery, quoteArguments...).Scan(&summary.OpenQuoteCount)
	if quoteError != nil {
		return SummaryView{}, fmt.Errorf("failed to count open quotations: %w", quoteError)
	}
	return summary, nil
}

func (repository *Repository) Lines(ctx context.Context, querier database.Querier, companyId uuid.UUID, invoiceId uuid.UUID) ([]LineView, error) {
	query := `
		SELECT il.id, il.product_id, p.sku, il.description, il.unit, il.quantity, il.unit_price, il.discount_amount, il.line_total
		FROM invoice_lines il
		LEFT JOIN products p ON p.company_id = il.company_id AND p.id = il.product_id
		WHERE il.company_id = $1 AND il.invoice_id = $2
		ORDER BY il.position
	`
	lineRows, queryError := querier.QueryContext(ctx, query, companyId, invoiceId)
	if queryError != nil {
		return nil, fmt.Errorf("failed to load invoice lines: %w", queryError)
	}
	defer lineRows.Close()

	lineViews := []LineView{}
	for lineRows.Next() {
		lineView := LineView{}
		scanError := lineRows.Scan(&lineView.Id, &lineView.ProductId, &lineView.Sku, &lineView.Description, &lineView.Unit, &lineView.Quantity,
			&lineView.UnitPrice, &lineView.DiscountAmount, &lineView.LineTotal)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan an invoice line: %w", scanError)
		}
		lineViews = append(lineViews, lineView)
	}
	return lineViews, lineRows.Err()
}

func (repository *Repository) Payments(ctx context.Context, querier database.Querier, companyId uuid.UUID, invoiceId uuid.UUID) ([]PaymentView, error) {
	query := `
		SELECT ip.id, ip.method, ip.amount, COALESCE(cp.reference, sr.reference),
		       (ip.voided_at IS NOT NULL OR cp.voided_at IS NOT NULL OR sr.voided_at IS NOT NULL),
		       ip.created_at, u.name, COALESCE(ip.customer_payment_id, ip.supplier_receipt_id)
		FROM invoice_payments ip
		JOIN users u ON u.company_id = ip.company_id AND u.id = ip.created_by
		LEFT JOIN customer_payments cp ON cp.company_id = ip.company_id AND cp.id = ip.customer_payment_id
		LEFT JOIN supplier_receipts sr ON sr.company_id = ip.company_id AND sr.id = ip.supplier_receipt_id
		WHERE ip.company_id = $1 AND ip.invoice_id = $2
		ORDER BY ip.created_at, ip.id
	`
	paymentRows, queryError := querier.QueryContext(ctx, query, companyId, invoiceId)
	if queryError != nil {
		return nil, fmt.Errorf("failed to load invoice payments: %w", queryError)
	}
	defer paymentRows.Close()

	paymentViews := []PaymentView{}
	for paymentRows.Next() {
		paymentView := PaymentView{}
		scanError := paymentRows.Scan(&paymentView.Id, &paymentView.Method, &paymentView.Amount, &paymentView.Reference, &paymentView.IsVoided,
			&paymentView.CreatedAt, &paymentView.CreatedByName, &paymentView.MoneyRecordId)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan an invoice payment: %w", scanError)
		}
		paymentViews = append(paymentViews, paymentView)
	}
	return paymentViews, paymentRows.Err()
}

func (repository *Repository) Related(ctx context.Context, querier database.Querier, companyId uuid.UUID, invoiceId uuid.UUID) ([]RelatedView, error) {
	query := `SELECT id, kind, number, status FROM invoices WHERE company_id = $1 AND source_invoice_id = $2 ORDER BY created_at`
	relatedRows, queryError := querier.QueryContext(ctx, query, companyId, invoiceId)
	if queryError != nil {
		return nil, fmt.Errorf("failed to load related documents: %w", queryError)
	}
	defer relatedRows.Close()

	relatedViews := []RelatedView{}
	for relatedRows.Next() {
		relatedView := RelatedView{}
		scanError := relatedRows.Scan(&relatedView.Id, &relatedView.Kind, &relatedView.Number, &relatedView.Status)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan a related document: %w", scanError)
		}
		relatedViews = append(relatedViews, relatedView)
	}
	return relatedViews, relatedRows.Err()
}

func (repository *Repository) FindRelated(ctx context.Context, querier database.Querier, companyId uuid.UUID, invoiceId uuid.UUID) (*RelatedView, error) {
	relatedView := RelatedView{}
	scanError := querier.QueryRowContext(ctx, `SELECT id, kind, number, status FROM invoices WHERE company_id = $1 AND id = $2`, companyId, invoiceId).
		Scan(&relatedView.Id, &relatedView.Kind, &relatedView.Number, &relatedView.Status)
	if errors.Is(scanError, sql.ErrNoRows) {
		return nil, nil
	}
	if scanError != nil {
		return nil, fmt.Errorf("failed to load the source document: %w", scanError)
	}
	return &relatedView, nil
}

func (repository *Repository) SetStatus(ctx context.Context, querier database.Querier, companyId uuid.UUID, invoiceId uuid.UUID, status string, changedAt time.Time) error {
	_, updateError := querier.ExecContext(ctx, `UPDATE invoices SET status = $3, updated_at = $4 WHERE company_id = $1 AND id = $2`, companyId, invoiceId, status, changedAt)
	if updateError != nil {
		return fmt.Errorf("failed to change the invoice status: %w", updateError)
	}
	return nil
}

func (repository *Repository) MarkDelivered(ctx context.Context, querier database.Querier, companyId uuid.UUID, invoiceId uuid.UUID, userId uuid.UUID, deliveredAt time.Time) error {
	costQuery := `
		UPDATE invoice_lines
		SET unit_cost = COALESCE((SELECT p.cost_price FROM products p WHERE p.company_id = invoice_lines.company_id AND p.id = invoice_lines.product_id), 0)
		WHERE company_id = $1 AND invoice_id = $2 AND product_id IS NOT NULL
	`
	_, costError := querier.ExecContext(ctx, costQuery, companyId, invoiceId)
	if costError != nil {
		return fmt.Errorf("failed to record the cost of delivered lines: %w", costError)
	}
	query := `UPDATE invoices SET status = 'issued', delivered_at = $3, delivered_by = $4, updated_at = $3 WHERE company_id = $1 AND id = $2`
	_, updateError := querier.ExecContext(ctx, query, companyId, invoiceId, deliveredAt, userId)
	if updateError != nil {
		return fmt.Errorf("failed to mark the invoice delivered: %w", updateError)
	}
	return nil
}

func (repository *Repository) MarkCancelled(ctx context.Context, querier database.Querier, companyId uuid.UUID, invoiceId uuid.UUID, userId uuid.UUID, reason string, cancelledAt time.Time) error {
	query := `UPDATE invoices SET status = 'cancelled', cancelled_at = $3, cancelled_by = $4, cancel_reason = $5, updated_at = $3 WHERE company_id = $1 AND id = $2`
	_, updateError := querier.ExecContext(ctx, query, companyId, invoiceId, cancelledAt, userId, reason)
	if updateError != nil {
		return fmt.Errorf("failed to cancel the invoice: %w", updateError)
	}
	return nil
}

func (repository *Repository) InsertPayment(ctx context.Context, querier database.Querier, payment Payment) error {
	query := `
		INSERT INTO invoice_payments (id, company_id, invoice_id, method, amount, customer_payment_id, supplier_receipt_id, created_by, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`
	_, insertError := querier.ExecContext(ctx, query, payment.Id, payment.CompanyId, payment.InvoiceId, payment.Method, payment.Amount,
		payment.CustomerPaymentId, payment.SupplierReceiptId, payment.CreatedBy, payment.CreatedAt)
	if insertError != nil {
		return fmt.Errorf("failed to save the invoice payment: %w", insertError)
	}
	return nil
}

func (repository *Repository) FindPayment(ctx context.Context, querier database.Querier, companyId uuid.UUID, invoiceId uuid.UUID, paymentId uuid.UUID) (*Payment, bool, error) {
	query := `SELECT id, method, amount, customer_payment_id, supplier_receipt_id, voided_at IS NOT NULL FROM invoice_payments WHERE company_id = $1 AND invoice_id = $2 AND id = $3`
	payment := Payment{CompanyId: companyId, InvoiceId: invoiceId}
	isVoided := false
	scanError := querier.QueryRowContext(ctx, query, companyId, invoiceId, paymentId).
		Scan(&payment.Id, &payment.Method, &payment.Amount, &payment.CustomerPaymentId, &payment.SupplierReceiptId, &isVoided)
	if errors.Is(scanError, sql.ErrNoRows) {
		return nil, false, nil
	}
	if scanError != nil {
		return nil, false, fmt.Errorf("failed to find the invoice payment: %w", scanError)
	}
	return &payment, isVoided, nil
}

func (repository *Repository) VoidPayment(ctx context.Context, querier database.Querier, companyId uuid.UUID, paymentId uuid.UUID, userId uuid.UUID, voidedAt time.Time) error {
	_, updateError := querier.ExecContext(ctx, `UPDATE invoice_payments SET voided_at = $3, voided_by = $4 WHERE company_id = $1 AND id = $2 AND voided_at IS NULL`, companyId, paymentId, voidedAt, userId)
	if updateError != nil {
		return fmt.Errorf("failed to void the invoice payment: %w", updateError)
	}
	return nil
}

func scanInvoices(invoiceRows *sql.Rows, queryError error) ([]InvoiceView, error) {
	if queryError != nil {
		return nil, fmt.Errorf("failed to load invoices: %w", queryError)
	}
	defer invoiceRows.Close()

	invoiceViews := []InvoiceView{}
	for invoiceRows.Next() {
		invoiceView := InvoiceView{}
		customerId := uuid.NullUUID{}
		supplierId := uuid.NullUUID{}
		sourceInvoiceId := uuid.NullUUID{}
		scanError := invoiceRows.Scan(
			&invoiceView.Id, &invoiceView.Kind, &invoiceView.Number, &invoiceView.Status, &customerId, &supplierId,
			&invoiceView.Party.Name, &invoiceView.Party.Phone, &invoiceView.Party.Email, &invoiceView.Party.Address, &invoiceView.Party.Tin,
			&invoiceView.ShopId, &invoiceView.ShopName, &invoiceView.IssueDate, &invoiceView.DueDate, &invoiceView.Reference, &invoiceView.Notes, &invoiceView.Terms,
			&invoiceView.Subtotal, &invoiceView.DiscountTotal, &invoiceView.TaxTotal, &invoiceView.TaxRateBasisPoints, &invoiceView.Total, &invoiceView.PaidTotal,
			&invoiceView.LineCount,
			&sourceInvoiceId, &invoiceView.DeliveredAt, &invoiceView.DeliveredByName, &invoiceView.CancelledAt, &invoiceView.CancelledByName, &invoiceView.CancelReason,
			&invoiceView.CreatedByName, &invoiceView.CreatedAt, &invoiceView.UpdatedAt,
		)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan an invoice: %w", scanError)
		}
		invoiceView.Party.Type = "customer"
		if customerId.Valid {
			invoiceView.Party.Id = customerId.UUID
		}
		if supplierId.Valid {
			invoiceView.Party.Type = "supplier"
			invoiceView.Party.Id = supplierId.UUID
		}
		if sourceInvoiceId.Valid {
			invoiceView.Source = &RelatedView{Id: sourceInvoiceId.UUID}
		}
		invoiceViews = append(invoiceViews, invoiceView)
	}
	return invoiceViews, invoiceRows.Err()
}
