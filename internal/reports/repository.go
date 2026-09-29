package reports

import (
	"context"
	"database/sql"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/stock"
	"github.com/google/uuid"
)

type Repository struct {
	isPostgres bool
}

func NewRepository(isPostgres bool) *Repository {
	return &Repository{
		isPostgres: isPostgres,
	}
}

type queryArguments struct {
	values []any
}

func (arguments *queryArguments) add(value any) string {
	arguments.values = append(arguments.values, value)
	return "$" + strconv.Itoa(len(arguments.values))
}

func (repository *Repository) timestampParameter(arguments *queryArguments, value time.Time) string {
	placeholder := arguments.add(value)
	if repository.isPostgres {
		return "CAST(" + placeholder + " AS TIMESTAMPTZ)"
	}
	return placeholder
}

func shopCondition(column string, scope Scope, arguments *queryArguments) string {
	if scope.AllShops {
		return ""
	}
	placeholders := []string{}
	for _, shopId := range scope.ShopIds {
		placeholders = append(placeholders, arguments.add(shopId))
	}
	return " AND " + column + " IN (" + strings.Join(placeholders, ", ") + ")"
}

func salesWhere(scope Scope, arguments *queryArguments) string {
	return "s.company_id = " + arguments.add(scope.CompanyId) +
		" AND s.created_at >= " + arguments.add(scope.From) +
		" AND s.created_at < " + arguments.add(scope.To) +
		shopCondition("s.shop_id", scope, arguments)
}

func (repository *Repository) SaleTotals(ctx context.Context, querier database.Querier, scope Scope) (SummaryView, error) {
	arguments := &queryArguments{}
	query := `
		SELECT COUNT(*),
		       CAST(COALESCE(SUM(s.total), 0) AS BIGINT),
		       CAST(COALESCE(SUM(s.tax_total), 0) AS BIGINT),
		       CAST(COALESCE(SUM(s.discount_total), 0) AS BIGINT),
		       CAST(COALESCE(SUM(s.change_given), 0) AS BIGINT)
		FROM sales s
		WHERE ` + salesWhere(scope, arguments)

	summary := SummaryView{}
	changeGiven := int64(0)
	scanError := querier.QueryRowContext(ctx, query, arguments.values...).Scan(
		&summary.SaleCount,
		&summary.Total,
		&summary.TaxTotal,
		&summary.DiscountTotal,
		&changeGiven,
	)
	if scanError != nil {
		return SummaryView{}, fmt.Errorf("failed to total sales: %w", scanError)
	}

	itemArguments := &queryArguments{}
	itemQuery := `
		SELECT CAST(COALESCE(SUM(i.quantity), 0) AS BIGINT),
		       CAST(COALESCE(SUM(i.unit_cost * i.quantity), 0) AS BIGINT)
		FROM sale_items i
		JOIN sales s ON s.company_id = i.company_id AND s.id = i.sale_id
		WHERE ` + salesWhere(scope, itemArguments)

	itemScanError := querier.QueryRowContext(ctx, itemQuery, itemArguments.values...).Scan(&summary.UnitsSold, &summary.CostTotal)
	if itemScanError != nil {
		return SummaryView{}, fmt.Errorf("failed to total sold items: %w", itemScanError)
	}

	paymentArguments := &queryArguments{}
	paymentQuery := `
		SELECT p.method, CAST(COALESCE(SUM(p.amount), 0) AS BIGINT)
		FROM sale_payments p
		JOIN sales s ON s.company_id = p.company_id AND s.id = p.sale_id
		WHERE ` + salesWhere(scope, paymentArguments) + `
		GROUP BY p.method
	`

	paymentRows, paymentError := querier.QueryContext(ctx, paymentQuery, paymentArguments.values...)
	if paymentError != nil {
		return SummaryView{}, fmt.Errorf("failed to total payments: %w", paymentError)
	}
	defer paymentRows.Close()
	for paymentRows.Next() {
		method := ""
		amount := int64(0)
		rowScanError := paymentRows.Scan(&method, &amount)
		if rowScanError != nil {
			return SummaryView{}, fmt.Errorf("failed to scan a payment total: %w", rowScanError)
		}
		switch method {
		case "cash":
			summary.Payments.Cash = amount - changeGiven
		case "card":
			summary.Payments.Card = amount
		case "mobile":
			summary.Payments.Mobile = amount
		case "credit":
			summary.Payments.Credit = amount
		}
	}
	return summary, paymentRows.Err()
}

