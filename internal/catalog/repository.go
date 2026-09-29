package catalog

import (
	"context"
	"fmt"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/google/uuid"
)

const (
	catalogColumns = `id, business_type, name, name_key, category, sub_category, unit, sku_prefix,
		default_price, metadata, created_at, updated_at`
	catalogColumnCount = 12
	upsertBatchSize    = 500
)

type Repository struct{}

func NewRepository() *Repository {
	return &Repository{}
}

func (repository *Repository) ListByBusinessType(ctx context.Context, querier database.Querier, businessType string) ([]CatalogProduct, error) {
	query := `
		SELECT ` + catalogColumns + `
		FROM catalog_products
		WHERE business_type = $1
		ORDER BY name
	`

	return repository.queryProducts(ctx, querier, query, businessType)
}

func (repository *Repository) ListForCompany(ctx context.Context, querier database.Querier, companyId uuid.UUID) ([]CatalogProduct, error) {
	query := `
		SELECT ` + catalogColumns + `
		FROM catalog_products
		WHERE business_type = (SELECT business_type FROM companies WHERE id = $1)
		ORDER BY name
	`

	return repository.queryProducts(ctx, querier, query, companyId)
}

func (repository *Repository) FindCompanyBusinessType(ctx context.Context, querier database.Querier, companyId uuid.UUID) (string, error) {
	query := `SELECT business_type FROM companies WHERE id = $1`

	businessType := ""
	scanError := querier.QueryRowContext(ctx, query, companyId).Scan(&businessType)
	if scanError != nil {
		return "", fmt.Errorf("failed to find company business type: %w", scanError)
	}

	return businessType, nil
}

func (repository *Repository) CountByBusinessType(ctx context.Context, querier database.Querier, businessType string) (int64, error) {
	query := `SELECT COUNT(*) FROM catalog_products WHERE business_type = $1`

	productCount := int64(0)
	scanError := querier.QueryRowContext(ctx, query, businessType).Scan(&productCount)
	if scanError != nil {
		return 0, fmt.Errorf("failed to count catalog products: %w", scanError)
	}

	return productCount, nil
}

func (repository *Repository) CountPerBusinessType(ctx context.Context, querier database.Querier) ([]BusinessTypeCount, error) {
	query := `
		SELECT business_type, COUNT(*)
		FROM catalog_products
		GROUP BY business_type
		ORDER BY business_type
	`

	countRows, queryError := querier.QueryContext(ctx, query)
	if queryError != nil {
		return nil, fmt.Errorf("failed to count catalog products per business type: %w", queryError)
	}
	defer countRows.Close()

	counts := []BusinessTypeCount{}
	for countRows.Next() {
		businessTypeCount := BusinessTypeCount{}
		scanError := countRows.Scan(&businessTypeCount.BusinessType, &businessTypeCount.Count)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan catalog count: %w", scanError)
		}
		counts = append(counts, businessTypeCount)
	}

	return counts, countRows.Err()
}

func (repository *Repository) ListNameKeys(ctx context.Context, querier database.Querier, businessType string) (map[string]bool, error) {
	query := `SELECT name_key FROM catalog_products WHERE business_type = $1`

	nameKeyRows, queryError := querier.QueryContext(ctx, query, businessType)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list catalog names: %w", queryError)
	}
	defer nameKeyRows.Close()

	existingNameKeys := map[string]bool{}
	for nameKeyRows.Next() {
		nameKey := ""
		scanError := nameKeyRows.Scan(&nameKey)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan catalog name: %w", scanError)
		}
		existingNameKeys[nameKey] = true
	}

	return existingNameKeys, nameKeyRows.Err()
}

func (repository *Repository) UpsertAll(ctx context.Context, querier database.Querier, catalogProducts []CatalogProduct) error {
	for batchStart := 0; batchStart < len(catalogProducts); batchStart += upsertBatchSize {
		batchEnd := min(batchStart+upsertBatchSize, len(catalogProducts))
		upsertError := repository.upsertBatch(ctx, querier, catalogProducts[batchStart:batchEnd])
		if upsertError != nil {
			return upsertError
		}
	}
	return nil
}

func (repository *Repository) upsertBatch(ctx context.Context, querier database.Querier, catalogProducts []CatalogProduct) error {
	valueGroups := ""
	arguments := make([]any, 0, len(catalogProducts)*catalogColumnCount)
	for productIndex, catalogProduct := range catalogProducts {
		if productIndex > 0 {
			valueGroups += ", "
		}
		valueGroups += "(" + database.Placeholders(productIndex*catalogColumnCount+1, catalogColumnCount) + ")"
		arguments = append(arguments,
			catalogProduct.Id,
			catalogProduct.BusinessType,
			catalogProduct.Name,
			catalogProduct.NameKey,
			catalogProduct.Category,
			catalogProduct.SubCategory,
			catalogProduct.Unit,
			catalogProduct.SkuPrefix,
			catalogProduct.DefaultPrice,
			string(catalogProduct.Metadata),
			catalogProduct.CreatedAt,
			catalogProduct.UpdatedAt,
		)
	}

	query := `
		INSERT INTO catalog_products (` + catalogColumns + `)
		VALUES ` + valueGroups + `
		ON CONFLICT (business_type, name_key) DO UPDATE SET
			name = excluded.name,
			category = excluded.category,
			sub_category = excluded.sub_category,
			unit = excluded.unit,
			sku_prefix = excluded.sku_prefix,
			default_price = excluded.default_price,
			metadata = excluded.metadata,
			updated_at = excluded.updated_at
	`

	_, upsertError := querier.ExecContext(ctx, query, arguments...)
	if upsertError != nil {
		return fmt.Errorf("failed to save catalog products: %w", upsertError)
	}

	return nil
}

func (repository *Repository) DeleteByBusinessType(ctx context.Context, querier database.Querier, businessType string) (int64, error) {
	query := `DELETE FROM catalog_products WHERE business_type = $1`

	deleteResult, deleteError := querier.ExecContext(ctx, query, businessType)
	if deleteError != nil {
		return 0, fmt.Errorf("failed to clear catalog products: %w", deleteError)
	}

	removedCount, countError := deleteResult.RowsAffected()
	if countError != nil {
		return 0, fmt.Errorf("failed to count cleared catalog products: %w", countError)
	}

	return removedCount, nil
}

func (repository *Repository) queryProducts(ctx context.Context, querier database.Querier, query string, argument any) ([]CatalogProduct, error) {
	productRows, queryError := querier.QueryContext(ctx, query, argument)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list catalog products: %w", queryError)
	}
	defer productRows.Close()

	foundProducts := []CatalogProduct{}
	for productRows.Next() {
		foundProduct := CatalogProduct{}
		metadataText := ""
		scanError := productRows.Scan(
			&foundProduct.Id,
			&foundProduct.BusinessType,
			&foundProduct.Name,
			&foundProduct.NameKey,
			&foundProduct.Category,
			&foundProduct.SubCategory,
			&foundProduct.Unit,
			&foundProduct.SkuPrefix,
			&foundProduct.DefaultPrice,
			&metadataText,
			&foundProduct.CreatedAt,
			&foundProduct.UpdatedAt,
		)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan catalog product: %w", scanError)
		}
		foundProduct.Metadata = []byte(metadataText)
		foundProducts = append(foundProducts, foundProduct)
	}

	return foundProducts, productRows.Err()
}
