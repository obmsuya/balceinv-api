package accounting

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/google/uuid"
)

const bookedMovementReasons = `('opening', 'return', 'purchase', 'adjustment', 'damage')`

func (repository *Repository) UnpostedSales(ctx context.Context, querier database.Querier, companyId uuid.UUID, since time.Time, onlySaleId *uuid.UUID) ([]SalePosting, error) {
	arguments := &queryArguments{}
	where := `s.company_id = ` + arguments.add(companyId) + ` AND s.created_at >= ` + arguments.add(since) +
		` AND NOT EXISTS (SELECT 1 FROM journal_entries e WHERE e.company_id = s.company_id AND e.source_type = 'sale' AND e.source_id = s.id)`
	if onlySaleId != nil {
		where += ` AND s.id = ` + arguments.add(*onlySaleId)
	}

	saleQuery := `
		SELECT s.id, s.shop_id, s.user_id, s.receipt_number, s.total, s.tax_total, s.change_given, s.created_at,
		       CAST(COALESCE((SELECT SUM(i.unit_cost * i.quantity) FROM sale_items i WHERE i.company_id = s.company_id AND i.sale_id = s.id), 0) AS BIGINT)
		FROM sales s
		WHERE ` + where + `
		ORDER BY s.created_at, s.id
	`
	saleRows, saleQueryError := querier.QueryContext(ctx, saleQuery, arguments.values...)
	if saleQueryError != nil {
		return nil, fmt.Errorf("failed to list sales for the books: %w", saleQueryError)
	}
	defer saleRows.Close()

	salePostings := []SalePosting{}
	postingIndexBySale := map[uuid.UUID]int{}
	for saleRows.Next() {
		userId := uuid.UUID{}
		salePosting := SalePosting{CompanyId: companyId, PaidByMethod: map[string]int64{}}
		scanError := saleRows.Scan(&salePosting.SaleId, &salePosting.ShopId, &userId, &salePosting.Reference, &salePosting.Total,
			&salePosting.TaxTotal, &salePosting.ChangeGiven, &salePosting.SoldAt, &salePosting.CostTotal)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan a sale for the books: %w", scanError)
		}
		salePosting.UserId = &userId
		postingIndexBySale[salePosting.SaleId] = len(salePostings)
		salePostings = append(salePostings, salePosting)
	}
	rowsError := saleRows.Err()
	if rowsError != nil {
		return nil, rowsError
	}
	if len(salePostings) == 0 {
		return salePostings, nil
	}

	paymentQuery := `SELECT p.sale_id, p.method, p.amount FROM sale_payments p JOIN sales s ON s.company_id = p.company_id AND s.id = p.sale_id WHERE ` + where
	paymentRows, paymentQueryError := querier.QueryContext(ctx, paymentQuery, arguments.values...)
	if paymentQueryError != nil {
		return nil, fmt.Errorf("failed to list sale payments for the books: %w", paymentQueryError)
	}
	defer paymentRows.Close()

	for paymentRows.Next() {
		saleId := uuid.UUID{}
		method := ""
		amount := int64(0)
		scanError := paymentRows.Scan(&saleId, &method, &amount)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan a sale payment for the books: %w", scanError)
		}
		salePostings[postingIndexBySale[saleId]].PaidByMethod[method] = amount
	}
	return salePostings, paymentRows.Err()
}

func (repository *Repository) UnpostedMovements(ctx context.Context, querier database.Querier, companyId uuid.UUID, since time.Time, onlyMovementId *uuid.UUID) ([]StockMovementPosting, error) {
	arguments := &queryArguments{}
	where := `m.company_id = ` + arguments.add(companyId) + ` AND m.created_at >= ` + arguments.add(since) +
		` AND m.reason IN ` + bookedMovementReasons +
		` AND NOT EXISTS (SELECT 1 FROM journal_entries e WHERE e.company_id = m.company_id AND e.source_type = 'stock_adjustment' AND e.source_id = m.id)`
	if onlyMovementId != nil {
		where += ` AND m.id = ` + arguments.add(*onlyMovementId)
	}

	query := `
		SELECT m.id, m.shop_id, m.user_id, m.reason, m.change, m.reference, m.created_at, p.cost_price
		FROM stock_movements m
		JOIN products p ON p.company_id = m.company_id AND p.id = m.product_id
		WHERE ` + where + `
		ORDER BY m.created_at, m.id
	`
	movementRows, queryError := querier.QueryContext(ctx, query, arguments.values...)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list stock movements for the books: %w", queryError)
	}
	defer movementRows.Close()

	movementPostings := []StockMovementPosting{}
	for movementRows.Next() {
		movementPosting := StockMovementPosting{CompanyId: companyId}
		scanError := movementRows.Scan(&movementPosting.MovementId, &movementPosting.ShopId, &movementPosting.UserId, &movementPosting.Reason,
			&movementPosting.Change, &movementPosting.Reference, &movementPosting.MovedAt, &movementPosting.UnitCost)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan a stock movement for the books: %w", scanError)
		}
		movementPostings = append(movementPostings, movementPosting)
	}
	return movementPostings, movementRows.Err()
}

