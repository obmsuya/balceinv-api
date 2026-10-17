package stock

import (
	"context"
	"fmt"
	"strings"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/shopscope"
	"github.com/google/uuid"
)

const levelQuantity = `COALESCE(ss.quantity, 0)`
const levelMinimum = `COALESCE(ss.min_stock, $5)`

func levelGrouped(scope shopscope.Scope, filter LevelFilter, arguments *[]any) string {
	searchText := strings.ToLower(strings.TrimSpace(filter.SearchText))
	*arguments = append(*arguments, searchText, "%"+searchText+"%", filter.Status, DefaultMinimumStock, filter.ProductId)
	return `
	FROM products p
	JOIN shops sh ON sh.company_id = p.company_id` + scope.Condition("sh.id", arguments) + `
	LEFT JOIN shop_stock ss ON ss.company_id = p.company_id AND ss.product_id = p.id AND ss.shop_id = sh.id
	WHERE p.company_id = $1 AND p.is_active
	  AND ($2 = '' OR lower(p.name) LIKE $3 OR lower(p.sku) LIKE $3)
	  AND ($6 = '' OR CAST(p.id AS TEXT) = $6)
	GROUP BY p.id
	HAVING $4 = ''
	    OR ($4 = 'low' AND SUM(CASE WHEN ` + levelQuantity + ` <= ` + levelMinimum + ` THEN 1 ELSE 0 END) > 0)
	    OR ($4 = 'out' AND SUM(CASE WHEN ` + levelQuantity + ` = 0 THEN 1 ELSE 0 END) > 0)
`
}

func (repository *Repository) CountLevels(ctx context.Context, querier database.Querier, companyId uuid.UUID, scope shopscope.Scope, filter LevelFilter) (int64, error) {
	arguments := []any{companyId}
	query := `SELECT COUNT(*) FROM (SELECT p.id ` + levelGrouped(scope, filter, &arguments) + `) counted`

	levelCount := int64(0)
	scanError := querier.QueryRowContext(ctx, query, arguments...).Scan(&levelCount)
	if scanError != nil {
		return 0, fmt.Errorf("failed to count stock levels: %w", scanError)
	}

	return levelCount, nil
}

func (repository *Repository) ListLevels(ctx context.Context, querier database.Querier, companyId uuid.UUID, scope shopscope.Scope, filter LevelFilter, limit int, offset int) ([]LevelView, error) {
	arguments := []any{companyId}
	groupedFrom := levelGrouped(scope, filter, &arguments)
	arguments = append(arguments, limit, offset)
	query := `
		SELECT p.id, p.parent_id, p.name, p.variant_label, p.sku, p.unit, p.category, p.price, p.cost_price,
		       CAST(SUM(` + levelQuantity + `) AS BIGINT), CAST(SUM(` + levelMinimum + `) AS BIGINT)
		` + groupedFrom + `
		ORDER BY p.name, p.variant_label, p.id
		LIMIT $` + fmt.Sprint(len(arguments)-1) + ` OFFSET $` + fmt.Sprint(len(arguments))

	levelRows, queryError := querier.QueryContext(ctx, query, arguments...)
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

func (repository *Repository) ListShopLevels(ctx context.Context, querier database.Querier, companyId uuid.UUID, scope shopscope.Scope, productIds []uuid.UUID) ([]ProductShopLevel, error) {
	if len(productIds) == 0 {
		return []ProductShopLevel{}, nil
	}

	arguments := []any{companyId, DefaultMinimumStock}
	arguments = append(arguments, database.ToArguments(productIds)...)
	productPlaceholders := database.Placeholders(3, len(productIds))
	query := `
		SELECT p.id, sh.id, sh.name, COALESCE(ss.quantity, 0), COALESCE(ss.min_stock, $2)
		FROM products p
		JOIN shops sh ON sh.company_id = p.company_id` + scope.Condition("sh.id", &arguments) + `
		LEFT JOIN shop_stock ss ON ss.company_id = p.company_id AND ss.product_id = p.id AND ss.shop_id = sh.id
		WHERE p.company_id = $1 AND p.id IN (` + productPlaceholders + `)
		ORDER BY sh.name, sh.id
	`

	shopRows, queryError := querier.QueryContext(ctx, query, arguments...)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list stock per shop: %w", queryError)
	}
	defer shopRows.Close()

	shopLevels := []ProductShopLevel{}
	for shopRows.Next() {
		shopLevel := ProductShopLevel{}
		scanError := shopRows.Scan(&shopLevel.ProductId, &shopLevel.Level.ShopId, &shopLevel.Level.ShopName, &shopLevel.Level.Quantity, &shopLevel.Level.MinStock)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan stock per shop: %w", scanError)
		}
		shopLevel.Level.Status = statusFor(shopLevel.Level.Quantity, shopLevel.Level.MinStock)
		shopLevels = append(shopLevels, shopLevel)
	}

	return shopLevels, shopRows.Err()
}

func (repository *Repository) Summarize(ctx context.Context, querier database.Querier, companyId uuid.UUID, scope shopscope.Scope) (SummaryView, error) {
	arguments := []any{companyId, DefaultMinimumStock}
	query := `
		SELECT COUNT(*),
		       CAST(COALESCE(SUM(units), 0) AS BIGINT),
		       CAST(COALESCE(SUM(units * cost_price), 0) AS BIGINT),
		       CAST(COALESCE(SUM(units * price), 0) AS BIGINT),
		       CAST(COALESCE(SUM(CASE WHEN low_shops > 0 THEN 1 ELSE 0 END), 0) AS BIGINT),
		       CAST(COALESCE(SUM(CASE WHEN out_shops > 0 THEN 1 ELSE 0 END), 0) AS BIGINT)
		FROM (
			SELECT p.id, p.cost_price, p.price,
			       SUM(COALESCE(ss.quantity, 0)) AS units,
			       SUM(CASE WHEN COALESCE(ss.quantity, 0) > 0 AND COALESCE(ss.quantity, 0) <= COALESCE(ss.min_stock, $2) THEN 1 ELSE 0 END) AS low_shops,
			       SUM(CASE WHEN COALESCE(ss.quantity, 0) = 0 THEN 1 ELSE 0 END) AS out_shops
			FROM products p
			JOIN shops sh ON sh.company_id = p.company_id` + scope.Condition("sh.id", &arguments) + `
			LEFT JOIN shop_stock ss ON ss.company_id = p.company_id AND ss.product_id = p.id AND ss.shop_id = sh.id
			WHERE p.company_id = $1 AND p.is_active
			GROUP BY p.id, p.cost_price, p.price
		) per_product
	`

	summary := SummaryView{}
	scanError := querier.QueryRowContext(ctx, query, arguments...).Scan(
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
