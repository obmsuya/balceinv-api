package suppliers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/google/uuid"
)

const supplierColumns = `
	id, company_id, name, contact_person, phone, email, tin, vrn, address, payment_terms_days,
	opening_balance, notes, is_active, created_by, created_at, updated_by, updated_at
`

const supplierFilter = `
	company_id = $1
	AND ($2 = '' OR lower(name) LIKE $3 OR lower(COALESCE(contact_person, '')) LIKE $3 OR COALESCE(phone, '') LIKE $3)
	AND (is_active OR $4)
`

type Repository struct{}

func NewRepository() *Repository {
	return &Repository{}
}

type rowScanner interface {
	Scan(destinations ...any) error
}

func scanSupplier(row rowScanner) (Supplier, error) {
	foundSupplier := Supplier{}
	scanError := row.Scan(
		&foundSupplier.Id,
		&foundSupplier.CompanyId,
		&foundSupplier.Name,
		&foundSupplier.ContactPerson,
		&foundSupplier.Phone,
		&foundSupplier.Email,
		&foundSupplier.Tin,
		&foundSupplier.Vrn,
		&foundSupplier.Address,
		&foundSupplier.PaymentTermsDays,
		&foundSupplier.OpeningBalance,
		&foundSupplier.Notes,
		&foundSupplier.IsActive,
		&foundSupplier.CreatedBy,
		&foundSupplier.CreatedAt,
		&foundSupplier.UpdatedBy,
		&foundSupplier.UpdatedAt,
	)
	return foundSupplier, scanError
}

func supplierFilterArguments(companyId uuid.UUID, filter SupplierFilter) []any {
	searchText := strings.TrimSpace(filter.SearchText)
	likePattern := "%" + strings.ToLower(searchText) + "%"
	return []any{companyId, searchText, likePattern, filter.IncludeInactive}
}

func (repository *Repository) InsertSupplier(ctx context.Context, querier database.Querier, newSupplier Supplier) error {
	query := `
		INSERT INTO suppliers (` + supplierColumns + `)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
	`

	_, insertError := querier.ExecContext(ctx, query,
		newSupplier.Id,
		newSupplier.CompanyId,
		newSupplier.Name,
		newSupplier.ContactPerson,
		newSupplier.Phone,
		newSupplier.Email,
		newSupplier.Tin,
		newSupplier.Vrn,
		newSupplier.Address,
		newSupplier.PaymentTermsDays,
		newSupplier.OpeningBalance,
		newSupplier.Notes,
		newSupplier.IsActive,
		newSupplier.CreatedBy,
		newSupplier.CreatedAt,
		newSupplier.UpdatedBy,
		newSupplier.UpdatedAt,
	)
	if insertError != nil {
		return fmt.Errorf("failed to insert supplier: %w", insertError)
	}

	return nil
}

func (repository *Repository) UpdateSupplier(ctx context.Context, querier database.Querier, changedSupplier Supplier) error {
	query := `
		UPDATE suppliers
		SET name = $3, contact_person = $4, phone = $5, email = $6, tin = $7, vrn = $8, address = $9,
		    payment_terms_days = $10, opening_balance = $11, notes = $12, is_active = $13, updated_by = $14, updated_at = $15
		WHERE company_id = $1 AND id = $2
	`

	_, updateError := querier.ExecContext(ctx, query,
		changedSupplier.CompanyId,
		changedSupplier.Id,
		changedSupplier.Name,
		changedSupplier.ContactPerson,
		changedSupplier.Phone,
		changedSupplier.Email,
		changedSupplier.Tin,
		changedSupplier.Vrn,
		changedSupplier.Address,
		changedSupplier.PaymentTermsDays,
		changedSupplier.OpeningBalance,
		changedSupplier.Notes,
		changedSupplier.IsActive,
		changedSupplier.UpdatedBy,
		changedSupplier.UpdatedAt,
	)
	if updateError != nil {
		return fmt.Errorf("failed to update supplier: %w", updateError)
	}

	return nil
}

func (repository *Repository) FindSupplier(ctx context.Context, querier database.Querier, companyId uuid.UUID, supplierId uuid.UUID) (*Supplier, error) {
	query := `SELECT ` + supplierColumns + ` FROM suppliers WHERE company_id = $1 AND id = $2`

	foundSupplier, scanError := scanSupplier(querier.QueryRowContext(ctx, query, companyId, supplierId))
	if errors.Is(scanError, sql.ErrNoRows) {
		return nil, nil
	}
	if scanError != nil {
		return nil, fmt.Errorf("failed to find supplier: %w", scanError)
	}

	return &foundSupplier, nil
}

func (repository *Repository) CountSuppliers(ctx context.Context, querier database.Querier, companyId uuid.UUID, filter SupplierFilter) (int64, error) {
	query := `SELECT COUNT(*) FROM suppliers WHERE ` + supplierFilter

	supplierCount := int64(0)
	scanError := querier.QueryRowContext(ctx, query, supplierFilterArguments(companyId, filter)...).Scan(&supplierCount)
	if scanError != nil {
		return 0, fmt.Errorf("failed to count suppliers: %w", scanError)
	}

	return supplierCount, nil
}

