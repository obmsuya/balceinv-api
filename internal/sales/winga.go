package sales

import (
	"context"
	"database/sql"
	"fmt"
	"strings"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/features"
	"github.com/google/uuid"
)

// ponytail: winga lines are not refundable line by line; voiding the sale undoes them. Add a sale_refund_winga_lines table when shops ask to refund one broker item.
type WingaLine struct {
	Name       string
	Quantity   int
	UnitPrice  int64
	WingaShare int64
	LineTotal  int64
}

func (pricedSale PricedSale) WithWingaLines(wingaLines []WingaLine) PricedSale {
	for _, wingaLine := range wingaLines {
		pricedSale.WingaLines = append(pricedSale.WingaLines, wingaLine)
		pricedSale.Subtotal += wingaLine.LineTotal
	}
	pricedSale.Total = pricedSale.Subtotal - pricedSale.DiscountTotal
	pricedSale.TaxTotal = IncludedTax(pricedSale.Total, pricedSale.TaxRateBasisPoints)
	return pricedSale
}

func (service *Service) wingaLinesFor(ctx context.Context, querier database.Querier, companyId uuid.UUID, wingaRequests []WingaLineRequest) ([]WingaLine, error) {
	if len(wingaRequests) == 0 {
		return nil, nil
	}
	companyFeatures, featuresError := features.NewRepository().Find(ctx, querier, companyId)
	if featuresError != nil {
		return nil, fmt.Errorf("failed to read whether winga lines are on: %w", featuresError)
	}
	if !companyFeatures.WingaEnabled {
		return nil, ErrWingaOff
	}

	wingaLines := make([]WingaLine, 0, len(wingaRequests))
	for _, wingaRequest := range wingaRequests {
		trimmedName := strings.TrimSpace(wingaRequest.Name)
		if trimmedName == "" {
			return nil, ErrWingaNameRequired
		}
		lineTotal := int64(wingaRequest.Quantity) * wingaRequest.UnitPrice
		if wingaRequest.WingaShare > lineTotal {
			return nil, ErrWingaShareTooHigh
		}
		wingaLines = append(wingaLines, WingaLine{
			Name:       trimmedName,
			Quantity:   wingaRequest.Quantity,
			UnitPrice:  wingaRequest.UnitPrice,
			WingaShare: wingaRequest.WingaShare,
			LineTotal:  lineTotal,
		})
	}
	return wingaLines, nil
}

func ToWingaLineViews(wingaLines []WingaLine) []WingaLineView {
	wingaLineViews := make([]WingaLineView, 0, len(wingaLines))
	for _, wingaLine := range wingaLines {
		wingaLineViews = append(wingaLineViews, WingaLineView{
			Name:       wingaLine.Name,
			Quantity:   wingaLine.Quantity,
			UnitPrice:  wingaLine.UnitPrice,
			WingaShare: wingaLine.WingaShare,
			LineTotal:  wingaLine.LineTotal,
		})
	}
	return wingaLineViews
}

func (repository *Repository) InsertWingaLines(ctx context.Context, querier database.Querier, companyId uuid.UUID, saleId uuid.UUID, wingaLines []WingaLine) error {
	for position, wingaLine := range wingaLines {
		query := `
			INSERT INTO sale_winga_lines (id, company_id, sale_id, position, name, quantity, unit_price, line_total, winga_share)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9)
		`
		_, insertError := querier.ExecContext(ctx, query, uuid.Must(uuid.NewV7()), companyId, saleId, position,
			wingaLine.Name, wingaLine.Quantity, wingaLine.UnitPrice, wingaLine.LineTotal, wingaLine.WingaShare)
		if insertError != nil {
			return fmt.Errorf("failed to insert a winga line: %w", insertError)
		}
	}
	return nil
}

func (repository *Repository) ListWingaLines(ctx context.Context, querier database.Querier, companyId uuid.UUID, saleId uuid.UUID) ([]WingaLine, error) {
	query := `
		SELECT name, quantity, unit_price, winga_share, line_total
		FROM sale_winga_lines
		WHERE company_id = $1 AND sale_id = $2
		ORDER BY position
	`
	return ScanWingaLines(querier.QueryContext(ctx, query, companyId, saleId))
}

func ScanWingaLines(wingaRows *sql.Rows, queryError error) ([]WingaLine, error) {
	if queryError != nil {
		return nil, fmt.Errorf("failed to list winga lines: %w", queryError)
	}
	defer wingaRows.Close()

	wingaLines := []WingaLine{}
	for wingaRows.Next() {
		wingaLine := WingaLine{}
		scanError := wingaRows.Scan(&wingaLine.Name, &wingaLine.Quantity, &wingaLine.UnitPrice, &wingaLine.WingaShare, &wingaLine.LineTotal)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan a winga line: %w", scanError)
		}
		wingaLines = append(wingaLines, wingaLine)
	}
	return wingaLines, wingaRows.Err()
}
