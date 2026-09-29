package reports

import (
	"context"
	"errors"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/identity"
	"github.com/chrisostomemataba/balceinv-api/internal/documents"
)

const (
	ReportSummary   = "summary"
	ReportDaily     = "daily"
	ReportProducts  = "products"
	ReportCashiers  = "cashiers"
	ReportShops     = "shops"
	ReportInventory = "inventory"
)

var ErrUnknownReport = errors.New("choose summary, daily, products, cashiers, shops or inventory")

type ExportRequest struct {
	Report   string
	Format   string
	Language string
	Sort     string
	Range    RangeRequest
}

type exportContext struct {
	branding documents.Branding
	language string
	scope    Scope
}

var exportLabels = map[string]map[string]string{
	documents.English: {
		"title.summary":   "Sales report",
		"title.daily":     "Sales per day",
		"title.products":  "Products sold",
		"title.cashiers":  "Sales per staff member",
		"title.shops":     "Sales per shop",
		"title.inventory": "Stock report",
		"sheet.days":      "Days",
		"sheet.products":  "Products",
		"sheet.staff":     "Staff",
		"sheet.shops":     "Shops",
		"sheet.stock":     "Not selling",
		"date":            "Date",
		"sales":           "Sales",
		"takings":         "Takings",
		"takingsInclTax":  "Takings (incl. tax)",
		"tax":             "Tax",
		"netSales":        "Net sales",
		"cost":            "Cost",
		"costOfGoods":     "Cost of goods",
		"grossProfit":     "Gross profit",
		"margin":          "Margin",
		"product":         "Product",
		"sku":             "SKU",
		"quantity":        "Quantity",
		"itemsSold":       "Items sold",
		"staff":           "Staff",
		"averageSale":     "Average sale",
		"shop":            "Shop",
		"shops":           "Shops",
		"allShops":        "All shops",
		"discountsGiven":  "Discounts given",
		"cash":            "Cash",
		"card":            "Card",
		"mobileMoney":     "Mobile money",
		"onHand":          "On hand",
		"valueAtCost":     "Value at cost",
		"lastSold":        "Last sold",
		"neverSold":       "Never sold",
		"stockValueCost":  "Stock value at cost",
		"stockValuePrice": "Stock value at price",
		"productsInStock": "Products",
		"unitsInStock":    "Items on hand",
		"runningLow":      "Running low",
		"outOfStock":      "Out of stock",
		"rankedBy":        "Ranked by",
		"sort.revenue":    "Sales",
		"sort.quantity":   "Quantity",
		"sort.profit":     "Profit",
		"notSoldFor":      "Not sold for (days)",
		"stockAsAt":       "Stock as at {date}",
		"period":          "Period",
	},
	documents.Swahili: {
		"title.summary":   "Ripoti ya mauzo",
		"title.daily":     "Mauzo kwa siku",
		"title.products":  "Bidhaa zilizouzwa",
		"title.cashiers":  "Mauzo kwa mfanyakazi",
		"title.shops":     "Mauzo kwa duka",
		"title.inventory": "Ripoti ya stoku",
		"sheet.days":      "Siku",
		"sheet.products":  "Bidhaa",
		"sheet.staff":     "Wafanyakazi",
		"sheet.shops":     "Maduka",
		"sheet.stock":     "Bidhaa zisizouzwa",
		"date":            "Tarehe",
		"sales":           "Mauzo",
		"takings":         "Makusanyo",
		"takingsInclTax":  "Makusanyo (pamoja na kodi)",
		"tax":             "Kodi",
		"netSales":        "Mauzo halisi",
		"cost":            "Gharama",
		"costOfGoods":     "Gharama ya bidhaa",
		"grossProfit":     "Faida ghafi",
		"margin":          "Asilimia ya faida",
		"product":         "Bidhaa",
		"sku":             "SKU",
		"quantity":        "Idadi",
		"itemsSold":       "Vipande vilivyouzwa",
		"staff":           "Mfanyakazi",
		"averageSale":     "Wastani wa mauzo",
		"shop":            "Duka",
		"shops":           "Maduka",
		"allShops":        "Maduka yote",
		"discountsGiven":  "Punguzo lililotolewa",
		"cash":            "Taslimu",
		"card":            "Kadi",
		"mobileMoney":     "Pesa ya simu",
		"onHand":          "Zilizopo",
		"valueAtCost":     "Thamani kwa bei ya kununua",
		"lastSold":        "Iliuzwa mwisho",
		"neverSold":       "Haijawahi kuuzwa",
		"stockValueCost":  "Thamani ya stoku kwa bei ya kununua",
		"stockValuePrice": "Thamani ya stoku kwa bei ya kuuza",
		"productsInStock": "Bidhaa",
		"unitsInStock":    "Vipande vilivyopo",
		"runningLow":      "Stoku ndogo",
		"outOfStock":      "Zimeisha",
		"rankedBy":        "Zimepangwa kwa",
		"sort.revenue":    "Mauzo",
		"sort.quantity":   "Idadi",
		"sort.profit":     "Faida",
		"notSoldFor":      "Hazijauzwa kwa (siku)",
		"stockAsAt":       "Stoku kufikia {date}",
		"period":          "Kipindi",
	},
}

