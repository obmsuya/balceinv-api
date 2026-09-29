package legacyimport

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/google/uuid"
)

type supplierColumn struct {
	Name       string
	IsRequired bool
}

type newTotals struct {
	Products   int64
	Users      int64
	Sales      int64
	SaleLines  int64
	Suppliers  int64
	SalesValue int64
	StockValue int64
}

type Repository struct{}

func NewRepository() *Repository {
	return &Repository{}
}

func (repository *Repository) SetPasswordHash(ctx context.Context, querier database.Querier, companyId uuid.UUID, userId uuid.UUID, passwordHash string) error {
	query := `UPDATE users SET password_hash = $3 WHERE company_id = $1 AND id = $2`
	_, updateError := querier.ExecContext(ctx, query, companyId, userId, passwordHash)
	if updateError != nil {
		return fmt.Errorf("failed to keep the old password: %w", updateError)
	}
	return nil
}

func (repository *Repository) ListSupplierColumns(ctx context.Context, querier database.Querier) ([]supplierColumn, error) {
	query := `SELECT name, "notnull" = 1 AND dflt_value IS NULL AND pk = 0 FROM pragma_table_info('suppliers')`

	columnRows, queryError := querier.QueryContext(ctx, query)
	if queryError != nil {
		return nil, fmt.Errorf("failed to read the suppliers table: %w", queryError)
	}
	defer columnRows.Close()

	supplierColumns := []supplierColumn{}
	for columnRows.Next() {
		column := supplierColumn{}
		scanError := columnRows.Scan(&column.Name, &column.IsRequired)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan supplier column: %w", scanError)
		}
		column.Name = strings.ToLower(column.Name)
		supplierColumns = append(supplierColumns, column)
	}

	return supplierColumns, columnRows.Err()
}

func (repository *Repository) InsertSupplier(ctx context.Context, querier database.Querier, columnNames []string, columnValues []any) error {
	query := `INSERT INTO suppliers (` + strings.Join(columnNames, ", ") + `) VALUES (` + database.Placeholders(1, len(columnNames)) + `)`
	_, insertError := querier.ExecContext(ctx, query, columnValues...)
	if insertError != nil {
		return fmt.Errorf("failed to insert supplier: %w", insertError)
	}
	return nil
}

func (repository *Repository) CountImported(ctx context.Context, querier database.Querier, companyId uuid.UUID, countsSuppliers bool) (newTotals, error) {
	query := `
		SELECT
			(SELECT COUNT(*) FROM products WHERE company_id = $1 AND is_active),
			(SELECT COUNT(*) FROM users WHERE company_id = $1),
			(SELECT COUNT(*) FROM sales WHERE company_id = $1),
			(SELECT COUNT(*) FROM sale_items WHERE company_id = $1),
			(SELECT COALESCE(SUM(total), 0) FROM sales WHERE company_id = $1),
			(SELECT COALESCE(SUM(ss.quantity * p.cost_price), 0)
			 FROM shop_stock ss
			 JOIN products p ON p.company_id = ss.company_id AND p.id = ss.product_id
			 WHERE ss.company_id = $1)
	`

	totals := newTotals{}
	scanError := querier.QueryRowContext(ctx, query, companyId).Scan(
		&totals.Products,
		&totals.Users,
		&totals.Sales,
		&totals.SaleLines,
		&totals.SalesValue,
		&totals.StockValue,
	)
	if scanError != nil {
		return newTotals{}, fmt.Errorf("failed to count imported data: %w", scanError)
	}

	if !countsSuppliers {
		return totals, nil
	}

	supplierCount := sql.NullInt64{}
	supplierScanError := querier.QueryRowContext(ctx, `SELECT COUNT(*) FROM suppliers WHERE company_id = $1`, companyId).Scan(&supplierCount)
	if supplierScanError != nil {
		return newTotals{}, fmt.Errorf("failed to count imported suppliers: %w", supplierScanError)
	}
	totals.Suppliers = supplierCount.Int64

	return totals, nil
}
