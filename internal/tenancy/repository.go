package tenancy

import (
	"context"
	"fmt"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/google/uuid"
)

type Repository struct{}

func NewRepository() *Repository {
	return &Repository{}
}

func (repository *Repository) CountCompanies(ctx context.Context, querier database.Querier) (int64, error) {
	query := `SELECT COUNT(*) FROM companies`

	companyCount := int64(0)
	scanError := querier.QueryRowContext(ctx, query).Scan(&companyCount)
	if scanError != nil {
		return 0, fmt.Errorf("failed to count companies: %w", scanError)
	}

	return companyCount, nil
}

func (repository *Repository) InsertCompany(ctx context.Context, querier database.Querier, newCompany Company) error {
	query := `
		INSERT INTO companies (id, name, business_type, phone, address, tin, currency_code, currency_decimals, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`

	_, insertError := querier.ExecContext(ctx, query,
		newCompany.Id,
		newCompany.Name,
		newCompany.BusinessType,
		newCompany.Phone,
		newCompany.Address,
		newCompany.Tin,
		newCompany.CurrencyCode,
		newCompany.CurrencyDecimals,
		newCompany.CreatedAt,
		newCompany.UpdatedAt,
	)
	if insertError != nil {
		return fmt.Errorf("failed to insert company: %w", insertError)
	}

	return nil
}

func (repository *Repository) InsertShop(ctx context.Context, querier database.Querier, newShop Shop) error {
	query := `
		INSERT INTO shops (id, company_id, name, receipt_prefix, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	_, insertError := querier.ExecContext(ctx, query,
		newShop.Id,
		newShop.CompanyId,
		newShop.Name,
		newShop.ReceiptPrefix,
		newShop.IsActive,
		newShop.CreatedAt,
		newShop.UpdatedAt,
	)
	if insertError != nil {
		return fmt.Errorf("failed to insert shop: %w", insertError)
	}

	return nil
}

func (repository *Repository) FindCompanyName(ctx context.Context, querier database.Querier, companyId uuid.UUID) (string, error) {
	query := `SELECT name FROM companies WHERE id = $1`

	companyName := ""
	scanError := querier.QueryRowContext(ctx, query, companyId).Scan(&companyName)
	if scanError != nil {
		return "", fmt.Errorf("failed to read company name: %w", scanError)
	}

	return companyName, nil
}

func (repository *Repository) ListWorkableShops(ctx context.Context, querier database.Querier, companyId uuid.UUID, userId uuid.UUID, isOwner bool) ([]ShopSummary, error) {
	query := `
		SELECT s.id, s.name
		FROM shops s
		WHERE s.company_id = $1
		  AND s.is_active
		  AND ($3 OR EXISTS (
		      SELECT 1 FROM user_shops us
		      WHERE us.company_id = s.company_id AND us.shop_id = s.id AND us.user_id = $2
		  ))
		ORDER BY s.name
	`

	shopRows, queryError := querier.QueryContext(ctx, query, companyId, userId, isOwner)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list workable shops: %w", queryError)
	}
	defer shopRows.Close()

	shopSummaries := []ShopSummary{}
	for shopRows.Next() {
		shopSummary := ShopSummary{}
		scanError := shopRows.Scan(
			&shopSummary.Id,
			&shopSummary.Name,
		)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan shop: %w", scanError)
		}
		shopSummaries = append(shopSummaries, shopSummary)
	}

	return shopSummaries, shopRows.Err()
}
