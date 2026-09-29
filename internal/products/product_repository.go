package products

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

type Repository struct{}

func NewRepository() *Repository {
	return &Repository{}
}

const productColumns = `
	p.id, p.company_id, p.parent_id, p.sku, p.name, p.variant_label, p.price, p.cost_price,
	p.wholesale_price, p.wholesale_min, p.category, p.unit, p.pieces_per_unit, p.image_key,
	p.metadata, p.is_active, p.created_at, p.updated_at, ss.quantity, ss.min_stock,
	(SELECT COUNT(*) FROM products v WHERE v.company_id = p.company_id AND v.parent_id = p.id AND v.is_active)
`

const productFilter = `
	p.company_id = $1
	AND ($2 = '' OR lower(p.name) LIKE $3 OR lower(p.sku) LIKE $3 OR EXISTS (
		SELECT 1 FROM barcodes b WHERE b.company_id = p.company_id AND b.product_id = p.id AND b.code = $2
	))
	AND ($4 = '' OR p.category = $4)
	AND (p.is_active OR $5)
`

func (repository *Repository) Count(ctx context.Context, querier database.Querier, companyId uuid.UUID, filter ListFilter) (int64, error) {
	query := `SELECT COUNT(*) FROM products p WHERE p.parent_id IS NULL AND ` + productFilter

	productCount := int64(0)
	scanError := querier.QueryRowContext(ctx, query, filterArguments(companyId, filter)...).Scan(&productCount)
	if scanError != nil {
		return 0, fmt.Errorf("failed to count products: %w", scanError)
	}

	return productCount, nil
}

func (repository *Repository) List(ctx context.Context, querier database.Querier, companyId uuid.UUID, shopId *uuid.UUID, filter ListFilter, limit int, offset int) ([]Product, error) {
	query := `
		SELECT ` + productColumns + `
		FROM products p
		LEFT JOIN shop_stock ss ON ss.company_id = p.company_id AND ss.product_id = p.id AND ss.shop_id = $6
		WHERE p.parent_id IS NULL AND ` + productFilter + `
		ORDER BY p.is_active DESC, p.name, p.id
		LIMIT $7 OFFSET $8
	`

	queryArguments := append(filterArguments(companyId, filter), shopId, limit, offset)
	return repository.queryProducts(ctx, querier, query, queryArguments...)
}

func (repository *Repository) ListVariants(ctx context.Context, querier database.Querier, companyId uuid.UUID, shopId *uuid.UUID, parentId uuid.UUID) ([]Product, error) {
	query := `
		SELECT ` + productColumns + `
		FROM products p
		LEFT JOIN shop_stock ss ON ss.company_id = p.company_id AND ss.product_id = p.id AND ss.shop_id = $2
		WHERE p.company_id = $1 AND p.parent_id = $3
		ORDER BY p.is_active DESC, p.variant_label, p.id
	`

	return repository.queryProducts(ctx, querier, query, companyId, shopId, parentId)
}

func (repository *Repository) Find(ctx context.Context, querier database.Querier, companyId uuid.UUID, shopId *uuid.UUID, productId uuid.UUID) (*Product, error) {
	query := `
		SELECT ` + productColumns + `
		FROM products p
		LEFT JOIN shop_stock ss ON ss.company_id = p.company_id AND ss.product_id = p.id AND ss.shop_id = $2
		WHERE p.company_id = $1 AND p.id = $3
	`

	foundProducts, queryError := repository.queryProducts(ctx, querier, query, companyId, shopId, productId)
	if queryError != nil {
		return nil, queryError
	}
	if len(foundProducts) == 0 {
		return nil, nil
	}
	return &foundProducts[0], nil
}

func (repository *Repository) Insert(ctx context.Context, querier database.Querier, newProduct Product) error {
	query := `
		INSERT INTO products (id, company_id, parent_id, sku, name, variant_label, price, cost_price,
		                      wholesale_price, wholesale_min, category, unit, pieces_per_unit, image_key,
		                      metadata, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17, $18)
	`

	_, insertError := querier.ExecContext(ctx, query,
		newProduct.Id,
		newProduct.CompanyId,
		newProduct.ParentId,
		newProduct.Sku,
		newProduct.Name,
		newProduct.VariantLabel,
		newProduct.Price,
		newProduct.CostPrice,
		newProduct.WholesalePrice,
		newProduct.WholesaleMin,
		newProduct.Category,
		newProduct.Unit,
		newProduct.PiecesPerUnit,
		newProduct.ImageKey,
		string(newProduct.Metadata),
		newProduct.IsActive,
		newProduct.CreatedAt,
		newProduct.UpdatedAt,
	)
	if insertError != nil {
		return fmt.Errorf("failed to insert product: %w", insertError)
	}

	return nil
}

