package discounts

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/google/uuid"
)

const discountColumns = `d.id, d.company_id, d.name, d.product_id, d.kind, d.value, d.starts_at, d.ends_at, d.is_active, d.created_by, d.created_at, d.updated_at`

type Repository struct{}

func NewRepository() *Repository {
	return &Repository{}
}

func (repository *Repository) Count(ctx context.Context, querier database.Querier, companyId uuid.UUID) (int64, error) {
	query := `SELECT COUNT(*) FROM discounts WHERE company_id = $1`

	discountCount := int64(0)
	scanError := querier.QueryRowContext(ctx, query, companyId).Scan(&discountCount)
	if scanError != nil {
		return 0, fmt.Errorf("failed to count discounts: %w", scanError)
	}

	return discountCount, nil
}

func (repository *Repository) List(ctx context.Context, querier database.Querier, companyId uuid.UUID, limit int, offset int) ([]Discount, []*string, []*string, error) {
	query := `
		SELECT ` + discountColumns + `, p.name, p.variant_label
		FROM discounts d
		LEFT JOIN products p ON p.company_id = d.company_id AND p.id = d.product_id
		WHERE d.company_id = $1
		ORDER BY d.is_active DESC, d.ends_at DESC, d.id DESC
		LIMIT $2 OFFSET $3
	`

	discountRows, queryError := querier.QueryContext(ctx, query, companyId, limit, offset)
	if queryError != nil {
		return nil, nil, nil, fmt.Errorf("failed to list discounts: %w", queryError)
	}
	defer discountRows.Close()

	foundDiscounts := []Discount{}
	productNames := []*string{}
	variantLabels := []*string{}
	for discountRows.Next() {
		foundDiscount := Discount{}
		var productName *string
		var variantLabel *string
		scanError := discountRows.Scan(append(discountDestinations(&foundDiscount), &productName, &variantLabel)...)
		if scanError != nil {
			return nil, nil, nil, fmt.Errorf("failed to scan discount: %w", scanError)
		}
		foundDiscounts = append(foundDiscounts, foundDiscount)
		productNames = append(productNames, productName)
		variantLabels = append(variantLabels, variantLabel)
	}

	return foundDiscounts, productNames, variantLabels, discountRows.Err()
}

func (repository *Repository) Find(ctx context.Context, querier database.Querier, companyId uuid.UUID, discountId uuid.UUID) (*Discount, *string, *string, error) {
	query := `
		SELECT ` + discountColumns + `, p.name, p.variant_label
		FROM discounts d
		LEFT JOIN products p ON p.company_id = d.company_id AND p.id = d.product_id
		WHERE d.company_id = $1 AND d.id = $2
	`

	foundDiscount := Discount{}
	var productName *string
	var variantLabel *string
	scanError := querier.QueryRowContext(ctx, query, companyId, discountId).Scan(append(discountDestinations(&foundDiscount), &productName, &variantLabel)...)
	if errors.Is(scanError, sql.ErrNoRows) {
		return nil, nil, nil, nil
	}
	if scanError != nil {
		return nil, nil, nil, fmt.Errorf("failed to find discount: %w", scanError)
	}

	return &foundDiscount, productName, variantLabel, nil
}

func (repository *Repository) ListApplicable(ctx context.Context, querier database.Querier, companyId uuid.UUID, productIds []uuid.UUID, now time.Time) ([]Discount, error) {
	query := `
		SELECT ` + discountColumns + `
		FROM discounts d
		WHERE d.company_id = $1 AND d.is_active AND d.starts_at <= $2 AND d.ends_at > $2
		  AND (d.product_id IS NULL OR d.product_id IN (` + database.Placeholders(3, len(productIds)) + `))
	`

	queryArguments := append([]any{companyId, now}, database.ToArguments(productIds)...)
	discountRows, queryError := querier.QueryContext(ctx, query, queryArguments...)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list applicable discounts: %w", queryError)
	}
	defer discountRows.Close()

	applicableDiscounts := []Discount{}
	for discountRows.Next() {
		applicableDiscount := Discount{}
		scanError := discountRows.Scan(discountDestinations(&applicableDiscount)...)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan discount: %w", scanError)
		}
		applicableDiscounts = append(applicableDiscounts, applicableDiscount)
	}

	return applicableDiscounts, discountRows.Err()
}

func (repository *Repository) Insert(ctx context.Context, querier database.Querier, newDiscount Discount) error {
	query := `
		INSERT INTO discounts (id, company_id, name, product_id, kind, value, starts_at, ends_at, is_active, created_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`

	_, insertError := querier.ExecContext(ctx, query,
		newDiscount.Id,
		newDiscount.CompanyId,
		newDiscount.Name,
		newDiscount.ProductId,
		newDiscount.Kind,
		newDiscount.Value,
		newDiscount.StartsAt,
		newDiscount.EndsAt,
		newDiscount.IsActive,
		newDiscount.CreatedBy,
		newDiscount.CreatedAt,
		newDiscount.UpdatedAt,
	)
	if insertError != nil {
		return fmt.Errorf("failed to insert discount: %w", insertError)
	}

	return nil
}

func (repository *Repository) Update(ctx context.Context, querier database.Querier, changedDiscount Discount) error {
	query := `
		UPDATE discounts
		SET name = $3, product_id = $4, kind = $5, value = $6, starts_at = $7, ends_at = $8, is_active = $9, updated_at = $10
		WHERE company_id = $1 AND id = $2
	`

	_, updateError := querier.ExecContext(ctx, query,
		changedDiscount.CompanyId,
		changedDiscount.Id,
		changedDiscount.Name,
		changedDiscount.ProductId,
		changedDiscount.Kind,
		changedDiscount.Value,
		changedDiscount.StartsAt,
		changedDiscount.EndsAt,
		changedDiscount.IsActive,
		changedDiscount.UpdatedAt,
	)
	if updateError != nil {
		return fmt.Errorf("failed to update discount: %w", updateError)
	}

	return nil
}

func (repository *Repository) IsActiveProduct(ctx context.Context, querier database.Querier, companyId uuid.UUID, productId uuid.UUID) (bool, error) {
	query := `SELECT COUNT(*) FROM products WHERE company_id = $1 AND id = $2 AND is_active`

	matchCount := 0
	scanError := querier.QueryRowContext(ctx, query, companyId, productId).Scan(&matchCount)
	if scanError != nil {
		return false, fmt.Errorf("failed to find product: %w", scanError)
	}

	return matchCount > 0, nil
}

func discountDestinations(discount *Discount) []any {
	return []any{
		&discount.Id,
		&discount.CompanyId,
		&discount.Name,
		&discount.ProductId,
		&discount.Kind,
		&discount.Value,
		&discount.StartsAt,
		&discount.EndsAt,
		&discount.IsActive,
		&discount.CreatedBy,
		&discount.CreatedAt,
		&discount.UpdatedAt,
	}
}