func (service *Service) Export(ctx context.Context, querier database.Querier, principal *identity.Principal, request ExportRequest) (documents.File, error) {
	exportFormat, formatError := documents.NormalizeFormat(request.Format)
	if formatError != nil {
		return documents.File{}, formatError
	}
	isKnownReport := map[string]bool{ReportSummary: true, ReportDaily: true, ReportProducts: true, ReportCashiers: true, ReportShops: true, ReportInventory: true}[request.Report]
	if !isKnownReport {
		return documents.File{}, ErrUnknownReport
	}
	productSort := request.Sort
	if productSort == "" {
		productSort = "revenue"
	}
	_, isKnownSort := productOrderColumns[productSort]
	if !isKnownSort {
		return documents.File{}, ErrInvalidSort
	}

	rangeRequest := request.Range
	if request.Report == ReportInventory {
		rangeRequest = RangeRequest{Shop: request.Range.Shop}
	}
	scope, scopeError := service.scope(ctx, querier, principal, rangeRequest)
	if scopeError != nil {
		return documents.File{}, scopeError
	}
	branding, brandingError := documents.LoadBranding(ctx, querier, service.objectStore, principal.CompanyId)
	if brandingError != nil {
		return documents.File{}, brandingError
	}
	viewer, viewerError := documents.LoadViewer(ctx, querier, principal.CompanyId, principal.UserId)
	if viewerError != nil {
		return documents.File{}, viewerError
	}
	exporting := exportContext{
		branding: branding,
		language: documents.ResolveLanguage(request.Language, viewer.Language, branding.DefaultLanguage),
		scope:    scope,
	}
	shopsText, shopsError := service.shopsText(ctx, querier, exporting)
	if shopsError != nil {
		return documents.File{}, shopsError
	}
	generatedAt := time.Now()
	document := documents.Document{
		Language:    exporting.language,
		Title:       exporting.label("title." + request.Report),
		Subtitle:    documents.PeriodText(exporting.language, scope.FirstDay, scope.LastDay),
		GeneratedBy: viewer.Name,
		GeneratedAt: generatedAt,
		Filters: []documents.Field{
			{Label: exporting.label("period"), Value: documents.PeriodText(exporting.language, scope.FirstDay, scope.LastDay)},
			{Label: exporting.label("shops"), Value: shopsText},
		},
	}
	if request.Report == ReportInventory {
		stockAsAt := documents.FormatDateTime(exporting.language, generatedAt.In(branding.Location()))
		document.Subtitle = strings.ReplaceAll(exporting.label("stockAsAt"), "{date}", stockAsAt)
		document.Filters = document.Filters[1:]
	}

	fillError := service.fillExport(ctx, querier, principal, request.Report, productSort, rangeRequest, exporting, &document)
	if fillError != nil {
		return documents.File{}, fillError
	}
	return documents.Render(branding, document, exportFormat, exportFileName(request.Report, scope))
}

