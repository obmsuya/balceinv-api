package stock

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/google/uuid"
)

type Repository struct{}

func NewRepository() *Repository {
	return &Repository{}
}

func (repository *Repository) EnsureShopStock(ctx context.Context, querier database.Querier, companyId uuid.UUID, shopId uuid.UUID, productId uuid.UUID, minimumStock int) error {
	query := `
		INSERT INTO shop_stock (company_id, shop_id, product_id, quantity, min_stock, updated_at)
		VALUES ($1, $2, $3, 0, $4, $5)
		ON CONFLICT (shop_id, product_id) DO NOTHING
	`

	_, insertError := querier.ExecContext(ctx, query, companyId, shopId, productId, minimumStock, time.Now().UTC())
	if insertError != nil {
		return fmt.Errorf("failed to prepare shop stock: %w", insertError)
	}

	return nil
}

func (repository *Repository) SetMinimumStock(ctx context.Context, querier database.Querier, companyId uuid.UUID, shopId uuid.UUID, productId uuid.UUID, minimumStock int) error {
	query := `
		UPDATE shop_stock
		SET min_stock = $4, updated_at = $5
		WHERE company_id = $1 AND shop_id = $2 AND product_id = $3
	`

	_, updateError := querier.ExecContext(ctx, query, companyId, shopId, productId, minimumStock, time.Now().UTC())
	if updateError != nil {
		return fmt.Errorf("failed to set minimum stock: %w", updateError)
	}

	return nil
}

func (repository *Repository) ChangeQuantity(ctx context.Context, querier database.Querier, companyId uuid.UUID, shopId uuid.UUID, productId uuid.UUID, change int) (int, int, bool, error) {
	query := `
		UPDATE shop_stock
		SET quantity = quantity + $4, updated_at = $5
		WHERE company_id = $1 AND shop_id = $2 AND product_id = $3 AND quantity + $4 >= 0
		RETURNING quantity, min_stock
	`

	quantityAfter := 0
	minimumStock := 0
	scanError := querier.QueryRowContext(ctx, query, companyId, shopId, productId, change, time.Now().UTC()).Scan(&quantityAfter, &minimumStock)
	if errors.Is(scanError, sql.ErrNoRows) {
		return 0, 0, false, nil
	}
	if scanError != nil {
		return 0, 0, false, fmt.Errorf("failed to change stock quantity: %w", scanError)
	}

	return quantityAfter, minimumStock, true, nil
}

func (repository *Repository) InsertMovement(ctx context.Context, querier database.Querier, movement Movement) error {
	query := `
		INSERT INTO stock_movements (id, company_id, shop_id, product_id, change, quantity_after, reason, reference, user_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`

	_, insertError := querier.ExecContext(ctx, query,
		movement.Id,
		movement.CompanyId,
		movement.ShopId,
		movement.ProductId,
		movement.Change,
		movement.QuantityAfter,
		movement.Reason,
		movement.Reference,
		movement.UserId,
		movement.CreatedAt,
	)
	if insertError != nil {
		return fmt.Errorf("failed to record stock movement: %w", insertError)
	}

	return nil
}

func (repository *Repository) MovementIdsOf(ctx context.Context, querier database.Querier, companyId uuid.UUID, productIds []uuid.UUID) ([]uuid.UUID, error) {
	query := `SELECT id FROM stock_movements WHERE company_id = $1 AND product_id IN (` + database.Placeholders(2, len(productIds)) + `) ORDER BY created_at, id`
	queryArguments := append([]any{companyId}, database.ToArguments(productIds)...)

	movementRows, queryError := querier.QueryContext(ctx, query, queryArguments...)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list the product's stock movements: %w", queryError)
	}
	defer movementRows.Close()

	movementIds := []uuid.UUID{}
	for movementRows.Next() {
		movementId := uuid.UUID{}
		scanError := movementRows.Scan(&movementId)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan a stock movement: %w", scanError)
		}
		movementIds = append(movementIds, movementId)
	}
	return movementIds, movementRows.Err()
}

func (repository *Repository) DeleteProductStock(ctx context.Context, querier database.Querier, companyId uuid.UUID, productIds []uuid.UUID) error {
	placeholders := database.Placeholders(2, len(productIds))
	queryArguments := append([]any{companyId}, database.ToArguments(productIds)...)
	for _, table := range []string{"stock_movements", "shop_stock"} {
		_, deleteError := querier.ExecContext(ctx, `DELETE FROM `+table+` WHERE company_id = $1 AND product_id IN (`+placeholders+`)`, queryArguments...)
		if deleteError != nil {
			return fmt.Errorf("failed to delete the product's %s: %w", table, deleteError)
		}
	}
	return nil
}
