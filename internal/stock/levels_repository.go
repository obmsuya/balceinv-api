package stock

import (
	"context"
	"fmt"
	"strings"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/google/uuid"
)

const levelFrom = `
	FROM products p
	LEFT JOIN shop_stock ss ON ss.company_id = p.company_id AND ss.product_id = p.id AND ss.shop_id = $2
	WHERE p.company_id = $1 AND p.is_active
	  AND ($3 = '' OR lower(p.name) LIKE $4 OR lower(p.sku) LIKE $4)
	  AND ($5 = ''
	       OR ($5 = 'low' AND COALESCE(ss.quantity, 0) <= COALESCE(ss.min_stock, $6))
	       OR ($5 = 'out' AND COALESCE(ss.quantity, 0) = 0))
`

func levelArguments(companyId uuid.UUID, shopId uuid.UUID, filter LevelFilter) []any {
	searchText := strings.ToLower(strings.TrimSpace(filter.SearchText))
	return []any{companyId, shopId, searchText, "%" + searchText + "%", filter.Status, DefaultMinimumStock}
}

func (repository *Repository) CountLevels(ctx context.Context, querier database.Querier, companyId uuid.UUID, shopId uuid.UUID, filter LevelFilter) (int64, error) {
	query := `SELECT COUNT(*) ` + levelFrom

	levelCount := int64(0)
	scanError := querier.QueryRowContext(ctx, query, levelArguments(companyId, shopId, filter)...).Scan(&levelCount)
	if scanError != nil {
		return 0, fmt.Errorf("failed to count stock levels: %w", scanError)
	}

	return levelCount, nil
}

func (repository *Repository) ListLevels(ctx context.Context, querier database.Querier, companyId uuid.UUID, shopId uuid.UUID, filter LevelFilter, limit int, offset int) ([]LevelView, error) {
	query := `
		SELECT p.id, p.parent_id, p.name, p.variant_label, p.sku, p.unit, p.category, p.price, p.cost_price,
		       COALESCE(ss.quantity, 0), COALESCE(ss.min_stock, $6)
		` + levelFrom + `
		ORDER BY p.name, p.variant_label, p.id
		LIMIT $7 OFFSET $8
	`

	queryArguments := append(levelArguments(companyId, shopId, filter), limit, offset)
	levelRows, queryError := querier.QueryContext(ctx, query, queryArguments...)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list stock levels: %w", queryError)
	}
	defer levelRows.Close()

	levelViews := []LevelView{}
	for levelRows.Next() {
		levelView := LevelView{}
		scanError := levelRows.Scan(
			&levelView.ProductId,
			&levelView.ParentId,
			&levelView.Name,
			&levelView.VariantLabel,
			&levelView.Sku,
			&levelView.Unit,
			&levelView.Category,
			&levelView.Price,
			&levelView.CostPrice,
			&levelView.Quantity,
			&levelView.MinStock,
		)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan stock level: %w", scanError)
		}
		levelView.Status = statusFor(levelView.Quantity, levelView.MinStock)
		levelViews = append(levelViews, levelView)
	}

	return levelViews, levelRows.Err()
}

func (repository *Repository) Summarize(ctx context.Context, querier database.Querier, companyId uuid.UUID, shopId uuid.UUID) (SummaryView, error) {
	query := `
		SELECT COUNT(*),
		       CAST(COALESCE(SUM(COALESCE(ss.quantity, 0)), 0) AS BIGINT),
		       CAST(COALESCE(SUM(COALESCE(ss.quantity, 0) * p.cost_price), 0) AS BIGINT),
		       CAST(COALESCE(SUM(COALESCE(ss.quantity, 0) * p.price), 0) AS BIGINT),
		       CAST(COALESCE(SUM(CASE WHEN COALESCE(ss.quantity, 0) > 0 AND COALESCE(ss.quantity, 0) <= COALESCE(ss.min_stock, $3) THEN 1 ELSE 0 END), 0) AS BIGINT),
		       CAST(COALESCE(SUM(CASE WHEN COALESCE(ss.quantity, 0) = 0 THEN 1 ELSE 0 END), 0) AS BIGINT)
		FROM products p
		LEFT JOIN shop_stock ss ON ss.company_id = p.company_id AND ss.product_id = p.id AND ss.shop_id = $2
		WHERE p.company_id = $1 AND p.is_active
	`

	summary := SummaryView{}
	scanError := querier.QueryRowContext(ctx, query, companyId, shopId, DefaultMinimumStock).Scan(
		&summary.ProductCount,
		&summary.TotalUnits,
		&summary.ValueAtCost,
		&summary.ValueAtPrice,
		&summary.LowCount,
		&summary.OutCount,
	)
	if scanError != nil {
		return SummaryView{}, fmt.Errorf("failed to summarize stock: %w", scanError)
	}

	return summary, nil
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
