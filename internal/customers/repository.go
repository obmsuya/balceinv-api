package customers

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/google/uuid"
)

const customerColumns = `
	c.id, c.company_id, c.name, c.phone, c.email, c.address, c.tin, c.credit_limit, c.opening_balance, c.notes, c.is_active,
	c.created_at, c.updated_at,
	(SELECT MAX(s.created_at) FROM sales s WHERE s.company_id = c.company_id AND s.customer_id = c.id) AS last_visit_at
`

const creditOnSales = `
	SELECT s.customer_id, s.id, s.receipt_number, s.created_at, p.amount
	FROM sales s
	JOIN sale_payments p ON p.company_id = s.company_id AND p.sale_id = s.id AND p.method = 'credit'
`

type Repository struct{}

func NewRepository() *Repository {
	return &Repository{}
}

func (repository *Repository) Insert(ctx context.Context, querier database.Querier, newCustomer Customer) error {
	query := `
		INSERT INTO customers (id, company_id, name, phone, email, address, tin, credit_limit, opening_balance, notes, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13)
	`

	_, insertError := querier.ExecContext(ctx, query,
		newCustomer.Id,
		newCustomer.CompanyId,
		newCustomer.Name,
		newCustomer.Phone,
		newCustomer.Email,
		newCustomer.Address,
		newCustomer.Tin,
		newCustomer.CreditLimit,
		newCustomer.OpeningBalance,
		newCustomer.Notes,
		newCustomer.IsActive,
		newCustomer.CreatedAt,
		newCustomer.UpdatedAt,
	)
	if insertError != nil {
		return fmt.Errorf("failed to insert customer: %w", insertError)
	}
	return nil
}

func (repository *Repository) Update(ctx context.Context, querier database.Querier, changedCustomer Customer) error {
	query := `
		UPDATE customers
		SET name = $3, phone = $4, email = $5, address = $6, tin = $7, credit_limit = $8, opening_balance = $9, notes = $10,
		    is_active = $11, updated_at = $12
		WHERE company_id = $1 AND id = $2
	`

	_, updateError := querier.ExecContext(ctx, query,
		changedCustomer.CompanyId,
		changedCustomer.Id,
		changedCustomer.Name,
		changedCustomer.Phone,
		changedCustomer.Email,
		changedCustomer.Address,
		changedCustomer.Tin,
		changedCustomer.CreditLimit,
		changedCustomer.OpeningBalance,
		changedCustomer.Notes,
		changedCustomer.IsActive,
		changedCustomer.UpdatedAt,
	)
	if updateError != nil {
		return fmt.Errorf("failed to update customer: %w", updateError)
	}
	return nil
}

func (repository *Repository) Lock(ctx context.Context, querier database.Querier, companyId uuid.UUID, customerId uuid.UUID) (bool, error) {
	query := `UPDATE customers SET updated_at = updated_at WHERE company_id = $1 AND id = $2`

	lockResult, lockError := querier.ExecContext(ctx, query, companyId, customerId)
	if lockError != nil {
		return false, fmt.Errorf("failed to lock customer: %w", lockError)
	}
	lockedCount, countError := lockResult.RowsAffected()
	if countError != nil {
		return false, fmt.Errorf("failed to count locked customers: %w", countError)
	}
	return lockedCount == 1, nil
}

func (repository *Repository) Find(ctx context.Context, querier database.Querier, companyId uuid.UUID, customerId uuid.UUID) (*Customer, error) {
	query := `SELECT ` + customerColumns + ` FROM customers c WHERE c.company_id = $1 AND c.id = $2`

	customerRows, queryError := querier.QueryContext(ctx, query, companyId, customerId)
	if queryError != nil {
		return nil, fmt.Errorf("failed to find customer: %w", queryError)
	}
	foundCustomers, scanError := scanCustomers(customerRows)
	if scanError != nil {
		return nil, scanError
	}
	if len(foundCustomers) == 0 {
		return nil, nil
	}
	return &foundCustomers[0], nil
}