type dayBucket struct {
	label    string
	startsAt time.Time
	endsAt   time.Time
}

func (repository *Repository) Daily(ctx context.Context, querier database.Querier, scope Scope, dayBuckets []dayBucket) ([]DayView, error) {
	arguments := &queryArguments{}
	dayRows := []string{}
	for _, bucket := range dayBuckets {
		labelPlaceholder := "CAST(" + arguments.add(bucket.label) + " AS TEXT)"
		startsPlaceholder := repository.timestampParameter(arguments, bucket.startsAt)
		endsPlaceholder := repository.timestampParameter(arguments, bucket.endsAt)
		dayRows = append(dayRows, "("+labelPlaceholder+", "+startsPlaceholder+", "+endsPlaceholder+")")
	}
	companyPlaceholder := arguments.add(scope.CompanyId)
	shopFilter := shopCondition("s.shop_id", scope, arguments)
	itemFrom := arguments.add(scope.From)
	itemTo := arguments.add(scope.To)
	itemShopFilter := shopCondition("s.shop_id", scope, arguments)

	query := `
		WITH days (day, starts_at, ends_at) AS (VALUES ` + strings.Join(dayRows, ", ") + `),
		item_costs AS (
			SELECT i.sale_id, SUM(i.unit_cost * i.quantity) AS cost
			FROM sale_items i
			JOIN sales s ON s.company_id = i.company_id AND s.id = i.sale_id
			WHERE s.company_id = ` + companyPlaceholder + ` AND s.created_at >= ` + itemFrom + ` AND s.created_at < ` + itemTo + itemShopFilter + `
			GROUP BY i.sale_id
		)
		SELECT d.day,
		       COUNT(s.id),
		       CAST(COALESCE(SUM(s.total), 0) AS BIGINT),
		       CAST(COALESCE(SUM(s.tax_total), 0) AS BIGINT),
		       CAST(COALESCE(SUM(c.cost), 0) AS BIGINT)
		FROM days d
		LEFT JOIN sales s ON s.company_id = ` + companyPlaceholder + ` AND s.created_at >= d.starts_at AND s.created_at < d.ends_at` + shopFilter + `
		LEFT JOIN item_costs c ON c.sale_id = s.id
		GROUP BY d.day
		ORDER BY d.day
	`

	dayRowsResult, queryError := querier.QueryContext(ctx, query, arguments.values...)
	if queryError != nil {
		return nil, fmt.Errorf("failed to total sales per day: %w", queryError)
	}
	defer dayRowsResult.Close()

	dayViews := []DayView{}
	for dayRowsResult.Next() {
		dayView := DayView{}
		scanError := dayRowsResult.Scan(&dayView.Date, &dayView.SaleCount, &dayView.Total, &dayView.TaxTotal, &dayView.CostTotal)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan a day: %w", scanError)
		}
		dayView.GrossProfit = dayView.Total - dayView.TaxTotal - dayView.CostTotal
		dayViews = append(dayViews, dayView)
	}
	return dayViews, dayRowsResult.Err()
}

func (repository *Repository) Products(ctx context.Context, querier database.Querier, scope Scope, orderColumn string, limit int) ([]ProductRowView, error) {
	arguments := &queryArguments{}
	whereClause := salesWhere(scope, arguments)
	limitPlaceholder := arguments.add(limit)
	query := `
		SELECT i.product_id,
		       MAX(i.product_name),
		       MAX(i.variant_label),
		       MAX(i.sku),
		       CAST(SUM(i.quantity) AS BIGINT),
		       CAST(SUM(i.line_total) AS BIGINT),
		       CAST(SUM(i.line_total * 10000 / (10000 + s.tax_rate_basis_points)) AS BIGINT),
		       CAST(SUM(i.unit_cost * i.quantity) AS BIGINT),
		       COUNT(DISTINCT i.sale_id)
		FROM sale_items i
		JOIN sales s ON s.company_id = i.company_id AND s.id = i.sale_id
		WHERE ` + whereClause + `
		GROUP BY i.product_id
		ORDER BY ` + orderColumn + ` DESC, i.product_id
		LIMIT ` + limitPlaceholder

	productRows, queryError := querier.QueryContext(ctx, query, arguments.values...)
	if queryError != nil {
		return nil, fmt.Errorf("failed to rank products: %w", queryError)
	}
	defer productRows.Close()

	productViews := []ProductRowView{}
	for productRows.Next() {
		productView := ProductRowView{}
		scanError := productRows.Scan(
			&productView.ProductId,
			&productView.Name,
			&productView.VariantLabel,
			&productView.Sku,
			&productView.Quantity,
			&productView.Revenue,
			&productView.NetRevenue,
			&productView.CostTotal,
			&productView.SaleCount,
		)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan a product row: %w", scanError)
		}
		productView.GrossProfit = productView.NetRevenue - productView.CostTotal
		productViews = append(productViews, productView)
	}
	return productViews, productRows.Err()
}