func (repository *Repository) ListSuppliers(ctx context.Context, querier database.Querier, companyId uuid.UUID, filter SupplierFilter, limit int, offset int) ([]Supplier, error) {
	query := `
		SELECT ` + supplierColumns + ` FROM suppliers
		WHERE ` + supplierFilter + `
		ORDER BY is_active DESC, lower(name), id
		LIMIT $5 OFFSET $6
	`

	queryArguments := append(supplierFilterArguments(companyId, filter), limit, offset)
	return repository.querySuppliers(ctx, querier, query, queryArguments...)
}

func (repository *Repository) ListAllSuppliers(ctx context.Context, querier database.Querier, companyId uuid.UUID) ([]Supplier, error) {
	query := `SELECT ` + supplierColumns + ` FROM suppliers WHERE company_id = $1 ORDER BY lower(name), id`
	return repository.querySuppliers(ctx, querier, query, companyId)
}

func (repository *Repository) querySuppliers(ctx context.Context, querier database.Querier, query string, queryArguments ...any) ([]Supplier, error) {
	supplierRows, queryError := querier.QueryContext(ctx, query, queryArguments...)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list suppliers: %w", queryError)
	}
	defer supplierRows.Close()

	foundSuppliers := []Supplier{}
	for supplierRows.Next() {
		foundSupplier, scanError := scanSupplier(supplierRows)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan supplier: %w", scanError)
		}
		foundSuppliers = append(foundSuppliers, foundSupplier)
	}

	return foundSuppliers, supplierRows.Err()
}

func (repository *Repository) SetSupplierActive(ctx context.Context, querier database.Querier, companyId uuid.UUID, supplierId uuid.UUID, isActive bool, userId uuid.UUID) error {
	query := `UPDATE suppliers SET is_active = $3, updated_by = $4, updated_at = $5 WHERE company_id = $1 AND id = $2`

	_, updateError := querier.ExecContext(ctx, query, companyId, supplierId, isActive, userId, time.Now().UTC())
	if updateError != nil {
		return fmt.Errorf("failed to change supplier status: %w", updateError)
	}

	return nil
}

func supplierScope(firstPlaceholder int, supplierIds []uuid.UUID) string {
	if supplierIds == nil {
		return ""
	}
	if len(supplierIds) == 0 {
		return ` AND 1 = 0`
	}
	return ` AND supplier_id IN (` + database.Placeholders(firstPlaceholder, len(supplierIds)) + `)`
}

func (repository *Repository) LoadLedgers(ctx context.Context, querier database.Querier, companyId uuid.UUID, suppliersInScope []Supplier, restrictToScope bool) (map[uuid.UUID][]LedgerEntry, error) {
	ledgers := map[uuid.UUID][]LedgerEntry{}
	for _, supplierInScope := range suppliersInScope {
		supplierId := supplierInScope.Id
		ledgers[supplierId] = []LedgerEntry{}
		if supplierInScope.OpeningBalance > 0 {
			ledgers[supplierId] = append(ledgers[supplierId], LedgerEntry{
				Kind:    EntryOpeningBalance,
				DatedAt: supplierInScope.CreatedAt,
				Debit:   supplierInScope.OpeningBalance,
			})
		}
	}

	var supplierIds []uuid.UUID
	if restrictToScope {
		supplierIds = make([]uuid.UUID, 0, len(suppliersInScope))
		for _, supplierInScope := range suppliersInScope {
			supplierIds = append(supplierIds, supplierInScope.Id)
		}
	}
	scopeArguments := append([]any{companyId}, database.ToArguments(supplierIds)...)

	ledgerQueries := []struct {
		kind  string
		query string
	}{
		{EntryPurchase, `SELECT supplier_id, id, purchase_number, received_at, total, NULL FROM purchases WHERE company_id = $1 AND status = 'received' AND supplier_id IS NOT NULL`},
		{EntryPayment, `SELECT supplier_id, id, payment_number, paid_at, amount, purchase_id FROM supplier_payments WHERE company_id = $1 AND voided_at IS NULL AND supplier_id IS NOT NULL`},
		{EntryReturn, `SELECT supplier_id, id, return_number, returned_at, total, NULL FROM supplier_returns WHERE company_id = $1`},
	}
	for _, ledgerQuery := range ledgerQueries {
		entryRows, queryError := querier.QueryContext(ctx, ledgerQuery.query+supplierScope(2, supplierIds), scopeArguments...)
		if queryError != nil {
			return nil, fmt.Errorf("failed to load supplier %s entries: %w", ledgerQuery.kind, queryError)
		}

		for entryRows.Next() {
			supplierId := uuid.UUID{}
			documentId := uuid.UUID{}
			entry := LedgerEntry{Kind: ledgerQuery.kind}
			amount := int64(0)
			var linkedPurchaseId *uuid.UUID
			scanError := entryRows.Scan(&supplierId, &documentId, &entry.Reference, &entry.DatedAt, &amount, &linkedPurchaseId)
			if scanError != nil {
				entryRows.Close()
				return nil, fmt.Errorf("failed to scan supplier %s entry: %w", ledgerQuery.kind, scanError)
			}
			entry.DocumentId = &documentId
			entry.PurchaseId = linkedPurchaseId
			if ledgerQuery.kind == EntryPurchase {
				entry.Debit = amount
			} else {
				entry.Credit = amount
			}
			ledgers[supplierId] = append(ledgers[supplierId], entry)
		}
		rowsError := entryRows.Err()
		entryRows.Close()
		if rowsError != nil {
			return nil, fmt.Errorf("failed to read supplier %s entries: %w", ledgerQuery.kind, rowsError)
		}
	}

	return ledgers, nil
}

