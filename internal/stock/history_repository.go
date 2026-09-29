package stock

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/google/uuid"
)

const movementJoins = `
	FROM stock_movements m
	JOIN products p ON p.company_id = m.company_id AND p.id = m.product_id
	LEFT JOIN users u ON u.company_id = m.company_id AND u.id = m.user_id
`

func movementWhere(companyId uuid.UUID, shopId uuid.UUID, filter MovementFilter) (string, []any) {
	conditions := []string{"m.company_id = $1", "m.shop_id = $2"}
	arguments := []any{companyId, shopId}

	addCondition := func(condition string, argument any) {
		arguments = append(arguments, argument)
		conditions = append(conditions, condition+" $"+strconv.Itoa(len(arguments)))
	}
	if filter.ProductId != nil {
		addCondition("m.product_id =", *filter.ProductId)
	}
	if filter.Reason != "" {
		addCondition("m.reason =", filter.Reason)
	}
	if filter.From != nil {
		addCondition("m.created_at >=", *filter.From)
	}
	if filter.To != nil {
		addCondition("m.created_at <", *filter.To)
	}

	return " WHERE " + strings.Join(conditions, " AND "), arguments
}

func (repository *Repository) CountMovements(ctx context.Context, querier database.Querier, companyId uuid.UUID, shopId uuid.UUID, filter MovementFilter) (int64, error) {
	whereClause, whereArguments := movementWhere(companyId, shopId, filter)
	query := `SELECT COUNT(*) ` + movementJoins + whereClause

	movementCount := int64(0)
	scanError := querier.QueryRowContext(ctx, query, whereArguments...).Scan(&movementCount)
	if scanError != nil {
		return 0, fmt.Errorf("failed to count stock movements: %w", scanError)
	}

	return movementCount, nil
}

func (repository *Repository) ListMovements(ctx context.Context, querier database.Querier, companyId uuid.UUID, shopId uuid.UUID, filter MovementFilter, limit int, offset int) ([]MovementView, error) {
	whereClause, whereArguments := movementWhere(companyId, shopId, filter)
	limitPosition := strconv.Itoa(len(whereArguments) + 1)
	offsetPosition := strconv.Itoa(len(whereArguments) + 2)
	query := `
		SELECT m.id, m.product_id, p.name, p.variant_label, p.sku, p.unit, m.change, m.quantity_after,
		       m.reason, m.reference, m.user_id, u.name, m.created_at
		` + movementJoins + whereClause + `
		ORDER BY m.created_at DESC, m.id DESC
		LIMIT $` + limitPosition + ` OFFSET $` + offsetPosition

	queryArguments := append(whereArguments, limit, offset)
	movementRows, queryError := querier.QueryContext(ctx, query, queryArguments...)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list stock movements: %w", queryError)
	}
	defer movementRows.Close()

	movementViews := []MovementView{}
	for movementRows.Next() {
		movementView := MovementView{}
		scanError := movementRows.Scan(
			&movementView.Id,
			&movementView.ProductId,
			&movementView.ProductName,
			&movementView.VariantLabel,
			&movementView.Sku,
			&movementView.Unit,
			&movementView.Change,
			&movementView.QuantityAfter,
			&movementView.Reason,
			&movementView.Reference,
			&movementView.UserId,
			&movementView.UserName,
			&movementView.CreatedAt,
		)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan stock movement: %w", scanError)
		}
		movementViews = append(movementViews, movementView)
	}

	return movementViews, movementRows.Err()
}