func (repository *Repository) Cashiers(ctx context.Context, querier database.Querier, scope Scope) ([]CashierRowView, error) {
	arguments := &queryArguments{}
	query := `
		SELECT s.user_id, MAX(u.name), COUNT(*), CAST(SUM(s.total) AS BIGINT)
		FROM sales s
		JOIN users u ON u.company_id = s.company_id AND u.id = s.user_id
		WHERE ` + salesWhere(scope, arguments) + `
		GROUP BY s.user_id
		ORDER BY SUM(s.total) DESC, s.user_id
	`

	cashierRows, queryError := querier.QueryContext(ctx, query, arguments.values...)
	if queryError != nil {
		return nil, fmt.Errorf("failed to total sales per cashier: %w", queryError)
	}
	defer cashierRows.Close()

	cashierViews := []CashierRowView{}
	for cashierRows.Next() {
		cashierView := CashierRowView{}
		scanError := cashierRows.Scan(&cashierView.UserId, &cashierView.Name, &cashierView.SaleCount, &cashierView.Total)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan a cashier row: %w", scanError)
		}
		cashierView.AverageSale = averageOf(cashierView.Total, cashierView.SaleCount)
		cashierViews = append(cashierViews, cashierView)
	}
	return cashierViews, cashierRows.Err()
}

func (repository *Repository) Shops(ctx context.Context, querier database.Querier, scope Scope) ([]ShopRowView, error) {
	arguments := &queryArguments{}
	whereClause := salesWhere(scope, arguments)
	query := `
		WITH item_costs AS (
			SELECT i.sale_id, SUM(i.unit_cost * i.quantity) AS cost
			FROM sale_items i
			JOIN sales s ON s.company_id = i.company_id AND s.id = i.sale_id
			WHERE ` + whereClause + `
			GROUP BY i.sale_id
		)
		SELECT s.shop_id,
		       MAX(sh.name),
		       COUNT(*),
		       CAST(SUM(s.total) AS BIGINT),
		       CAST(SUM(s.tax_total) AS BIGINT),
		       CAST(COALESCE(SUM(c.cost), 0) AS BIGINT)
		FROM sales s
		JOIN shops sh ON sh.company_id = s.company_id AND sh.id = s.shop_id
		LEFT JOIN item_costs c ON c.sale_id = s.id
		WHERE ` + whereClause + `
		GROUP BY s.shop_id
		ORDER BY SUM(s.total) DESC, s.shop_id
	`

	shopRows, queryError := querier.QueryContext(ctx, query, arguments.values...)
	if queryError != nil {
		return nil, fmt.Errorf("failed to total sales per shop: %w", queryError)
	}
	defer shopRows.Close()

	shopViews := []ShopRowView{}
	for shopRows.Next() {
		shopView := ShopRowView{}
		scanError := shopRows.Scan(&shopView.ShopId, &shopView.Name, &shopView.SaleCount, &shopView.Total, &shopView.TaxTotal, &shopView.CostTotal)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan a shop row: %w", scanError)
		}
		shopView.GrossProfit = shopView.Total - shopView.TaxTotal - shopView.CostTotal
		shopViews = append(shopViews, shopView)
	}
	return shopViews, shopRows.Err()
}

