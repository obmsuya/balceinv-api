package shops

import (
	"context"
	"fmt"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/google/uuid"
)

func (repository *Repository) IsUsed(ctx context.Context, querier database.Querier, companyId uuid.UUID, shopId uuid.UUID) (bool, error) {
	query := `
		SELECT CASE WHEN
			EXISTS (SELECT 1 FROM sales WHERE company_id = $1 AND shop_id = $2)
			OR EXISTS (SELECT 1 FROM stock_movements WHERE company_id = $1 AND shop_id = $2)
			OR EXISTS (SELECT 1 FROM stock_transfers WHERE company_id = $1 AND (from_shop_id = $2 OR to_shop_id = $2))
			OR EXISTS (SELECT 1 FROM purchases WHERE company_id = $1 AND shop_id = $2)
			OR EXISTS (SELECT 1 FROM purchase_orders WHERE company_id = $1 AND shop_id = $2)
			OR EXISTS (SELECT 1 FROM supplier_payments WHERE company_id = $1 AND shop_id = $2)
			OR EXISTS (SELECT 1 FROM supplier_returns WHERE company_id = $1 AND shop_id = $2)
			OR EXISTS (SELECT 1 FROM customer_orders WHERE company_id = $1 AND shop_id = $2)
			OR EXISTS (SELECT 1 FROM customer_payments WHERE company_id = $1 AND shop_id = $2)
			OR EXISTS (SELECT 1 FROM journal_entries WHERE company_id = $1 AND shop_id = $2)
			OR EXISTS (SELECT 1 FROM journal_lines WHERE company_id = $1 AND shop_id = $2)
		THEN 1 ELSE 0 END
	`

	usedFlag := 0
	scanError := querier.QueryRowContext(ctx, query, companyId, shopId).Scan(&usedFlag)
	if scanError != nil {
		return false, fmt.Errorf("failed to check whether the shop was used: %w", scanError)
	}
	return usedFlag == 1, nil
}

func (repository *Repository) HasStaffOnlyHere(ctx context.Context, querier database.Querier, companyId uuid.UUID, shopId uuid.UUID) (bool, error) {
	query := `
		SELECT COUNT(*) FROM user_shops us
		WHERE us.company_id = $1 AND us.shop_id = $2
		  AND NOT EXISTS (SELECT 1 FROM user_shops other WHERE other.company_id = us.company_id AND other.user_id = us.user_id AND other.shop_id <> us.shop_id)
	`

	staffCount := 0
	scanError := querier.QueryRowContext(ctx, query, companyId, shopId).Scan(&staffCount)
	if scanError != nil {
		return false, fmt.Errorf("failed to check the shop's staff: %w", scanError)
	}
	return staffCount > 0, nil
}

func (repository *Repository) Delete(ctx context.Context, querier database.Querier, companyId uuid.UUID, shopId uuid.UUID) error {
	statements := []string{
		`DELETE FROM notifications WHERE company_id = $1 AND shop_id = $2`,
		`DELETE FROM shop_stock WHERE company_id = $1 AND shop_id = $2`,
		`DELETE FROM user_shops WHERE company_id = $1 AND shop_id = $2`,
		`UPDATE sessions SET shop_id = NULL WHERE company_id = $1 AND shop_id = $2`,
		`UPDATE support_messages SET shop_id = NULL WHERE company_id = $1 AND shop_id = $2`,
		`DELETE FROM shops WHERE company_id = $1 AND id = $2`,
	}
	for _, statement := range statements {
		_, execError := querier.ExecContext(ctx, statement, companyId, shopId)
		if execError != nil {
			return fmt.Errorf("failed to delete the shop: %w", execError)
		}
	}
	return nil
}