func (repository *Repository) Update(ctx context.Context, querier database.Querier, changedProduct Product) error {
	query := `
		UPDATE products
		SET sku = $3, name = $4, variant_label = $5, price = $6, cost_price = $7, wholesale_price = $8,
		    wholesale_min = $9, category = $10, unit = $11, pieces_per_unit = $12, metadata = $13,
		    is_active = $14, updated_at = $15
		WHERE company_id = $1 AND id = $2
	`

	_, updateError := querier.ExecContext(ctx, query,
		changedProduct.CompanyId,
		changedProduct.Id,
		changedProduct.Sku,
		changedProduct.Name,
		changedProduct.VariantLabel,
		changedProduct.Price,
		changedProduct.CostPrice,
		changedProduct.WholesalePrice,
		changedProduct.WholesaleMin,
		changedProduct.Category,
		changedProduct.Unit,
		changedProduct.PiecesPerUnit,
		string(changedProduct.Metadata),
		changedProduct.IsActive,
		time.Now().UTC(),
	)
	if updateError != nil {
		return fmt.Errorf("failed to update product: %w", updateError)
	}

	return nil
}

func (repository *Repository) SetActive(ctx context.Context, querier database.Querier, companyId uuid.UUID, productId uuid.UUID, isActive bool) error {
	query := `UPDATE products SET is_active = $3, updated_at = $4 WHERE company_id = $1 AND (id = $2 OR parent_id = $2)`

	_, updateError := querier.ExecContext(ctx, query, companyId, productId, isActive, time.Now().UTC())
	if updateError != nil {
		return fmt.Errorf("failed to change product status: %w", updateError)
	}

	return nil
}

func (repository *Repository) SetImageKey(ctx context.Context, querier database.Querier, companyId uuid.UUID, productId uuid.UUID, imageKey string) error {
	query := `UPDATE products SET image_key = $3, updated_at = $4 WHERE company_id = $1 AND id = $2`

	_, updateError := querier.ExecContext(ctx, query, companyId, productId, imageKey, time.Now().UTC())
	if updateError != nil {
		return fmt.Errorf("failed to set product image: %w", updateError)
	}

	return nil
}

func (repository *Repository) ListBarcodes(ctx context.Context, querier database.Querier, companyId uuid.UUID, productIds []uuid.UUID) (map[uuid.UUID][]Barcode, error) {
	barcodesByProduct := map[uuid.UUID][]Barcode{}
	hasNoProducts := len(productIds) == 0
	if hasNoProducts {
		return barcodesByProduct, nil
	}

	query := `
		SELECT id, company_id, product_id, code, pack_size, created_at
		FROM barcodes
		WHERE company_id = $1 AND product_id IN (` + database.Placeholders(2, len(productIds)) + `)
		ORDER BY pack_size, code
	`

	queryArguments := append([]any{companyId}, database.ToArguments(productIds)...)
	barcodeRows, queryError := querier.QueryContext(ctx, query, queryArguments...)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list barcodes: %w", queryError)
	}
	defer barcodeRows.Close()

	for barcodeRows.Next() {
		foundBarcode := Barcode{}
		scanError := barcodeRows.Scan(
			&foundBarcode.Id,
			&foundBarcode.CompanyId,
			&foundBarcode.ProductId,
			&foundBarcode.Code,
			&foundBarcode.PackSize,
			&foundBarcode.CreatedAt,
		)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan barcode: %w", scanError)
		}
		barcodesByProduct[foundBarcode.ProductId] = append(barcodesByProduct[foundBarcode.ProductId], foundBarcode)
	}

	return barcodesByProduct, barcodeRows.Err()
}

func (repository *Repository) ReplaceBarcodes(ctx context.Context, querier database.Querier, companyId uuid.UUID, productId uuid.UUID, newBarcodes []Barcode) error {
	deleteQuery := `DELETE FROM barcodes WHERE company_id = $1 AND product_id = $2`
	_, deleteError := querier.ExecContext(ctx, deleteQuery, companyId, productId)
	if deleteError != nil {
		return fmt.Errorf("failed to clear barcodes: %w", deleteError)
	}

	for _, newBarcode := range newBarcodes {
		insertQuery := `INSERT INTO barcodes (id, company_id, product_id, code, pack_size, created_at) VALUES ($1, $2, $3, $4, $5, $6)`
		_, insertError := querier.ExecContext(ctx, insertQuery,
			newBarcode.Id,
			newBarcode.CompanyId,
			newBarcode.ProductId,
			newBarcode.Code,
			newBarcode.PackSize,
			newBarcode.CreatedAt,
		)
		if insertError != nil {
			return fmt.Errorf("failed to insert barcode: %w", insertError)
		}
	}

	return nil
}