func (service *Service) fillExport(ctx context.Context, querier database.Querier, principal *identity.Principal, report string, productSort string, rangeRequest RangeRequest, exporting exportContext, document *documents.Document) error {
	if report == ReportSummary || report == ReportDaily {
		summary, summaryError := service.summaryFor(ctx, querier, exporting.scope)
		if summaryError != nil {
			return summaryError
		}
		document.Cards = exporting.summaryCards(summary, report == ReportSummary)
	}
	if report == ReportSummary || report == ReportDaily {
		dayViews, dailyError := service.repository.Daily(ctx, querier, exporting.scope, dayBucketsFor(exporting.scope))
		if dailyError != nil {
			return dailyError
		}
		document.Tables = append(document.Tables, exporting.daysTable(dayViews))
	}
	if report == ReportSummary || report == ReportProducts {
		productViews, productsError := service.repository.Products(ctx, querier, exporting.scope, productOrderColumns[productSort], maximumProductLimit)
		if productsError != nil {
			return productsError
		}
		document.Tables = append(document.Tables, exporting.productsTable(productViews))
		document.Filters = append(document.Filters, documents.Field{Label: exporting.label("rankedBy"), Value: exporting.label("sort." + productSort)})
	}
	if report == ReportSummary || report == ReportCashiers {
		cashierViews, cashiersError := service.repository.Cashiers(ctx, querier, exporting.scope)
		if cashiersError != nil {
			return cashiersError
		}
		document.Tables = append(document.Tables, exporting.staffTable(cashierViews))
	}
	if report == ReportSummary || report == ReportShops {
		shopViews, shopsError := service.repository.Shops(ctx, querier, exporting.scope)
		if shopsError != nil {
			return shopsError
		}
		document.Tables = append(document.Tables, exporting.shopsTable(shopViews))
	}
	if report == ReportSummary || report == ReportInventory {
		inventoryView, inventoryError := service.Inventory(ctx, querier, principal, rangeRequest.Shop)
		if inventoryError != nil {
			return inventoryError
		}
		document.Cards = append(document.Cards, exporting.stockCards(inventoryView.Stock)...)
		document.Tables = append(document.Tables, exporting.deadStockTable(inventoryView.DeadStock))
		document.Filters = append(document.Filters, documents.Field{Label: exporting.label("notSoldFor"), Value: int64(inventoryView.DeadStockDays), Kind: documents.Integer})
	}
	return nil
}

func (exporting exportContext) label(key string) string {
	labelText, isKnown := exportLabels[exporting.language][key]
	if !isKnown {
		return exportLabels[documents.English][key]
	}
	return labelText
}

func (exporting exportContext) moneyTitle(key string) string {
	if exporting.branding.CurrencyCode == "" {
		return exporting.label(key)
	}
	return exporting.label(key) + " (" + exporting.branding.CurrencyCode + ")"
}

func (exporting exportContext) summaryCards(summary SummaryView, includesPayments bool) []documents.Field {
	summaryCards := []documents.Field{
		{Label: exporting.label("takingsInclTax"), Value: summary.Total, Kind: documents.Money},
		{Label: exporting.label("grossProfit"), Value: summary.GrossProfit, Kind: documents.Money},
		{Label: exporting.label("netSales"), Value: summary.NetSales, Kind: documents.Money},
		{Label: exporting.label("margin"), Value: summary.MarginBasisPoints, Kind: documents.Percent},
		{Label: exporting.label("sales"), Value: summary.SaleCount, Kind: documents.Integer},
		{Label: exporting.label("itemsSold"), Value: summary.UnitsSold, Kind: documents.Integer},
		{Label: exporting.label("averageSale"), Value: summary.AverageSale, Kind: documents.Money},
		{Label: exporting.label("discountsGiven"), Value: summary.DiscountTotal, Kind: documents.Money},
		{Label: exporting.label("tax"), Value: summary.TaxTotal, Kind: documents.Money},
		{Label: exporting.label("costOfGoods"), Value: summary.CostTotal, Kind: documents.Money},
	}
	if !includesPayments {
		return summaryCards
	}
	return append(summaryCards,
		documents.Field{Label: exporting.label("cash"), Value: summary.Payments.Cash, Kind: documents.Money},
		documents.Field{Label: exporting.label("card"), Value: summary.Payments.Card, Kind: documents.Money},
		documents.Field{Label: exporting.label("mobileMoney"), Value: summary.Payments.Mobile, Kind: documents.Money},
	)
}