func (repository *Repository) List(ctx context.Context, querier database.Querier, companyId uuid.UUID, filter CustomerFilter, limit int, offset int) ([]Customer, int64, error) {
	conditions := []string{"c.company_id = $1"}
	arguments := []any{companyId}
	addArgument := func(argument any) string {
		arguments = append(arguments, argument)
		return "$" + strconv.Itoa(len(arguments))
	}

	if !filter.IncludeInactive {
		conditions = append(conditions, "c.is_active")
	}
	searchText := strings.ToLower(strings.TrimSpace(filter.SearchText))
	if searchText != "" {
		searchConditions := []string{"lower(c.name) LIKE " + addArgument("%"+searchText+"%")}
		if filter.SearchPhone != "" {
			searchConditions = append(searchConditions, "c.phone LIKE "+addArgument("%"+filter.SearchPhone+"%"))
		}
		conditions = append(conditions, "("+strings.Join(searchConditions, " OR ")+")")
	}
	whereClause := " WHERE " + strings.Join(conditions, " AND ")

	totalCustomers := int64(0)
	countError := querier.QueryRowContext(ctx, `SELECT COUNT(*) FROM customers c`+whereClause, arguments...).Scan(&totalCustomers)
	if countError != nil {
		return nil, 0, fmt.Errorf("failed to count customers: %w", countError)
	}

	orderClause := " ORDER BY lower(c.name), c.id"
	if filter.SortRecent {
		orderClause = " ORDER BY last_visit_at DESC NULLS LAST, lower(c.name), c.id"
	}
	query := `SELECT ` + customerColumns + ` FROM customers c` + whereClause + orderClause +
		` LIMIT ` + addArgument(limit) + ` OFFSET ` + addArgument(offset)

	customerRows, queryError := querier.QueryContext(ctx, query, arguments...)
	if queryError != nil {
		return nil, 0, fmt.Errorf("failed to list customers: %w", queryError)
	}
	foundCustomers, scanError := scanCustomers(customerRows)
	if scanError != nil {
		return nil, 0, scanError
	}
	return foundCustomers, totalCustomers, nil
}

func (repository *Repository) ListOwing(ctx context.Context, querier database.Querier, companyId uuid.UUID) ([]Customer, error) {
	query := `
		SELECT ` + customerColumns + `
		FROM customers c
		WHERE c.company_id = $1 AND (
			c.opening_balance > 0 OR EXISTS (
				SELECT 1 FROM sales s
				JOIN sale_payments p ON p.company_id = s.company_id AND p.sale_id = s.id AND p.method = 'credit'
				WHERE s.company_id = c.company_id AND s.customer_id = c.id
			)
		)
	`

	customerRows, queryError := querier.QueryContext(ctx, query, companyId)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list customers who owe: %w", queryError)
	}
	return scanCustomers(customerRows)
}

func (repository *Repository) Balance(ctx context.Context, querier database.Querier, companyId uuid.UUID, customerId uuid.UUID) (int64, error) {
	query := `
		SELECT CAST(
			c.opening_balance
			+ COALESCE((
				SELECT SUM(p.amount) FROM sales s
				JOIN sale_payments p ON p.company_id = s.company_id AND p.sale_id = s.id AND p.method = 'credit'
				WHERE s.company_id = c.company_id AND s.customer_id = c.id
			), 0)
			- COALESCE((
				SELECT SUM(cp.amount) FROM customer_payments cp
				WHERE cp.company_id = c.company_id AND cp.customer_id = c.id AND cp.voided_at IS NULL
			), 0)
		AS BIGINT)
		FROM customers c
		WHERE c.company_id = $1 AND c.id = $2
	`

	balance := int64(0)
	scanError := querier.QueryRowContext(ctx, query, companyId, customerId).Scan(&balance)
	if scanError != nil {
		return 0, fmt.Errorf("failed to work out the customer's balance: %w", scanError)
	}
	return balance, nil
}

