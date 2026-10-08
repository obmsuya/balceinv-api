package sales

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/google/uuid"
)

type refundableItem struct {
	Id               uuid.UUID
	ProductId        uuid.UUID
	ProductName      string
	VariantLabel     string
	Sku              string
	Quantity         int
	UnitPrice        int64
	AddonsUnitTotal  int64
	LineTotal        int64
	UnitCost         int64
	RefundedQuantity int
	RefundedAmount   int64
}

type newRefund struct {
	Id         uuid.UUID
	CompanyId  uuid.UUID
	SaleId     uuid.UUID
	ClientRef  string
	Method     string
	Amount     int64
	TaxAmount  int64
	CostAmount int64
	Restocked  bool
	Reason     string
	CreatedBy  uuid.UUID
	CreatedAt  time.Time
	Lines      []newRefundLine
}

type newRefundLine struct {
	SaleItemId uuid.UUID
	Quantity   int
	Amount     int64
	CostAmount int64
}

func (repository *Repository) RefundableItems(ctx context.Context, querier database.Querier, companyId uuid.UUID, saleId uuid.UUID) (map[uuid.UUID]refundableItem, error) {
	query := `
		SELECT i.id, i.product_id, i.product_name, i.variant_label, i.sku, i.quantity, i.unit_price, i.addons_unit_total,
		       i.line_total, i.unit_cost,
		       CAST(COALESCE((SELECT SUM(rl.quantity) FROM sale_refund_lines rl WHERE rl.company_id = i.company_id AND rl.sale_item_id = i.id), 0) AS BIGINT),
		       CAST(COALESCE((SELECT SUM(rl.amount) FROM sale_refund_lines rl WHERE rl.company_id = i.company_id AND rl.sale_item_id = i.id), 0) AS BIGINT)
		FROM sale_items i
		WHERE i.company_id = $1 AND i.sale_id = $2
	`

	itemRows, queryError := querier.QueryContext(ctx, query, companyId, saleId)
	if queryError != nil {
		return nil, fmt.Errorf("failed to load the sale's items for a refund: %w", queryError)
	}
	defer itemRows.Close()

	itemsById := map[uuid.UUID]refundableItem{}
	for itemRows.Next() {
		item := refundableItem{}
		scanError := itemRows.Scan(&item.Id, &item.ProductId, &item.ProductName, &item.VariantLabel, &item.Sku, &item.Quantity,
			&item.UnitPrice, &item.AddonsUnitTotal, &item.LineTotal, &item.UnitCost, &item.RefundedQuantity, &item.RefundedAmount)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan a sale item for a refund: %w", scanError)
		}
		itemsById[item.Id] = item
	}
	return itemsById, itemRows.Err()
}

func (repository *Repository) CreditRefunded(ctx context.Context, querier database.Querier, companyId uuid.UUID, saleId uuid.UUID) (int64, error) {
	query := `SELECT CAST(COALESCE(SUM(amount), 0) AS BIGINT) FROM sale_refunds WHERE company_id = $1 AND sale_id = $2 AND method = 'credit'`

	creditRefunded := int64(0)
	scanError := querier.QueryRowContext(ctx, query, companyId, saleId).Scan(&creditRefunded)
	if scanError != nil {
		return 0, fmt.Errorf("failed to total credit refunds: %w", scanError)
	}
	return creditRefunded, nil
}

func (repository *Repository) FindRefundSaleByClientRef(ctx context.Context, querier database.Querier, companyId uuid.UUID, clientRef string) (*uuid.UUID, error) {
	saleId := uuid.UUID{}
	scanError := querier.QueryRowContext(ctx, `SELECT sale_id FROM sale_refunds WHERE company_id = $1 AND client_ref = $2`, companyId, clientRef).Scan(&saleId)
	if errors.Is(scanError, sql.ErrNoRows) {
		return nil, nil
	}
	if scanError != nil {
		return nil, fmt.Errorf("failed to look up the refund reference: %w", scanError)
	}
	return &saleId, nil
}