func (exporting exportContext) stockCards(stockTotals StockTotalsView) []documents.Field {
	return []documents.Field{
		{Label: exporting.label("stockValueCost"), Value: stockTotals.ValueAtCost, Kind: documents.Money},
		{Label: exporting.label("stockValuePrice"), Value: stockTotals.ValueAtPrice, Kind: documents.Money},
		{Label: exporting.label("productsInStock"), Value: stockTotals.ProductCount, Kind: documents.Integer},
		{Label: exporting.label("unitsInStock"), Value: stockTotals.Units, Kind: documents.Integer},
		{Label: exporting.label("runningLow"), Value: stockTotals.LowCount, Kind: documents.Integer},
		{Label: exporting.label("outOfStock"), Value: stockTotals.OutCount, Kind: documents.Integer},
	}
}

func (exporting exportContext) daysTable(dayViews []DayView) documents.Table {
	dayRows := [][]any{}
	for _, dayView := range dayViews {
		dayDate, parseError := time.ParseInLocation(dateLayout, dayView.Date, exporting.scope.Location)
		var dateCell any = dayView.Date
		if parseError == nil {
			dateCell = dayDate
		}
		netSales := dayView.Total - dayView.TaxTotal
		dayRows = append(dayRows, []any{dateCell, dayView.SaleCount, dayView.Total, dayView.TaxTotal, dayView.CostTotal, dayView.GrossProfit, marginOf(dayView.GrossProfit, netSales)})
	}
	return documents.Table{
		Title: exporting.label("sheet.days"),
		Columns: []documents.Column{
			{Title: exporting.label("date"), Kind: documents.Date},
			{Title: exporting.label("sales"), Kind: documents.Integer, Sum: true},
			{Title: exporting.moneyTitle("takings"), Kind: documents.Money, Sum: true},
			{Title: exporting.moneyTitle("tax"), Kind: documents.Money, Sum: true},
			{Title: exporting.moneyTitle("cost"), Kind: documents.Money, Sum: true},
			{Title: exporting.moneyTitle("grossProfit"), Kind: documents.Money, Sum: true},
			{Title: exporting.label("margin"), Kind: documents.Percent},
		},
		Rows: dayRows,
	}
}

func (exporting exportContext) productsTable(productViews []ProductRowView) documents.Table {
	productRows := [][]any{}
	for _, productView := range productViews {
		productRows = append(productRows, []any{productLabel(productView.Name, productView.VariantLabel), productView.Sku, productView.Quantity, productView.Revenue, productView.NetRevenue, productView.CostTotal, productView.GrossProfit, marginOf(productView.GrossProfit, productView.NetRevenue)})
	}
	return documents.Table{
		Title: exporting.label("sheet.products"),
		Columns: []documents.Column{
			{Title: exporting.label("product"), Kind: documents.Text},
			{Title: exporting.label("sku"), Kind: documents.Text},
			{Title: exporting.label("quantity"), Kind: documents.Integer, Sum: true},
			{Title: exporting.moneyTitle("takings"), Kind: documents.Money, Sum: true},
			{Title: exporting.moneyTitle("netSales"), Kind: documents.Money, Sum: true},
			{Title: exporting.moneyTitle("cost"), Kind: documents.Money, Sum: true},
			{Title: exporting.moneyTitle("grossProfit"), Kind: documents.Money, Sum: true},
			{Title: exporting.label("margin"), Kind: documents.Percent},
		},
		Rows: productRows,
	}
}

