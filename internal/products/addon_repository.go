package products

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/google/uuid"
)

const addonColumns = `id, company_id, product_id, name, price, is_active, created_at, updated_at`

func (repository *Repository) ListAddons(ctx context.Context, querier database.Querier, companyId uuid.UUID, productId uuid.UUID) ([]Addon, error) {
	query := `
		SELECT ` + addonColumns + `
		FROM product_addons
		WHERE company_id = $1 AND product_id = $2
		ORDER BY is_active DESC, name
	`

	addonRows, queryError := querier.QueryContext(ctx, query, companyId, productId)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list add-ons: %w", queryError)
	}
	defer addonRows.Close()

	foundAddons := []Addon{}
	for addonRows.Next() {
		foundAddon, scanError := scanAddon(addonRows)
		if scanError != nil {
			return nil, scanError
		}
		foundAddons = append(foundAddons, foundAddon)
	}

	return foundAddons, addonRows.Err()
}

func (repository *Repository) FindAddon(ctx context.Context, querier database.Querier, companyId uuid.UUID, addonId uuid.UUID) (*Addon, error) {
	query := `SELECT ` + addonColumns + ` FROM product_addons WHERE company_id = $1 AND id = $2`

	foundAddon, scanError := scanAddon(querier.QueryRowContext(ctx, query, companyId, addonId))
	if scanError != nil {
		if errors.Is(scanError, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, scanError
	}

	return &foundAddon, nil
}

func (repository *Repository) InsertAddon(ctx context.Context, querier database.Querier, newAddon Addon) error {
	query := `
		INSERT INTO product_addons (id, company_id, product_id, name, price, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8)
	`

	_, insertError := querier.ExecContext(ctx, query,
		newAddon.Id,
		newAddon.CompanyId,
		newAddon.ProductId,
		newAddon.Name,
		newAddon.Price,
		newAddon.IsActive,
		newAddon.CreatedAt,
		newAddon.UpdatedAt,
	)
	if insertError != nil {
		return fmt.Errorf("failed to insert add-on: %w", insertError)
	}

	return nil
}

func (repository *Repository) UpdateAddon(ctx context.Context, querier database.Querier, changedAddon Addon) error {
	query := `
		UPDATE product_addons
		SET name = $3, price = $4, is_active = $5, updated_at = $6
		WHERE company_id = $1 AND id = $2
	`

	_, updateError := querier.ExecContext(ctx, query,
		changedAddon.CompanyId,
		changedAddon.Id,
		changedAddon.Name,
		changedAddon.Price,
		changedAddon.IsActive,
		time.Now().UTC(),
	)
	if updateError != nil {
		return fmt.Errorf("failed to update add-on: %w", updateError)
	}

	return nil
}

func (repository *Repository) DeleteAddon(ctx context.Context, querier database.Querier, companyId uuid.UUID, addonId uuid.UUID) error {
	query := `DELETE FROM product_addons WHERE company_id = $1 AND id = $2`

	_, deleteError := querier.ExecContext(ctx, query, companyId, addonId)
	if deleteError != nil {
		return fmt.Errorf("failed to delete add-on: %w", deleteError)
	}

	return nil
}

type rowScanner interface {
	Scan(destinations ...any) error
}

func scanAddon(row rowScanner) (Addon, error) {
	scannedAddon := Addon{}
	scanError := row.Scan(
		&scannedAddon.Id,
		&scannedAddon.CompanyId,
		&scannedAddon.ProductId,
		&scannedAddon.Name,
		&scannedAddon.Price,
		&scannedAddon.IsActive,
		&scannedAddon.CreatedAt,
		&scannedAddon.UpdatedAt,
	)
	if scanError != nil {
		if errors.Is(scanError, sql.ErrNoRows) {
			return scannedAddon, scanError
		}
		return scannedAddon, fmt.Errorf("failed to scan add-on: %w", scanError)
	}
	return scannedAddon, nil
}