func (repository *Repository) StockTotals(ctx context.Context, querier database.Querier, scope Scope) (StockTotalsView, error) {
	arguments := &queryArguments{}
	companyPlaceholder := arguments.add(scope.CompanyId)
	shopFilter := shopCondition("sh.id", scope, arguments)
	defaultMinimumPlaceholder := arguments.add(stock.DefaultMinimumStock)
	query := `
		SELECT COUNT(DISTINCT p.id),
		       CAST(COALESCE(SUM(COALESCE(ss.quantity, 0)), 0) AS BIGINT),
		       CAST(COALESCE(SUM(COALESCE(ss.quantity, 0) * p.cost_price), 0) AS BIGINT),
		       CAST(COALESCE(SUM(COALESCE(ss.quantity, 0) * p.price), 0) AS BIGINT),
		       CAST(COALESCE(SUM(CASE WHEN COALESCE(ss.quantity, 0) > 0 AND COALESCE(ss.quantity, 0) <= COALESCE(ss.min_stock, ` + defaultMinimumPlaceholder + `) THEN 1 ELSE 0 END), 0) AS BIGINT),
		       CAST(COALESCE(SUM(CASE WHEN COALESCE(ss.quantity, 0) = 0 THEN 1 ELSE 0 END), 0) AS BIGINT)
		FROM products p
		JOIN shops sh ON sh.company_id = p.company_id AND sh.is_active` + shopFilter + `
		LEFT JOIN shop_stock ss ON ss.company_id = p.company_id AND ss.product_id = p.id AND ss.shop_id = sh.id
		WHERE p.company_id = ` + companyPlaceholder + ` AND p.is_active
	`

	stockTotals := StockTotalsView{}
	scanError := querier.QueryRowContext(ctx, query, arguments.values...).Scan(
		&stockTotals.ProductCount,
		&stockTotals.Units,
		&stockTotals.ValueAtCost,
		&stockTotals.ValueAtPrice,
		&stockTotals.LowCount,
		&stockTotals.OutCount,
	)
	if scanError != nil {
		return StockTotalsView{}, fmt.Errorf("failed to total stock: %w", scanError)
	}
	return stockTotals, nil
}

func (repository *Repository) DeadStock(ctx context.Context, querier database.Querier, scope Scope, soldBefore time.Time, limit int) ([]DeadStockView, error) {
	arguments := &queryArguments{}
	companyPlaceholder := arguments.add(scope.CompanyId)
	stockShopFilter := shopCondition("ss.shop_id", scope, arguments)
	saleShopFilter := shopCondition("s.shop_id", scope, arguments)
	soldBeforePlaceholder := arguments.add(soldBefore)
	limitPlaceholder := arguments.add(limit)
	query := `
		WITH stock AS (
			SELECT ss.product_id, SUM(ss.quantity) AS quantity
			FROM shop_stock ss
			WHERE ss.company_id = ` + companyPlaceholder + stockShopFilter + `
			GROUP BY ss.product_id
		),
		last_sales AS (
			SELECT i.product_id, MAX(s.created_at) AS last_sold_at
			FROM sale_items i
			JOIN sales s ON s.company_id = i.company_id AND s.id = i.sale_id
			WHERE s.company_id = ` + companyPlaceholder + saleShopFilter + `
			GROUP BY i.product_id
		)
		SELECT p.id, p.name, p.variant_label, p.sku,
		       CAST(st.quantity AS BIGINT),
		       CAST(st.quantity * p.cost_price AS BIGINT),
		       ls.last_sold_at
		FROM stock st
		JOIN products p ON p.company_id = ` + companyPlaceholder + ` AND p.id = st.product_id
		LEFT JOIN last_sales ls ON ls.product_id = st.product_id
		WHERE p.is_active AND st.quantity > 0 AND (ls.last_sold_at IS NULL OR ls.last_sold_at < ` + soldBeforePlaceholder + `)
		ORDER BY st.quantity * p.cost_price DESC, p.id
		LIMIT ` + limitPlaceholder

	deadRows, queryError := querier.QueryContext(ctx, query, arguments.values...)
	if queryError != nil {
		return nil, fmt.Errorf("failed to find dead stock: %w", queryError)
	}
	defer deadRows.Close()

	deadViews := []DeadStockView{}
	for deadRows.Next() {
		deadView := DeadStockView{}
		lastSoldText := sql.NullString{}
		scanError := deadRows.Scan(
			&deadView.ProductId,
			&deadView.Name,
			&deadView.VariantLabel,
			&deadView.Sku,
			&deadView.Quantity,
			&deadView.ValueAtCost,
			&lastSoldText,
		)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan dead stock: %w", scanError)
		}
		deadView.LastSoldAt = parseStoredTime(lastSoldText)
		deadViews = append(deadViews, deadView)
	}
	return deadViews, deadRows.Err()
}

