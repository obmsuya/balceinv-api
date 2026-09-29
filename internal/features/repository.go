package features

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/google/uuid"
)

type Repository struct{}

func NewRepository() *Repository {
	return &Repository{}
}

func (repository *Repository) Find(ctx context.Context, querier database.Querier, companyId uuid.UUID) (Features, error) {
	query := `
		SELECT company_id, suppliers_enabled, purchase_orders_enabled, customers_enabled, credit_sales_enabled,
		       customer_orders_enabled, accounting_mode, vat_registered, vat_number, updated_by, updated_at
		FROM company_features
		WHERE company_id = $1
	`

	foundFeatures := Features{}
	scanError := querier.QueryRowContext(ctx, query, companyId).Scan(
		&foundFeatures.CompanyId,
		&foundFeatures.SuppliersEnabled,
		&foundFeatures.PurchaseOrdersEnabled,
		&foundFeatures.CustomersEnabled,
		&foundFeatures.CreditSalesEnabled,
		&foundFeatures.CustomerOrdersEnabled,
		&foundFeatures.AccountingMode,
		&foundFeatures.VatRegistered,
		&foundFeatures.VatNumber,
		&foundFeatures.UpdatedBy,
		&foundFeatures.UpdatedAt,
	)
	if errors.Is(scanError, sql.ErrNoRows) {
		return Defaults(companyId), nil
	}
	if scanError != nil {
		return Features{}, fmt.Errorf("failed to find company features: %w", scanError)
	}

	return foundFeatures, nil
}

func (repository *Repository) Save(ctx context.Context, querier database.Querier, changedFeatures Features) error {
	query := `
		INSERT INTO company_features (
			company_id, suppliers_enabled, purchase_orders_enabled, customers_enabled, credit_sales_enabled,
			customer_orders_enabled, accounting_mode, vat_registered, vat_number, updated_by, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
		ON CONFLICT (company_id) DO UPDATE SET
			suppliers_enabled = excluded.suppliers_enabled,
			purchase_orders_enabled = excluded.purchase_orders_enabled,
			customers_enabled = excluded.customers_enabled,
			credit_sales_enabled = excluded.credit_sales_enabled,
			customer_orders_enabled = excluded.customer_orders_enabled,
			accounting_mode = excluded.accounting_mode,
			vat_registered = excluded.vat_registered,
			vat_number = excluded.vat_number,
			updated_by = excluded.updated_by,
			updated_at = excluded.updated_at
	`

	_, saveError := querier.ExecContext(ctx, query,
		changedFeatures.CompanyId,
		changedFeatures.SuppliersEnabled,
		changedFeatures.PurchaseOrdersEnabled,
		changedFeatures.CustomersEnabled,
		changedFeatures.CreditSalesEnabled,
		changedFeatures.CustomerOrdersEnabled,
		changedFeatures.AccountingMode,
		changedFeatures.VatRegistered,
		changedFeatures.VatNumber,
		changedFeatures.UpdatedBy,
		changedFeatures.UpdatedAt,
	)
	if saveError != nil {
		return fmt.Errorf("failed to save company features: %w", saveError)
	}

	return nil
}
