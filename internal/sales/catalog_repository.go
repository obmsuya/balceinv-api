package sales

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

func (repository *Repository) LoadProducts(ctx context.Context, querier database.Querier, companyId uuid.UUID, shopId uuid.UUID, productIds []uuid.UUID) (map[uuid.UUID]PricingProduct, error) {
	query := `
		SELECT p.id, p.name, p.variant_label, p.sku, p.unit, p.price, p.cost_price, p.wholesale_price, p.wholesale_min,
		       COALESCE(ss.quantity, 0)
		FROM products p
		LEFT JOIN shop_stock ss ON ss.company_id = p.company_id AND ss.product_id = p.id AND ss.shop_id = $2
		WHERE p.company_id = $1 AND p.is_active AND p.id IN (` + database.Placeholders(3, len(productIds)) + `)
	`

	queryArguments := append([]any{companyId, shopId}, database.ToArguments(productIds)...)
	productRows, queryError := querier.QueryContext(ctx, query, queryArguments...)
	if queryError != nil {
		return nil, fmt.Errorf("failed to load sale products: %w", queryError)
	}
	defer productRows.Close()

	productsById := map[uuid.UUID]PricingProduct{}
	for productRows.Next() {
		pricingProduct := PricingProduct{}
		scanError := productRows.Scan(
			&pricingProduct.Id,
			&pricingProduct.Name,
			&pricingProduct.VariantLabel,
			&pricingProduct.Sku,
			&pricingProduct.Unit,
			&pricingProduct.Price,
			&pricingProduct.CostPrice,
			&pricingProduct.WholesalePrice,
			&pricingProduct.WholesaleMin,
			&pricingProduct.InStock,
		)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan sale product: %w", scanError)
		}
		productsById[pricingProduct.Id] = pricingProduct
	}

	return productsById, productRows.Err()
}

func (repository *Repository) LoadAddons(ctx context.Context, querier database.Querier, companyId uuid.UUID, addonIds []uuid.UUID) (map[uuid.UUID]PricingAddon, error) {
	addonsById := map[uuid.UUID]PricingAddon{}
	if len(addonIds) == 0 {
		return addonsById, nil
	}

	query := `
		SELECT id, product_id, name, price
		FROM product_addons
		WHERE company_id = $1 AND is_active AND id IN (` + database.Placeholders(2, len(addonIds)) + `)
	`

	queryArguments := append([]any{companyId}, database.ToArguments(addonIds)...)
	addonRows, queryError := querier.QueryContext(ctx, query, queryArguments...)
	if queryError != nil {
		return nil, fmt.Errorf("failed to load sale add-ons: %w", queryError)
	}
	defer addonRows.Close()

	for addonRows.Next() {
		pricingAddon := PricingAddon{}
		scanError := addonRows.Scan(&pricingAddon.Id, &pricingAddon.ProductId, &pricingAddon.Name, &pricingAddon.Price)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan sale add-on: %w", scanError)
		}
		addonsById[pricingAddon.Id] = pricingAddon
	}

	return addonsById, addonRows.Err()
}

func (repository *Repository) TakeReceiptCounter(ctx context.Context, querier database.Querier, companyId uuid.UUID, shopId uuid.UUID) (*ShopCounter, error) {
	query := `
		UPDATE shops
		SET next_receipt_number = next_receipt_number + 1
		WHERE company_id = $1 AND id = $2 AND is_active
		RETURNING receipt_prefix, next_receipt_number - 1
	`

	shopCounter := ShopCounter{}
	scanError := querier.QueryRowContext(ctx, query, companyId, shopId).Scan(&shopCounter.ReceiptPrefix, &shopCounter.Number)
	if errors.Is(scanError, sql.ErrNoRows) {
		return nil, nil
	}
	if scanError != nil {
		return nil, fmt.Errorf("failed to take a receipt number: %w", scanError)
	}

	return &shopCounter, nil
}