func (repository *Repository) RecentSales(ctx context.Context, querier database.Querier, scope Scope, limit int) ([]RecentSaleView, error) {
	arguments := &queryArguments{}
	query := `
		SELECT s.id, s.receipt_number, sh.name, u.name, s.total, s.created_at
		FROM sales s
		JOIN shops sh ON sh.company_id = s.company_id AND sh.id = s.shop_id
		JOIN users u ON u.company_id = s.company_id AND u.id = s.user_id
		WHERE s.company_id = ` + arguments.add(scope.CompanyId) + shopCondition("s.shop_id", scope, arguments) + `
		ORDER BY s.created_at DESC, s.id DESC
		LIMIT ` + arguments.add(limit)

	recentRows, queryError := querier.QueryContext(ctx, query, arguments.values...)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list recent sales: %w", queryError)
	}
	defer recentRows.Close()

	recentViews := []RecentSaleView{}
	for recentRows.Next() {
		recentView := RecentSaleView{}
		scanError := recentRows.Scan(&recentView.Id, &recentView.ReceiptNumber, &recentView.ShopName, &recentView.CashierName, &recentView.Total, &recentView.CreatedAt)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan a recent sale: %w", scanError)
		}
		recentViews = append(recentViews, recentView)
	}
	return recentViews, recentRows.Err()
}

func (repository *Repository) AssignedShopIds(ctx context.Context, querier database.Querier, companyId uuid.UUID, userId uuid.UUID) ([]uuid.UUID, error) {
	query := `SELECT shop_id FROM user_shops WHERE company_id = $1 AND user_id = $2 ORDER BY shop_id`

	shopRows, queryError := querier.QueryContext(ctx, query, companyId, userId)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list assigned shops: %w", queryError)
	}
	defer shopRows.Close()

	shopIds := []uuid.UUID{}
	for shopRows.Next() {
		shopId := uuid.UUID{}
		scanError := shopRows.Scan(&shopId)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan an assigned shop: %w", scanError)
		}
		shopIds = append(shopIds, shopId)
	}
	return shopIds, shopRows.Err()
}

func (repository *Repository) ShopExists(ctx context.Context, querier database.Querier, companyId uuid.UUID, shopId uuid.UUID) (bool, error) {
	query := `SELECT COUNT(*) FROM shops WHERE company_id = $1 AND id = $2`

	shopCount := 0
	scanError := querier.QueryRowContext(ctx, query, companyId, shopId).Scan(&shopCount)
	if scanError != nil {
		return false, fmt.Errorf("failed to find the shop: %w", scanError)
	}
	return shopCount == 1, nil
}

func (repository *Repository) ShopNames(ctx context.Context, querier database.Querier, scope Scope) ([]string, error) {
	arguments := &queryArguments{}
	query := `SELECT name FROM shops WHERE company_id = ` + arguments.add(scope.CompanyId) + shopCondition("id", scope, arguments) + ` ORDER BY name`

	nameRows, queryError := querier.QueryContext(ctx, query, arguments.values...)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list shop names: %w", queryError)
	}
	defer nameRows.Close()

	shopNames := []string{}
	for nameRows.Next() {
		shopName := ""
		scanError := nameRows.Scan(&shopName)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan a shop name: %w", scanError)
		}
		shopNames = append(shopNames, shopName)
	}
	return shopNames, nameRows.Err()
}

var storedTimeLayouts = []string{time.RFC3339Nano, "2006-01-02 15:04:05.999999999-07:00", "2006-01-02 15:04:05.999999999Z07:00", "2006-01-02 15:04:05"}

func parseStoredTime(storedText sql.NullString) *time.Time {
	if !storedText.Valid {
		return nil
	}
	for _, layout := range storedTimeLayouts {
		parsedTime, parseError := time.Parse(layout, storedText.String)
		if parseError == nil {
			utcTime := parsedTime.UTC()
			return &utcTime
		}
	}
	return nil
}

func averageOf(total int64, count int64) int64 {
	if count == 0 {
		return 0
	}
	return (total + count/2) / count
}