func (repository *Repository) CreditDebts(ctx context.Context, querier database.Querier, companyId uuid.UUID, customerIds []uuid.UUID) ([]Debt, error) {
	if len(customerIds) == 0 {
		return []Debt{}, nil
	}

	query := creditOnSales + ` WHERE s.company_id = $1 AND s.customer_id IN (` + database.Placeholders(2, len(customerIds)) + `)`
	queryArguments := append([]any{companyId}, database.ToArguments(customerIds)...)
	debtRows, queryError := querier.QueryContext(ctx, query, queryArguments...)
	if queryError != nil {
		return nil, fmt.Errorf("failed to load credit sales: %w", queryError)
	}
	defer debtRows.Close()

	debts := []Debt{}
	for debtRows.Next() {
		debt := Debt{}
		saleId := uuid.UUID{}
		receiptNumber := ""
		scanError := debtRows.Scan(&debt.CustomerId, &saleId, &receiptNumber, &debt.OccurredAt, &debt.Amount)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan a credit sale: %w", scanError)
		}
		debts = append(debts, debt)
	}
	return debts, debtRows.Err()
}

func (repository *Repository) PaidTotals(ctx context.Context, querier database.Querier, companyId uuid.UUID, customerIds []uuid.UUID) (map[uuid.UUID]int64, error) {
	paidByCustomer := map[uuid.UUID]int64{}
	if len(customerIds) == 0 {
		return paidByCustomer, nil
	}

	query := `
		SELECT customer_id, CAST(SUM(amount) AS BIGINT)
		FROM customer_payments
		WHERE company_id = $1 AND voided_at IS NULL AND customer_id IN (` + database.Placeholders(2, len(customerIds)) + `)
		GROUP BY customer_id
	`
	queryArguments := append([]any{companyId}, database.ToArguments(customerIds)...)
	paidRows, queryError := querier.QueryContext(ctx, query, queryArguments...)
	if queryError != nil {
		return nil, fmt.Errorf("failed to total customer payments: %w", queryError)
	}
	defer paidRows.Close()

	for paidRows.Next() {
		customerId := uuid.UUID{}
		paidTotal := int64(0)
		scanError := paidRows.Scan(&customerId, &paidTotal)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan a payment total: %w", scanError)
		}
		paidByCustomer[customerId] = paidTotal
	}
	return paidByCustomer, paidRows.Err()
}

func (repository *Repository) InsertPayment(ctx context.Context, querier database.Querier, newPayment Payment) error {
	query := `
		INSERT INTO customer_payments (id, company_id, customer_id, shop_id, amount, method, reference, received_at, created_by)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`

	_, insertError := querier.ExecContext(ctx, query,
		newPayment.Id,
		newPayment.CompanyId,
		newPayment.CustomerId,
		newPayment.ShopId,
		newPayment.Amount,
		newPayment.Method,
		newPayment.Reference,
		newPayment.ReceivedAt,
		newPayment.CreatedBy,
	)
	if insertError != nil {
		return fmt.Errorf("failed to record customer payment: %w", insertError)
	}
	return nil
}

func (repository *Repository) VoidPayment(ctx context.Context, querier database.Querier, companyId uuid.UUID, customerId uuid.UUID, paymentId uuid.UUID, voidedBy uuid.UUID, reason string, voidedAt time.Time) (bool, error) {
	query := `
		UPDATE customer_payments
		SET voided_at = $5, voided_by = $4, void_reason = $6
		WHERE company_id = $1 AND customer_id = $2 AND id = $3 AND voided_at IS NULL
	`

	voidResult, voidError := querier.ExecContext(ctx, query, companyId, customerId, paymentId, voidedBy, voidedAt, reason)
	if voidError != nil {
		return false, fmt.Errorf("failed to void customer payment: %w", voidError)
	}
	voidedCount, countError := voidResult.RowsAffected()
	if countError != nil {
		return false, fmt.Errorf("failed to count voided payments: %w", countError)
	}
	return voidedCount == 1, nil
}