func (repository *Repository) InsertPriceChange(ctx context.Context, querier database.Querier, priceChange PriceChange) error {
	query := `
		INSERT INTO price_history (id, company_id, product_id, old_price, new_price, changed_by, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	_, insertError := querier.ExecContext(ctx, query,
		priceChange.Id,
		priceChange.CompanyId,
		priceChange.ProductId,
		priceChange.OldPrice,
		priceChange.NewPrice,
		priceChange.ChangedBy,
		priceChange.CreatedAt,
	)
	if insertError != nil {
		return fmt.Errorf("failed to record price change: %w", insertError)
	}

	return nil
}

func (repository *Repository) ListCategories(ctx context.Context, querier database.Querier, companyId uuid.UUID) ([]string, error) {
	query := `
		SELECT DISTINCT category
		FROM products
		WHERE company_id = $1 AND category IS NOT NULL AND is_active
		ORDER BY category
	`

	categoryRows, queryError := querier.QueryContext(ctx, query, companyId)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list categories: %w", queryError)
	}
	defer categoryRows.Close()

	categories := []string{}
	for categoryRows.Next() {
		category := ""
		scanError := categoryRows.Scan(&category)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan category: %w", scanError)
		}
		categories = append(categories, category)
	}

	return categories, categoryRows.Err()
}

func (repository *Repository) FindCurrencyDecimals(ctx context.Context, querier database.Querier, companyId uuid.UUID) (int, error) {
	query := `SELECT currency_decimals FROM companies WHERE id = $1`

	currencyDecimals := 0
	scanError := querier.QueryRowContext(ctx, query, companyId).Scan(&currencyDecimals)
	if scanError != nil {
		return 0, fmt.Errorf("failed to read currency decimals: %w", scanError)
	}

	return currencyDecimals, nil
}

func (repository *Repository) FindTakenSkus(ctx context.Context, querier database.Querier, companyId uuid.UUID, candidateSkus []string) (map[string]bool, error) {
	return repository.findTakenValues(ctx, querier, companyId, "products", "sku", candidateSkus)
}

func (repository *Repository) FindTakenBarcodes(ctx context.Context, querier database.Querier, companyId uuid.UUID, candidateCodes []string) (map[string]bool, error) {
	return repository.findTakenValues(ctx, querier, companyId, "barcodes", "code", candidateCodes)
}

func (repository *Repository) findTakenValues(ctx context.Context, querier database.Querier, companyId uuid.UUID, tableName string, columnName string, candidateValues []string) (map[string]bool, error) {
	takenValues := map[string]bool{}
	const chunkSize = 500

	for chunkStart := 0; chunkStart < len(candidateValues); chunkStart += chunkSize {
		chunkEnd := min(chunkStart+chunkSize, len(candidateValues))
		valueChunk := candidateValues[chunkStart:chunkEnd]

		query := `SELECT ` + columnName + ` FROM ` + tableName + ` WHERE company_id = $1 AND ` + columnName + ` IN (` + database.Placeholders(2, len(valueChunk)) + `)`
		queryArguments := append([]any{companyId}, database.ToArguments(valueChunk)...)

		takenRows, queryError := querier.QueryContext(ctx, query, queryArguments...)
		if queryError != nil {
			return nil, fmt.Errorf("failed to check existing %s: %w", columnName, queryError)
		}
		for takenRows.Next() {
			takenValue := ""
			scanError := takenRows.Scan(&takenValue)
			if scanError != nil {
				takenRows.Close()
				return nil, fmt.Errorf("failed to scan existing %s: %w", columnName, scanError)
			}
			takenValues[takenValue] = true
		}
		takenRows.Close()
	}

	return takenValues, nil
}

func (repository *Repository) queryProducts(ctx context.Context, querier database.Querier, query string, queryArguments ...any) ([]Product, error) {
	productRows, queryError := querier.QueryContext(ctx, query, queryArguments...)
	if queryError != nil {
		return nil, fmt.Errorf("failed to query products: %w", queryError)
	}
	defer productRows.Close()

	foundProducts := []Product{}
	for productRows.Next() {
		foundProduct := Product{}
		metadataText := sql.RawBytes{}
		scanError := productRows.Scan(
			&foundProduct.Id,
			&foundProduct.CompanyId,
			&foundProduct.ParentId,
			&foundProduct.Sku,
			&foundProduct.Name,
			&foundProduct.VariantLabel,
			&foundProduct.Price,
			&foundProduct.CostPrice,
			&foundProduct.WholesalePrice,
			&foundProduct.WholesaleMin,
			&foundProduct.Category,
			&foundProduct.Unit,
			&foundProduct.PiecesPerUnit,
			&foundProduct.ImageKey,
			&metadataText,
			&foundProduct.IsActive,
			&foundProduct.CreatedAt,
			&foundProduct.UpdatedAt,
			&foundProduct.Quantity,
			&foundProduct.MinimumStock,
			&foundProduct.VariantCount,
		)
		if scanError != nil {
			if errors.Is(scanError, sql.ErrNoRows) {
				return foundProducts, nil
			}
			return nil, fmt.Errorf("failed to scan product: %w", scanError)
		}
		foundProduct.Metadata = append([]byte(nil), metadataText...)
		foundProducts = append(foundProducts, foundProduct)
	}

	return foundProducts, productRows.Err()
}

func filterArguments(companyId uuid.UUID, filter ListFilter) []any {
	searchText := strings.TrimSpace(filter.SearchText)
	likePattern := "%" + strings.ToLower(searchText) + "%"
	return []any{companyId, searchText, likePattern, strings.TrimSpace(filter.Category), filter.IncludeArchived}
}