func (exporting exportContext) staffTable(cashierViews []CashierRowView) documents.Table {
	staffRows := [][]any{}
	for _, cashierView := range cashierViews {
		staffRows = append(staffRows, []any{cashierView.Name, cashierView.SaleCount, cashierView.Total, cashierView.AverageSale})
	}
	return documents.Table{
		Title: exporting.label("sheet.staff"),
		Columns: []documents.Column{
			{Title: exporting.label("staff"), Kind: documents.Text},
			{Title: exporting.label("sales"), Kind: documents.Integer, Sum: true},
			{Title: exporting.moneyTitle("takings"), Kind: documents.Money, Sum: true},
			{Title: exporting.moneyTitle("averageSale"), Kind: documents.Money},
		},
		Rows: staffRows,
	}
}

func (exporting exportContext) shopsTable(shopViews []ShopRowView) documents.Table {
	shopRows := [][]any{}
	for _, shopView := range shopViews {
		shopRows = append(shopRows, []any{shopView.Name, shopView.SaleCount, shopView.Total, shopView.TaxTotal, shopView.CostTotal, shopView.GrossProfit})
	}
	return documents.Table{
		Title: exporting.label("sheet.shops"),
		Columns: []documents.Column{
			{Title: exporting.label("shop"), Kind: documents.Text},
			{Title: exporting.label("sales"), Kind: documents.Integer, Sum: true},
			{Title: exporting.moneyTitle("takings"), Kind: documents.Money, Sum: true},
			{Title: exporting.moneyTitle("tax"), Kind: documents.Money, Sum: true},
			{Title: exporting.moneyTitle("cost"), Kind: documents.Money, Sum: true},
			{Title: exporting.moneyTitle("grossProfit"), Kind: documents.Money, Sum: true},
		},
		Rows: shopRows,
	}
}

func (exporting exportContext) deadStockTable(deadViews []DeadStockView) documents.Table {
	deadRows := [][]any{}
	for _, deadView := range deadViews {
		var lastSoldCell any = exporting.label("neverSold")
		if deadView.LastSoldAt != nil {
			lastSoldCell = *deadView.LastSoldAt
		}
		deadRows = append(deadRows, []any{productLabel(deadView.Name, deadView.VariantLabel), deadView.Sku, deadView.Quantity, deadView.ValueAtCost, lastSoldCell})
	}
	return documents.Table{
		Title: exporting.label("sheet.stock"),
		Columns: []documents.Column{
			{Title: exporting.label("product"), Kind: documents.Text},
			{Title: exporting.label("sku"), Kind: documents.Text},
			{Title: exporting.label("onHand"), Kind: documents.Integer, Sum: true},
			{Title: exporting.moneyTitle("valueAtCost"), Kind: documents.Money, Sum: true},
			{Title: exporting.label("lastSold"), Kind: documents.Date},
		},
		Rows: deadRows,
	}
}

func (service *Service) shopsText(ctx context.Context, querier database.Querier, exporting exportContext) (string, error) {
	if exporting.scope.AllShops {
		return exporting.label("allShops"), nil
	}
	shopNames, namesError := service.repository.ShopNames(ctx, querier, exporting.scope)
	if namesError != nil {
		return "", namesError
	}
	return strings.Join(shopNames, ", "), nil
}

func exportFileName(report string, scope Scope) string {
	if report == ReportInventory {
		return "stock-report-" + scope.ToDate
	}
	if report == ReportSummary {
		return "report-" + scope.FromDate + "-to-" + scope.ToDate
	}
	return "report-" + report + "-" + scope.FromDate + "-to-" + scope.ToDate
}

func productLabel(productName string, variantLabel string) string {
	if strings.TrimSpace(variantLabel) == "" {
		return productName
	}
	return productName + " · " + variantLabel
}

func marginOf(grossProfit int64, netSales int64) any {
	if netSales <= 0 {
		return nil
	}
	return (grossProfit*10000 + netSales/2) / netSales
}