func (repository *Repository) TakeDocumentNumber(ctx context.Context, querier database.Querier, companyId uuid.UUID, documentKind string) (string, error) {
	query := `
		INSERT INTO supplier_document_counters (company_id, document_kind, last_number)
		VALUES ($1, $2, 1)
		ON CONFLICT (company_id, document_kind) DO UPDATE SET last_number = supplier_document_counters.last_number + 1
		RETURNING last_number
	`

	lastNumber := int64(0)
	scanError := querier.QueryRowContext(ctx, query, companyId, documentKind).Scan(&lastNumber)
	if scanError != nil {
		return "", fmt.Errorf("failed to take a %s number: %w", documentKind, scanError)
	}

	return fmt.Sprintf("%s-%06d", documentPrefixes[documentKind], lastNumber), nil
}

func (repository *Repository) FindOpenShop(ctx context.Context, querier database.Querier, companyId uuid.UUID, shopId uuid.UUID) (bool, error) {
	query := `SELECT COUNT(*) FROM shops WHERE company_id = $1 AND id = $2 AND is_active`

	matchCount := 0
	scanError := querier.QueryRowContext(ctx, query, companyId, shopId).Scan(&matchCount)
	if scanError != nil {
		return false, fmt.Errorf("failed to find shop: %w", scanError)
	}

	return matchCount > 0, nil
}

func (repository *Repository) IsAssignedToShop(ctx context.Context, querier database.Querier, companyId uuid.UUID, userId uuid.UUID, shopId uuid.UUID) (bool, error) {
	query := `SELECT COUNT(*) FROM user_shops WHERE company_id = $1 AND user_id = $2 AND shop_id = $3`

	matchCount := 0
	scanError := querier.QueryRowContext(ctx, query, companyId, userId, shopId).Scan(&matchCount)
	if scanError != nil {
		return false, fmt.Errorf("failed to check shop assignment: %w", scanError)
	}

	return matchCount > 0, nil
}

func (repository *Repository) LoadCostedProducts(ctx context.Context, querier database.Querier, companyId uuid.UUID, productIds []uuid.UUID) (map[uuid.UUID]CostedProduct, error) {
	query := `
		SELECT p.id, p.cost_price, p.preferred_supplier_id,
		       CAST(COALESCE((
		           SELECT SUM(ss.quantity)
		           FROM shop_stock ss
		           JOIN shops s ON s.company_id = ss.company_id AND s.id = ss.shop_id AND s.is_active
		           WHERE ss.company_id = p.company_id AND ss.product_id = p.id
		       ), 0) AS BIGINT)
		FROM products p
		WHERE p.company_id = $1 AND p.is_active AND p.id IN (` + database.Placeholders(2, len(productIds)) + `)
	`

	queryArguments := append([]any{companyId}, database.ToArguments(productIds)...)
	productRows, queryError := querier.QueryContext(ctx, query, queryArguments...)
	if queryError != nil {
		return nil, fmt.Errorf("failed to load product costs: %w", queryError)
	}
	defer productRows.Close()

	costedProducts := map[uuid.UUID]CostedProduct{}
	for productRows.Next() {
		costedProduct := CostedProduct{}
		scanError := productRows.Scan(&costedProduct.Id, &costedProduct.CostPrice, &costedProduct.PreferredSupplierId, &costedProduct.OnHandQuantity)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan product cost: %w", scanError)
		}
		costedProducts[costedProduct.Id] = costedProduct
	}

	return costedProducts, productRows.Err()
}

func (repository *Repository) SetProductCost(ctx context.Context, querier database.Querier, companyId uuid.UUID, productId uuid.UUID, costPrice int64) error {
	query := `UPDATE products SET cost_price = $3, updated_at = $4 WHERE company_id = $1 AND id = $2`

	_, updateError := querier.ExecContext(ctx, query, companyId, productId, costPrice, time.Now().UTC())
	if updateError != nil {
		return fmt.Errorf("failed to update product cost: %w", updateError)
	}

	return nil
}

func (repository *Repository) SetPreferredSupplier(ctx context.Context, querier database.Querier, companyId uuid.UUID, productId uuid.UUID, supplierId uuid.UUID) error {
	query := `UPDATE products SET preferred_supplier_id = $3 WHERE company_id = $1 AND id = $2 AND preferred_supplier_id IS NULL`

	_, updateError := querier.ExecContext(ctx, query, companyId, productId, supplierId)
	if updateError != nil {
		return fmt.Errorf("failed to set the usual supplier: %w", updateError)
	}

	return nil
}
