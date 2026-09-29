package shops

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/google/uuid"
)

const shopColumns = `id, company_id, name, address, phone, receipt_prefix, is_active, created_at, updated_at`

type Repository struct{}

func NewRepository() *Repository {
	return &Repository{}
}

func (repository *Repository) Count(ctx context.Context, querier database.Querier, companyId uuid.UUID) (int64, error) {
	query := `SELECT COUNT(*) FROM shops WHERE company_id = $1`

	shopCount := int64(0)
	scanError := querier.QueryRowContext(ctx, query, companyId).Scan(&shopCount)
	if scanError != nil {
		return 0, fmt.Errorf("failed to count shops: %w", scanError)
	}

	return shopCount, nil
}

func (repository *Repository) CountActive(ctx context.Context, querier database.Querier, companyId uuid.UUID) (int64, error) {
	query := `SELECT COUNT(*) FROM shops WHERE company_id = $1 AND is_active`

	activeCount := int64(0)
	scanError := querier.QueryRowContext(ctx, query, companyId).Scan(&activeCount)
	if scanError != nil {
		return 0, fmt.Errorf("failed to count active shops: %w", scanError)
	}

	return activeCount, nil
}

func (repository *Repository) List(ctx context.Context, querier database.Querier, companyId uuid.UUID, limit int, offset int) ([]Shop, error) {
	query := `
		SELECT ` + shopColumns + `
		FROM shops
		WHERE company_id = $1
		ORDER BY is_active DESC, name
		LIMIT $2 OFFSET $3
	`

	shopRows, queryError := querier.QueryContext(ctx, query, companyId, limit, offset)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list shops: %w", queryError)
	}
	defer shopRows.Close()

	foundShops := []Shop{}
	for shopRows.Next() {
		foundShop, scanError := scanShop(shopRows)
		if scanError != nil {
			return nil, scanError
		}
		foundShops = append(foundShops, foundShop)
	}

	return foundShops, shopRows.Err()
}

func (repository *Repository) Find(ctx context.Context, querier database.Querier, companyId uuid.UUID, shopId uuid.UUID) (*Shop, error) {
	query := `SELECT ` + shopColumns + ` FROM shops WHERE company_id = $1 AND id = $2`

	foundShop, scanError := scanShop(querier.QueryRowContext(ctx, query, companyId, shopId))
	if errors.Is(scanError, sql.ErrNoRows) {
		return nil, nil
	}
	if scanError != nil {
		return nil, scanError
	}

	return &foundShop, nil
}

func (repository *Repository) Insert(ctx context.Context, querier database.Querier, newShop Shop) error {
	query := `
		INSERT INTO shops (id, company_id, name, address, phone, receipt_prefix, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
	`

	_, insertError := querier.ExecContext(ctx, query,
		newShop.Id,
		newShop.CompanyId,
		newShop.Name,
		newShop.Address,
		newShop.Phone,
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

func (repository *Repository) Update(ctx context.Context, querier database.Querier, changedShop Shop) error {
	query := `
		UPDATE shops
		SET name = $3, address = $4, phone = $5, receipt_prefix = $6, is_active = $7, updated_at = $8
		WHERE company_id = $1 AND id = $2
	`

	_, updateError := querier.ExecContext(ctx, query,
		changedShop.CompanyId,
		changedShop.Id,
		changedShop.Name,
		changedShop.Address,
		changedShop.Phone,
		changedShop.ReceiptPrefix,
		changedShop.IsActive,
		changedShop.UpdatedAt,
	)
	if updateError != nil {
		return fmt.Errorf("failed to update shop: %w", updateError)
	}

	return nil
}

type rowScanner interface {
	Scan(destinations ...any) error
}

func scanShop(row rowScanner) (Shop, error) {
	scannedShop := Shop{}
	scanError := row.Scan(
		&scannedShop.Id,
		&scannedShop.CompanyId,
		&scannedShop.Name,
		&scannedShop.Address,
		&scannedShop.Phone,
		&scannedShop.ReceiptPrefix,
		&scannedShop.IsActive,
		&scannedShop.CreatedAt,
		&scannedShop.UpdatedAt,
	)
	if errors.Is(scanError, sql.ErrNoRows) {
		return scannedShop, scanError
	}
	if scanError != nil {
		return scannedShop, fmt.Errorf("failed to scan shop: %w", scanError)
	}
	return scannedShop, nil
}
