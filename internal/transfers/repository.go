package transfers

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/google/uuid"
)

const transferSummaryColumns = `
	t.id, t.from_shop_id, fs.name, t.to_shop_id, ts.name, t.note, u.name, t.created_at,
	(SELECT COUNT(*) FROM stock_transfer_items i WHERE i.company_id = t.company_id AND i.transfer_id = t.id),
	(SELECT COALESCE(SUM(i.quantity), 0) FROM stock_transfer_items i WHERE i.company_id = t.company_id AND i.transfer_id = t.id)
`

const transferJoins = `
	FROM stock_transfers t
	JOIN shops fs ON fs.company_id = t.company_id AND fs.id = t.from_shop_id
	JOIN shops ts ON ts.company_id = t.company_id AND ts.id = t.to_shop_id
	LEFT JOIN users u ON u.company_id = t.company_id AND u.id = t.user_id
`

type Repository struct{}

func NewRepository() *Repository {
	return &Repository{}
}

func (repository *Repository) FindOpenShop(ctx context.Context, querier database.Querier, companyId uuid.UUID, shopId uuid.UUID) (bool, error) {
	query := `SELECT COUNT(*) FROM shops WHERE company_id = $1 AND id = $2 AND is_active`

	matchCount := 0
	scanError := querier.QueryRowContext(ctx, query, companyId, shopId).Scan(&matchCount)
	if scanError != nil {
		return false, fmt.Errorf("failed to find shop: %w", scanError)
	}

	return matchCount > 0, nil
}

func (repository *Repository) IsAssignedToShop(ctx context.Context, querier database.Querier, companyId uuid.UUID, userId uuid.UUID, shopId uuid.UUID) (bool, error) {
	query := `SELECT COUNT(*) FROM user_shops WHERE company_id = $1 AND user_id = $2 AND shop_id = $3`

	matchCount := 0
	scanError := querier.QueryRowContext(ctx, query, companyId, userId, shopId).Scan(&matchCount)
	if scanError != nil {
		return false, fmt.Errorf("failed to check shop assignment: %w", scanError)
	}

	return matchCount > 0, nil
}

func (repository *Repository) CountActiveProducts(ctx context.Context, querier database.Querier, companyId uuid.UUID, productIds []uuid.UUID) (int, error) {
	query := `SELECT COUNT(*) FROM products WHERE company_id = $1 AND is_active AND id IN (` + database.Placeholders(2, len(productIds)) + `)`

	queryArguments := append([]any{companyId}, database.ToArguments(productIds)...)
	matchCount := 0
	scanError := querier.QueryRowContext(ctx, query, queryArguments...).Scan(&matchCount)
	if scanError != nil {
		return 0, fmt.Errorf("failed to check transfer products: %w", scanError)
	}

	return matchCount, nil
}

func (repository *Repository) Insert(ctx context.Context, querier database.Querier, newTransfer Transfer, newItems []TransferItem) error {
	transferQuery := `
		INSERT INTO stock_transfers (id, company_id, from_shop_id, to_shop_id, note, user_id, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`

	_, transferError := querier.ExecContext(ctx, transferQuery,
		newTransfer.Id,
		newTransfer.CompanyId,
		newTransfer.FromShopId,
		newTransfer.ToShopId,
		newTransfer.Note,
		newTransfer.UserId,
		newTransfer.CreatedAt,
	)
	if transferError != nil {
		return fmt.Errorf("failed to insert transfer: %w", transferError)
	}

	valueGroups := ""
	itemArguments := make([]any, 0, len(newItems)*4)
	for itemIndex, newItem := range newItems {
		if itemIndex > 0 {
			valueGroups += ", "
		}
		valueGroups += "(" + database.Placeholders(itemIndex*4+1, 4) + ")"
		itemArguments = append(itemArguments, newItem.CompanyId, newItem.TransferId, newItem.ProductId, newItem.Quantity)
	}

	itemQuery := `INSERT INTO stock_transfer_items (company_id, transfer_id, product_id, quantity) VALUES ` + valueGroups
	_, itemError := querier.ExecContext(ctx, itemQuery, itemArguments...)
	if itemError != nil {
		return fmt.Errorf("failed to insert transfer items: %w", itemError)
	}

	return nil
}

