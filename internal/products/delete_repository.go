package products

import (
	"context"
	"fmt"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/google/uuid"
)

var productUsage = []struct {
	reason string
	table  string
}{
	{"sold", "sale_items"},
	{"bought", "purchase_lines"},
	{"bought", "purchase_order_lines"},
	{"returned_to_supplier", "supplier_return_lines"},
	{"ordered", "customer_order_lines"},
	{"transferred", "stock_transfer_items"},
}

func (repository *Repository) FamilyIds(ctx context.Context, querier database.Querier, companyId uuid.UUID, productId uuid.UUID) ([]uuid.UUID, error) {
	query := `SELECT id FROM products WHERE company_id = $1 AND (id = $2 OR parent_id = $2) ORDER BY parent_id IS NULL, id`

	familyRows, queryError := querier.QueryContext(ctx, query, companyId, productId)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list the product and its variants: %w", queryError)
	}
	defer familyRows.Close()

	familyIds := []uuid.UUID{}
	for familyRows.Next() {
		familyId := uuid.UUID{}
		scanError := familyRows.Scan(&familyId)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan a product id: %w", scanError)
		}
		familyIds = append(familyIds, familyId)
	}
	return familyIds, familyRows.Err()
}

func (repository *Repository) UsageReason(ctx context.Context, querier database.Querier, companyId uuid.UUID, productIds []uuid.UUID) (string, error) {
	placeholders := database.Placeholders(2, len(productIds))
	queryArguments := append([]any{companyId}, database.ToArguments(productIds)...)
	for _, usage := range productUsage {
		usageCount := int64(0)
		query := `SELECT COUNT(*) FROM (SELECT 1 FROM ` + usage.table + ` WHERE company_id = $1 AND product_id IN (` + placeholders + `) LIMIT 1) used`
		scanError := querier.QueryRowContext(ctx, query, queryArguments...).Scan(&usageCount)
		if scanError != nil {
			return "", fmt.Errorf("failed to check %s: %w", usage.table, scanError)
		}
		if usageCount > 0 {
			return usage.reason, nil
		}
	}
	return "", nil
}

func (repository *Repository) DeleteFamily(ctx context.Context, querier database.Querier, companyId uuid.UUID, familyIds []uuid.UUID) error {
	placeholders := database.Placeholders(2, len(familyIds))
	queryArguments := append([]any{companyId}, database.ToArguments(familyIds)...)
	for _, table := range []string{"notifications", "discounts"} {
		_, deleteError := querier.ExecContext(ctx, `DELETE FROM `+table+` WHERE company_id = $1 AND product_id IN (`+placeholders+`)`, queryArguments...)
		if deleteError != nil {
			return fmt.Errorf("failed to delete the product's %s: %w", table, deleteError)
		}
	}
	for _, familyId := range familyIds {
		_, deleteError := querier.ExecContext(ctx, `DELETE FROM products WHERE company_id = $1 AND id = $2`, companyId, familyId)
		if deleteError != nil {
			return fmt.Errorf("failed to delete the product: %w", deleteError)
		}
	}
	return nil
}