const paymentViewQuery = `
	SELECT cp.id, cp.customer_id, cp.amount, cp.method, cp.reference, cp.received_at, sh.name, creator.name,
	       cp.voided_at, voider.name, cp.void_reason
	FROM customer_payments cp
	JOIN users creator ON creator.company_id = cp.company_id AND creator.id = cp.created_by
	LEFT JOIN users voider ON voider.company_id = cp.company_id AND voider.id = cp.voided_by
	LEFT JOIN shops sh ON sh.company_id = cp.company_id AND sh.id = cp.shop_id
`

func (repository *Repository) FindPayment(ctx context.Context, querier database.Querier, companyId uuid.UUID, customerId uuid.UUID, paymentId uuid.UUID) (*PaymentView, error) {
	query := paymentViewQuery + ` WHERE cp.company_id = $1 AND cp.customer_id = $2 AND cp.id = $3`

	paymentRows, queryError := querier.QueryContext(ctx, query, companyId, customerId, paymentId)
	if queryError != nil {
		return nil, fmt.Errorf("failed to find customer payment: %w", queryError)
	}
	paymentViews, scanError := scanPayments(paymentRows)
	if scanError != nil {
		return nil, scanError
	}
	if len(paymentViews) == 0 {
		return nil, nil
	}
	return &paymentViews[0], nil
}

func (repository *Repository) ListPayments(ctx context.Context, querier database.Querier, companyId uuid.UUID, customerId uuid.UUID) ([]PaymentView, error) {
	query := paymentViewQuery + ` WHERE cp.company_id = $1 AND cp.customer_id = $2 ORDER BY cp.received_at DESC, cp.id DESC`

	paymentRows, queryError := querier.QueryContext(ctx, query, companyId, customerId)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list customer payments: %w", queryError)
	}
	return scanPayments(paymentRows)
}

func (repository *Repository) CountSales(ctx context.Context, querier database.Querier, companyId uuid.UUID, customerId uuid.UUID) (int64, error) {
	saleCount := int64(0)
	scanError := querier.QueryRowContext(ctx, `SELECT COUNT(*) FROM sales WHERE company_id = $1 AND customer_id = $2`, companyId, customerId).Scan(&saleCount)
	if scanError != nil {
		return 0, fmt.Errorf("failed to count customer sales: %w", scanError)
	}
	return saleCount, nil
}

func (repository *Repository) ListSales(ctx context.Context, querier database.Querier, companyId uuid.UUID, customerId uuid.UUID, limit int, offset int) ([]CustomerSaleView, error) {
	query := `
		SELECT s.id, s.receipt_number, sh.name, s.total,
		       COALESCE((SELECT p.amount FROM sale_payments p WHERE p.company_id = s.company_id AND p.sale_id = s.id AND p.method = 'credit'), 0),
		       s.created_at
		FROM sales s
		JOIN shops sh ON sh.company_id = s.company_id AND sh.id = s.shop_id
		WHERE s.company_id = $1 AND s.customer_id = $2
		ORDER BY s.created_at DESC, s.id DESC
		LIMIT $3 OFFSET $4
	`

	saleRows, queryError := querier.QueryContext(ctx, query, companyId, customerId, limit, offset)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list customer sales: %w", queryError)
	}
	defer saleRows.Close()

	saleViews := []CustomerSaleView{}
	for saleRows.Next() {
		saleView := CustomerSaleView{}
		scanError := saleRows.Scan(&saleView.Id, &saleView.ReceiptNumber, &saleView.ShopName, &saleView.Total, &saleView.CreditAmount, &saleView.CreatedAt)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan customer sale: %w", scanError)
		}
		saleViews = append(saleViews, saleView)
	}
	return saleViews, saleRows.Err()
}