func (repository *Repository) CountForShop(ctx context.Context, querier database.Querier, companyId uuid.UUID, shopId uuid.UUID) (int64, error) {
	query := `SELECT COUNT(*) FROM stock_transfers WHERE company_id = $1 AND (from_shop_id = $2 OR to_shop_id = $2)`

	transferCount := int64(0)
	scanError := querier.QueryRowContext(ctx, query, companyId, shopId).Scan(&transferCount)
	if scanError != nil {
		return 0, fmt.Errorf("failed to count transfers: %w", scanError)
	}

	return transferCount, nil
}

func (repository *Repository) ListForShop(ctx context.Context, querier database.Querier, companyId uuid.UUID, shopId uuid.UUID, limit int, offset int) ([]TransferView, error) {
	query := `
		SELECT ` + transferSummaryColumns + transferJoins + `
		WHERE t.company_id = $1 AND (t.from_shop_id = $2 OR t.to_shop_id = $2)
		ORDER BY t.created_at DESC, t.id DESC
		LIMIT $3 OFFSET $4
	`

	transferRows, queryError := querier.QueryContext(ctx, query, companyId, shopId, limit, offset)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list transfers: %w", queryError)
	}
	defer transferRows.Close()

	transferViews := []TransferView{}
	for transferRows.Next() {
		transferView, scanError := scanTransfer(transferRows)
		if scanError != nil {
			return nil, scanError
		}
		transferViews = append(transferViews, transferView)
	}

	return transferViews, transferRows.Err()
}

func (repository *Repository) Find(ctx context.Context, querier database.Querier, companyId uuid.UUID, transferId uuid.UUID) (*TransferView, error) {
	query := `SELECT ` + transferSummaryColumns + transferJoins + ` WHERE t.company_id = $1 AND t.id = $2`

	transferView, scanError := scanTransfer(querier.QueryRowContext(ctx, query, companyId, transferId))
	if errors.Is(scanError, sql.ErrNoRows) {
		return nil, nil
	}
	if scanError != nil {
		return nil, scanError
	}

	return &transferView, nil
}

func (repository *Repository) ListItems(ctx context.Context, querier database.Querier, companyId uuid.UUID, transferId uuid.UUID) ([]TransferItemView, error) {
	query := `
		SELECT i.product_id, p.name, p.variant_label, p.sku, p.unit, i.quantity
		FROM stock_transfer_items i
		JOIN products p ON p.company_id = i.company_id AND p.id = i.product_id
		WHERE i.company_id = $1 AND i.transfer_id = $2
		ORDER BY p.name, p.variant_label
	`

	itemRows, queryError := querier.QueryContext(ctx, query, companyId, transferId)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list transfer items: %w", queryError)
	}
	defer itemRows.Close()

	itemViews := []TransferItemView{}
	for itemRows.Next() {
		itemView := TransferItemView{}
		scanError := itemRows.Scan(
			&itemView.ProductId,
			&itemView.ProductName,
			&itemView.VariantLabel,
			&itemView.Sku,
			&itemView.Unit,
			&itemView.Quantity,
		)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan transfer item: %w", scanError)
		}
		itemViews = append(itemViews, itemView)
	}

	return itemViews, itemRows.Err()
}

type rowScanner interface {
	Scan(destinations ...any) error
}

func scanTransfer(row rowScanner) (TransferView, error) {
	transferView := TransferView{}
	totalUnits := int64(0)
	scanError := row.Scan(
		&transferView.Id,
		&transferView.FromShopId,
		&transferView.FromShopName,
		&transferView.ToShopId,
		&transferView.ToShopName,
		&transferView.Note,
		&transferView.UserName,
		&transferView.CreatedAt,
		&transferView.ItemCount,
		&totalUnits,
	)
	if errors.Is(scanError, sql.ErrNoRows) {
		return transferView, scanError
	}
	if scanError != nil {
		return transferView, fmt.Errorf("failed to scan transfer: %w", scanError)
	}
	transferView.TotalUnits = int(totalUnits)
	return transferView, nil
}