func (repository *Repository) InsertRefund(ctx context.Context, querier database.Querier, refund newRefund) error {
	refundQuery := `
		INSERT INTO sale_refunds (id, company_id, sale_id, client_ref, method, amount, tax_amount, cost_amount, restocked, reason, created_by, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12)
	`
	_, refundError := querier.ExecContext(ctx, refundQuery, refund.Id, refund.CompanyId, refund.SaleId, refund.ClientRef, refund.Method,
		refund.Amount, refund.TaxAmount, refund.CostAmount, refund.Restocked, refund.Reason, refund.CreatedBy, refund.CreatedAt)
	if refundError != nil {
		return fmt.Errorf("failed to insert the refund: %w", refundError)
	}

	for _, refundLine := range refund.Lines {
		lineQuery := `
			INSERT INTO sale_refund_lines (company_id, refund_id, sale_item_id, quantity, amount, cost_amount)
			VALUES ($1, $2, $3, $4, $5, $6)
		`
		_, lineError := querier.ExecContext(ctx, lineQuery, refund.CompanyId, refund.Id, refundLine.SaleItemId, refundLine.Quantity, refundLine.Amount, refundLine.CostAmount)
		if lineError != nil {
			return fmt.Errorf("failed to insert a refund line: %w", lineError)
		}
	}
	return nil
}

func (repository *Repository) ListRefunds(ctx context.Context, querier database.Querier, companyId uuid.UUID, saleId *uuid.UUID, refundId *uuid.UUID) ([]RefundView, error) {
	query := `
		SELECT r.id, r.sale_id, r.method, r.amount, r.tax_amount, r.restocked, r.reason, u.name, r.created_at
		FROM sale_refunds r
		JOIN users u ON u.company_id = r.company_id AND u.id = r.created_by
		WHERE r.company_id = $1 AND (r.sale_id = $2 OR r.id = $3)
		ORDER BY r.created_at, r.id
	`

	refundRows, queryError := querier.QueryContext(ctx, query, companyId, saleId, refundId)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list refunds: %w", queryError)
	}
	defer refundRows.Close()

	refunds := []RefundView{}
	for refundRows.Next() {
		refund := RefundView{Lines: []RefundLineView{}}
		scanError := refundRows.Scan(&refund.Id, &refund.SaleId, &refund.Method, &refund.Amount, &refund.TaxAmount, &refund.Restocked,
			&refund.Reason, &refund.CreatedByName, &refund.CreatedAt)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan a refund: %w", scanError)
		}
		refunds = append(refunds, refund)
	}
	rowsError := refundRows.Err()
	if rowsError != nil {
		return nil, rowsError
	}

	for refundIndex := range refunds {
		lineQuery := `
			SELECT rl.sale_item_id, i.product_name, i.variant_label, i.sku, rl.quantity, rl.amount
			FROM sale_refund_lines rl
			JOIN sale_items i ON i.company_id = rl.company_id AND i.id = rl.sale_item_id
			WHERE rl.company_id = $1 AND rl.refund_id = $2
			ORDER BY i.position
		`
		lineRows, lineQueryError := querier.QueryContext(ctx, lineQuery, companyId, refunds[refundIndex].Id)
		if lineQueryError != nil {
			return nil, fmt.Errorf("failed to list refund lines: %w", lineQueryError)
		}
		for lineRows.Next() {
			refundLine := RefundLineView{}
			scanError := lineRows.Scan(&refundLine.ItemId, &refundLine.ProductName, &refundLine.VariantLabel, &refundLine.Sku, &refundLine.Quantity, &refundLine.Amount)
			if scanError != nil {
				lineRows.Close()
				return nil, fmt.Errorf("failed to scan a refund line: %w", scanError)
			}
			refunds[refundIndex].Lines = append(refunds[refundIndex].Lines, refundLine)
		}
		lineRowsError := lineRows.Err()
		lineRows.Close()
		if lineRowsError != nil {
			return nil, lineRowsError
		}
	}
	return refunds, nil
}