func (repository *Repository) StatementEntries(ctx context.Context, querier database.Querier, companyId uuid.UUID, customerId uuid.UUID, endsAt time.Time) ([]StatementEntry, error) {
	creditQuery := creditOnSales + ` WHERE s.company_id = $1 AND s.customer_id = $2 AND s.created_at < $3`
	creditRows, creditQueryError := querier.QueryContext(ctx, creditQuery, companyId, customerId, endsAt)
	if creditQueryError != nil {
		return nil, fmt.Errorf("failed to load credit sales for the statement: %w", creditQueryError)
	}
	defer creditRows.Close()

	entries := []StatementEntry{}
	for creditRows.Next() {
		entry := StatementEntry{Kind: "credit_sale"}
		saleCustomerId := uuid.UUID{}
		saleId := uuid.UUID{}
		scanError := creditRows.Scan(&saleCustomerId, &saleId, &entry.Reference, &entry.At, &entry.Debit)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan a statement sale: %w", scanError)
		}
		entry.SaleId = &saleId
		entries = append(entries, entry)
	}
	creditRowsError := creditRows.Err()
	if creditRowsError != nil {
		return nil, creditRowsError
	}

	paymentQuery := `
		SELECT id, method, reference, received_at, amount
		FROM customer_payments
		WHERE company_id = $1 AND customer_id = $2 AND voided_at IS NULL AND received_at < $3
	`
	paymentRows, paymentQueryError := querier.QueryContext(ctx, paymentQuery, companyId, customerId, endsAt)
	if paymentQueryError != nil {
		return nil, fmt.Errorf("failed to load payments for the statement: %w", paymentQueryError)
	}
	defer paymentRows.Close()

	for paymentRows.Next() {
		entry := StatementEntry{Kind: "payment"}
		paymentId := uuid.UUID{}
		method := ""
		reference := sql.NullString{}
		scanError := paymentRows.Scan(&paymentId, &method, &reference, &entry.At, &entry.Credit)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan a statement payment: %w", scanError)
		}
		entry.PaymentId = &paymentId
		entry.Reference = method
		if reference.Valid && reference.String != "" {
			entry.Reference = method + " " + reference.String
		}
		entries = append(entries, entry)
	}
	return entries, paymentRows.Err()
}

func scanCustomers(customerRows *sql.Rows) ([]Customer, error) {
	defer customerRows.Close()

	foundCustomers := []Customer{}
	for customerRows.Next() {
		foundCustomer := Customer{}
		lastVisitText := sql.NullString{}
		scanError := customerRows.Scan(
			&foundCustomer.Id,
			&foundCustomer.CompanyId,
			&foundCustomer.Name,
			&foundCustomer.Phone,
			&foundCustomer.Email,
			&foundCustomer.Address,
			&foundCustomer.Tin,
			&foundCustomer.CreditLimit,
			&foundCustomer.OpeningBalance,
			&foundCustomer.Notes,
			&foundCustomer.IsActive,
			&foundCustomer.CreatedAt,
			&foundCustomer.UpdatedAt,
			&lastVisitText,
		)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan customer: %w", scanError)
		}
		foundCustomer.LastVisitAt = parseStoredTime(lastVisitText)
		foundCustomers = append(foundCustomers, foundCustomer)
	}
	return foundCustomers, customerRows.Err()
}

func scanPayments(paymentRows *sql.Rows) ([]PaymentView, error) {
	defer paymentRows.Close()

	paymentViews := []PaymentView{}
	for paymentRows.Next() {
		paymentView := PaymentView{}
		scanError := paymentRows.Scan(
			&paymentView.Id,
			&paymentView.CustomerId,
			&paymentView.Amount,
			&paymentView.Method,
			&paymentView.Reference,
			&paymentView.ReceivedAt,
			&paymentView.ShopName,
			&paymentView.CreatedByName,
			&paymentView.VoidedAt,
			&paymentView.VoidedByName,
			&paymentView.VoidReason,
		)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan customer payment: %w", scanError)
		}
		paymentViews = append(paymentViews, paymentView)
	}
	return paymentViews, paymentRows.Err()
}

var storedTimeLayouts = []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05.999999999Z07:00", "2006-01-02 15:04:05"}

func parseStoredTime(storedText sql.NullString) *time.Time {
	if !storedText.Valid {
		return nil
	}
	for _, layout := range storedTimeLayouts {
		parsedTime, parseError := time.Parse(layout, storedText.String)
		if parseError == nil {
			utcTime := parsedTime.UTC()
			return &utcTime
		}
	}
	return nil
}