func (repository *Repository) UnpostedTransfers(ctx context.Context, querier database.Querier, companyId uuid.UUID, since time.Time, onlyTransferId *uuid.UUID) ([]TransferPosting, error) {
	arguments := &queryArguments{}
	where := `t.company_id = ` + arguments.add(companyId) + ` AND t.created_at >= ` + arguments.add(since) +
		` AND NOT EXISTS (SELECT 1 FROM journal_entries e WHERE e.company_id = t.company_id AND e.source_type = 'stock_transfer' AND e.source_id = t.id)`
	if onlyTransferId != nil {
		where += ` AND t.id = ` + arguments.add(*onlyTransferId)
	}

	query := `
		SELECT t.id, t.from_shop_id, t.to_shop_id, t.user_id, t.created_at,
		       CAST(COALESCE((SELECT SUM(i.quantity * p.cost_price) FROM stock_transfer_items i
		                      JOIN products p ON p.company_id = i.company_id AND p.id = i.product_id
		                      WHERE i.company_id = t.company_id AND i.transfer_id = t.id), 0) AS BIGINT)
		FROM stock_transfers t
		WHERE ` + where + `
		ORDER BY t.created_at, t.id
	`
	transferRows, queryError := querier.QueryContext(ctx, query, arguments.values...)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list stock transfers for the books: %w", queryError)
	}
	defer transferRows.Close()

	transferPostings := []TransferPosting{}
	for transferRows.Next() {
		transferPosting := TransferPosting{CompanyId: companyId}
		scanError := transferRows.Scan(&transferPosting.TransferId, &transferPosting.FromShopId, &transferPosting.ToShopId,
			&transferPosting.UserId, &transferPosting.SentAt, &transferPosting.ValueAtCost)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan a stock transfer for the books: %w", scanError)
		}
		transferPostings = append(transferPostings, transferPosting)
	}
	return transferPostings, transferRows.Err()
}

type shopValue struct {
	ShopId uuid.UUID
	Value  int64
}

func (repository *Repository) StockValueByShopAt(ctx context.Context, querier database.Querier, companyId uuid.UUID, at time.Time) ([]shopValue, error) {
	query := `
		SELECT ss.shop_id,
		       CAST(COALESCE(SUM((ss.quantity - COALESCE(later.change_total, 0)) * p.cost_price), 0) AS BIGINT)
		FROM shop_stock ss
		JOIN products p ON p.company_id = ss.company_id AND p.id = ss.product_id
		LEFT JOIN (
			SELECT shop_id, product_id, SUM(change) AS change_total
			FROM stock_movements
			WHERE company_id = $1 AND created_at >= $2
			GROUP BY shop_id, product_id
		) later ON later.shop_id = ss.shop_id AND later.product_id = ss.product_id
		WHERE ss.company_id = $1
		GROUP BY ss.shop_id
		ORDER BY ss.shop_id
	`
	valueRows, queryError := querier.QueryContext(ctx, query, companyId, at)
	if queryError != nil {
		return nil, fmt.Errorf("failed to value the stock: %w", queryError)
	}
	defer valueRows.Close()

	shopValues := []shopValue{}
	for valueRows.Next() {
		value := shopValue{}
		scanError := valueRows.Scan(&value.ShopId, &value.Value)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan a stock value: %w", scanError)
		}
		shopValues = append(shopValues, value)
	}
	return shopValues, valueRows.Err()
}

func (repository *Repository) FirstRecordAt(ctx context.Context, querier database.Querier, companyId uuid.UUID) (*time.Time, error) {
	queries := []string{
		`SELECT created_at FROM stock_movements WHERE company_id = $1 ORDER BY created_at LIMIT 1`,
		`SELECT created_at FROM sales WHERE company_id = $1 ORDER BY created_at LIMIT 1`,
	}

	var firstRecordAt *time.Time
	for _, query := range queries {
		recordedAt := time.Time{}
		scanError := querier.QueryRowContext(ctx, query, companyId).Scan(&recordedAt)
		if errors.Is(scanError, sql.ErrNoRows) {
			continue
		}
		if scanError != nil {
			return nil, fmt.Errorf("failed to find the first record: %w", scanError)
		}
		if firstRecordAt == nil || recordedAt.Before(*firstRecordAt) {
			foundAt := recordedAt
			firstRecordAt = &foundAt
		}
	}
	return firstRecordAt, nil
}
