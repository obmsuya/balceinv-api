package sales

import (
	"context"
	"fmt"
	"strconv"
	"strings"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/google/uuid"
)

func saleWhere(companyId uuid.UUID, shopId uuid.UUID, filter SaleFilter) (string, []any) {
	conditions := []string{"s.company_id = $1", "s.shop_id = $2"}
	arguments := []any{companyId, shopId}

	addCondition := func(condition string, argument any) {
		arguments = append(arguments, argument)
		conditions = append(conditions, strings.ReplaceAll(condition, "?", "$"+strconv.Itoa(len(arguments))))
	}
	searchText := strings.ToLower(strings.TrimSpace(filter.SearchText))
	if searchText != "" {
		addCondition("lower(s.receipt_number) LIKE ?", "%"+searchText+"%")
	}
	if filter.From != nil {
		addCondition("s.created_at >= ?", *filter.From)
	}
	if filter.To != nil {
		addCondition("s.created_at < ?", *filter.To)
	}
	if filter.FiscalWaiting {
		conditions = append(conditions, "EXISTS (SELECT 1 FROM fiscal_receipts f WHERE f.company_id = s.company_id AND f.sale_id = s.id AND f.status <> 'sent')")
	}

	return " WHERE " + strings.Join(conditions, " AND "), arguments
}

func (repository *Repository) Totals(ctx context.Context, querier database.Querier, companyId uuid.UUID, shopId uuid.UUID, filter SaleFilter) (TotalsView, error) {
	whereClause, whereArguments := saleWhere(companyId, shopId, filter)
	query := `
		SELECT COUNT(*),
		       CAST(COALESCE(SUM(s.total), 0) AS BIGINT),
		       CAST(COALESCE(SUM(s.tax_total), 0) AS BIGINT),
		       CAST(COALESCE(SUM(s.discount_total), 0) AS BIGINT)
		FROM sales s` + whereClause + ` AND s.voided_at IS NULL`

	totals := TotalsView{}
	scanError := querier.QueryRowContext(ctx, query, whereArguments...).Scan(&totals.SaleCount, &totals.Total, &totals.TaxTotal, &totals.DiscountTotal)
	if scanError != nil {
		return TotalsView{}, fmt.Errorf("failed to total sales: %w", scanError)
	}

	return totals, nil
}

func (repository *Repository) ListSummaries(ctx context.Context, querier database.Querier, companyId uuid.UUID, shopId uuid.UUID, filter SaleFilter, limit int, offset int) ([]SaleSummaryView, error) {
	whereClause, whereArguments := saleWhere(companyId, shopId, filter)
	limitPosition := strconv.Itoa(len(whereArguments) + 1)
	offsetPosition := strconv.Itoa(len(whereArguments) + 2)
	query := `
		SELECT s.id, s.receipt_number, s.total, s.discount_total, u.name, s.created_at,
		       (SELECT COALESCE(SUM(i.quantity), 0) FROM sale_items i WHERE i.company_id = s.company_id AND i.sale_id = s.id),
		       (SELECT f.status FROM fiscal_receipts f WHERE f.company_id = s.company_id AND f.sale_id = s.id),
		       s.voided_at
		FROM sales s
		JOIN users u ON u.company_id = s.company_id AND u.id = s.user_id` + whereClause + `
		ORDER BY s.created_at DESC, s.id DESC
		LIMIT $` + limitPosition + ` OFFSET $` + offsetPosition

	queryArguments := append(whereArguments, limit, offset)
	saleRows, queryError := querier.QueryContext(ctx, query, queryArguments...)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list sales: %w", queryError)
	}
	defer saleRows.Close()

	summaries := []SaleSummaryView{}
	saleIds := []uuid.UUID{}
	for saleRows.Next() {
		summary := SaleSummaryView{PaymentMethods: []string{}}
		scanError := saleRows.Scan(
			&summary.Id,
			&summary.ReceiptNumber,
			&summary.Total,
			&summary.DiscountTotal,
			&summary.CashierName,
			&summary.CreatedAt,
			&summary.UnitCount,
			&summary.FiscalStatus,
			&summary.VoidedAt,
		)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan sale: %w", scanError)
		}
		summaries = append(summaries, summary)
		saleIds = append(saleIds, summary.Id)
	}
	if rowsError := saleRows.Err(); rowsError != nil {
		return nil, rowsError
	}
	if len(saleIds) == 0 {
		return summaries, nil
	}

	paymentQuery := `
		SELECT sale_id, method FROM sale_payments
		WHERE company_id = $1 AND sale_id IN (` + database.Placeholders(2, len(saleIds)) + `)
		ORDER BY method
	`
	paymentArguments := append([]any{companyId}, database.ToArguments(saleIds)...)
	paymentRows, paymentError := querier.QueryContext(ctx, paymentQuery, paymentArguments...)
	if paymentError != nil {
		return nil, fmt.Errorf("failed to list sale payment methods: %w", paymentError)
	}
	defer paymentRows.Close()

	summaryIndexById := map[uuid.UUID]int{}
	for summaryIndex, summary := range summaries {
		summaryIndexById[summary.Id] = summaryIndex
	}
	for paymentRows.Next() {
		saleId := uuid.UUID{}
		method := ""
		scanError := paymentRows.Scan(&saleId, &method)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan sale payment method: %w", scanError)
		}
		summaryIndex := summaryIndexById[saleId]
		summaries[summaryIndex].PaymentMethods = append(summaries[summaryIndex].PaymentMethods, method)
	}

	return summaries, paymentRows.Err()
}

func (repository *Repository) FindReceiptShop(ctx context.Context, querier database.Querier, companyId uuid.UUID, shopId uuid.UUID) (ReceiptShopView, error) {
	query := `SELECT name, address, phone FROM shops WHERE company_id = $1 AND id = $2`

	shopView := ReceiptShopView{}
	scanError := querier.QueryRowContext(ctx, query, companyId, shopId).Scan(&shopView.Name, &shopView.Address, &shopView.Phone)
	if scanError != nil {
		return ReceiptShopView{}, fmt.Errorf("failed to find receipt shop: %w", scanError)
	}

	return shopView, nil
}
