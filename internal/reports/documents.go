package reports

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/identity"
	"github.com/chrisostomemataba/balceinv-api/internal/documents"
)

const (
	documentProductLimit   = 5000
	documentDeadStockLimit = 5000
)

type reportHeading struct {
	title       string
	subtitle    string
	filters     []documents.Field
	generatedBy string
	generatedAt time.Time
}

func (service *Service) Export(ctx context.Context, querier database.Querier, principal *identity.Principal, request ExportRequest) (documents.File, error) {
	exportFormat, formatError := documents.NormalizeFormat(request.Format)
	if formatError != nil {
		return documents.File{}, formatError
	}
	isKnownReport := map[string]bool{ReportSummary: true, ReportDaily: true, ReportProducts: true, ReportCashiers: true, ReportShops: true, ReportInventory: true, ReportDeadStock: true}[request.Report]
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

	isStockReport := request.Report == ReportInventory || request.Report == ReportDeadStock
	rangeRequest := request.Range
	if isStockReport {
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

	heading := reportHeading{
		title:       exporting.label("title." + request.Report),
		subtitle:    documents.PeriodText(exporting.language, scope.FirstDay, scope.LastDay),
		filters:     []documents.Field{{Label: exporting.label("shops"), Value: shopsText}},
		generatedBy: viewer.Name,
		generatedAt: time.Now(),
	}
	if isStockReport {
		stockAsAt := documents.FormatDateTime(exporting.language, heading.generatedAt.In(branding.Location()))
		heading.subtitle = strings.ReplaceAll(exporting.label("stockAsAt"), "{date}", stockAsAt)
	}
	fileName := exportFileName(request.Report, scope)

	if request.Report == ReportSummary {
		statement, statementError := service.salesSummaryStatement(ctx, querier, exporting, heading)
		if statementError != nil {
			return documents.File{}, statementError
		}
		return documents.RenderStatement(branding, statement, exportFormat, fileName)
	}
	register, registerError := service.reportRegister(ctx, querier, principal, request.Report, productSort, exporting, heading)
	if registerError != nil {
		return documents.File{}, registerError
	}
	return documents.RenderRegister(branding, register, exportFormat, fileName)
}

func (service *Service) salesSummaryStatement(ctx context.Context, querier database.Querier, exporting exportContext, heading reportHeading) (documents.Statement, error) {
	current, currentError := service.summaryFor(ctx, querier, exporting.scope)
	if currentError != nil {
		return documents.Statement{}, currentError
	}
	previousFirstDay, previousLastDay := documents.PreviousPeriod(exporting.scope.FirstDay, exporting.scope.LastDay)
	previous, previousError := service.summaryFor(ctx, querier, rangeScope(exporting.scope, previousFirstDay, previousLastDay))
	if previousError != nil {
		return documents.Statement{}, previousError
	}

	showsVat := current.TaxTotal != 0 || previous.TaxTotal != 0
	netTerms := []documents.StatementTerm{{Key: "takings"}}
	statementRows := []documents.StatementRow{
		{Style: documents.StatementHeading, Label: exporting.label("sumTakings")},
		{Key: "takings", Style: documents.StatementLine, Label: exporting.label("sumTakingsInclVat"), Current: current.Total, Previous: previous.Total},
	}
	if showsVat {
		statementRows = append(statementRows, documents.StatementRow{Key: "vat", Style: documents.StatementLine, Label: exporting.label("sumLessVat"), Current: -current.TaxTotal, Previous: -previous.TaxTotal, IsDeduction: true})
		netTerms = append(netTerms, documents.StatementTerm{Key: "vat"})
	}
	grossLabel := "sumGrossProfit"
	if current.GrossProfit < 0 {
		grossLabel = "sumGrossLoss"
	}
	statementRows = append(statementRows,
		documents.StatementRow{Key: "net", Style: documents.StatementSubtotal, Label: exporting.label("sumNetSales"), Current: current.NetSales, Previous: previous.NetSales, Terms: netTerms},
		documents.StatementRow{Key: "cost", Style: documents.StatementLine, Label: exporting.label("sumLessCost"), Current: -current.CostTotal, Previous: -previous.CostTotal, IsDeduction: true},
		documents.StatementRow{Key: "gross", Style: documents.StatementTotal, Label: exporting.label(grossLabel), Current: current.GrossProfit, Previous: previous.GrossProfit, Terms: []documents.StatementTerm{{Key: "net"}, {Key: "cost"}}},
		documents.StatementRow{Style: documents.StatementRatio, Label: exporting.label("sumGrossMargin"), RatioNumerator: "gross", RatioDenominator: "net"},
		documents.StatementRow{Style: documents.StatementHeading, Label: exporting.label("sumPaid")},
		documents.StatementRow{Style: documents.StatementItem, Label: exporting.label("cash"), Current: current.Payments.Cash, Previous: previous.Payments.Cash},
		documents.StatementRow{Style: documents.StatementItem, Label: exporting.label("mobileMoney"), Current: current.Payments.Mobile, Previous: previous.Payments.Mobile},
		documents.StatementRow{Style: documents.StatementItem, Label: exporting.label("card"), Current: current.Payments.Card, Previous: previous.Payments.Card},
		documents.StatementRow{Style: documents.StatementItem, Label: exporting.label("payLater"), Current: current.Payments.Credit, Previous: previous.Payments.Credit},
		documents.StatementRow{Style: documents.StatementSubtotal, Label: exporting.label("sumTotalPaid"), Current: paymentsTotal(current.Payments), Previous: paymentsTotal(previous.Payments), SumsSection: true},
		documents.StatementRow{Style: documents.StatementHeading, Label: exporting.label("sumOther")},
		documents.StatementRow{Style: documents.StatementLine, Label: exporting.label("sumDiscounts"), Current: current.DiscountTotal, Previous: previous.DiscountTotal},
		documents.StatementRow{Style: documents.StatementLine, Label: exporting.label("averageSale"), Current: current.AverageSale, Previous: previous.AverageSale},
	)

	notes := []string{exporting.label("sumNoteTakings"), exporting.label("sumNoteCost")}
	if current.Payments.Credit != 0 || previous.Payments.Credit != 0 {
		notes = append(notes, exporting.label("sumNoteCredit"))
	}
	filters := append(heading.filters,
		documents.Field{Label: exporting.label("comparedWith"), Value: documents.PeriodText(exporting.language, previousFirstDay, previousLastDay)},
		documents.Field{Label: exporting.label("sales"), Value: current.SaleCount, Kind: documents.Integer},
		documents.Field{Label: exporting.label("itemsSold"), Value: current.UnitsSold, Kind: documents.Integer},
	)
	return documents.Statement{
		Language:        exporting.language,
		Title:           heading.title,
		Subtitle:        heading.subtitle,
		CurrentHeading:  documents.PeriodHeading(exporting.language, exporting.scope.FirstDay, exporting.scope.LastDay, false),
		PreviousHeading: documents.PeriodHeading(exporting.language, previousFirstDay, previousLastDay, true),
		Filters:         filters,
		GeneratedBy:     heading.generatedBy,
		GeneratedAt:     heading.generatedAt,
		Rows:            statementRows,
		Notes:           notes,
		SheetName:       heading.title,
	}, nil
}

func paymentsTotal(payments PaymentTotalsView) int64 {
	return payments.Cash + payments.Mobile + payments.Card + payments.Credit
}

func (service *Service) reportRegister(ctx context.Context, querier database.Querier, principal *identity.Principal, report string, productSort string, exporting exportContext, heading reportHeading) (documents.Register, error) {
	register := documents.Register{
		Language:    exporting.language,
		Title:       heading.title,
		Subtitle:    heading.subtitle,
		Filters:     heading.filters,
		GeneratedBy: heading.generatedBy,
		GeneratedAt: heading.generatedAt,
		TotalLabel:  exporting.label("total"),
		SheetName:   heading.title,
	}
	switch report {
	case ReportDaily:
		return register, service.fillDaily(ctx, querier, exporting, &register)
	case ReportProducts:
		return register, service.fillProducts(ctx, querier, exporting, productSort, &register)
	case ReportCashiers:
		return register, service.fillStaff(ctx, querier, exporting, &register)
	case ReportShops:
		return register, service.fillShops(ctx, querier, exporting, &register)
	case ReportInventory:
		return register, service.fillStock(ctx, querier, exporting, &register)
	default:
		return register, service.fillDeadStock(ctx, querier, principal, exporting, &register)
	}
}

func (service *Service) fillDaily(ctx context.Context, querier database.Querier, exporting exportContext, register *documents.Register) error {
	dayViews, dailyError := service.repository.Daily(ctx, querier, exporting.scope, dayBucketsFor(exporting.scope))
	if dailyError != nil {
		return dailyError
	}
	summary, summaryError := service.summaryFor(ctx, querier, exporting.scope)
	if summaryError != nil {
		return summaryError
	}
	showsVat := summary.TaxTotal != 0
	register.Columns = []documents.RegisterColumn{
		{Title: exporting.label("date"), Kind: documents.Date, Weight: 1.6},
		{Title: exporting.label("sales"), Kind: documents.Integer, Sum: true},
		{Title: exporting.moneyTitle("takings"), Kind: documents.Money, Sum: true},
	}
	if showsVat {
		register.Columns = append(register.Columns, documents.RegisterColumn{Title: exporting.moneyTitle("vat"), Kind: documents.Money, Sum: true})
	}
	register.Columns = append(register.Columns,
		documents.RegisterColumn{Title: exporting.moneyTitle("costOfGoods"), Kind: documents.Money, Sum: true},
		documents.RegisterColumn{Title: exporting.moneyTitle("grossProfit"), Kind: documents.Money, Sum: true},
		documents.RegisterColumn{Title: exporting.label("margin"), Kind: documents.Percent},
	)
	for _, dayView := range dayViews {
		dayDate, parseError := time.ParseInLocation(dateLayout, dayView.Date, exporting.scope.Location)
		var dateCell any = dayView.Date
		if parseError == nil {
			dateCell = dayDate
		}
		cells := []any{dateCell, dayView.SaleCount, dayView.Total}
		if showsVat {
			cells = append(cells, dayView.TaxTotal)
		}
		cells = append(cells, dayView.CostTotal, dayView.GrossProfit, marginOf(dayView.GrossProfit, dayView.Total-dayView.TaxTotal))
		register.Rows = append(register.Rows, documents.RegisterRow{Cells: cells})
	}
	register.Summary = []documents.Field{
		{Label: exporting.label("takings"), Value: summary.Total, Kind: documents.Money},
		{Label: exporting.label("grossProfit"), Value: summary.GrossProfit, Kind: documents.Money, Strong: true},
		{Label: exporting.label("sales"), Value: summary.SaleCount, Kind: documents.Integer},
		{Label: exporting.label("margin"), Value: summary.MarginBasisPoints, Kind: documents.Percent},
	}
	register.EmptyText = exporting.label("emptySales")
	register.Notes = []string{exporting.label("dailyNote"), exporting.label("sumNoteCost")}
	return nil
}

func (service *Service) fillProducts(ctx context.Context, querier database.Querier, exporting exportContext, productSort string, register *documents.Register) error {
	productViews, productsError := service.repository.Products(ctx, querier, exporting.scope, productOrderColumns[productSort], documentProductLimit)
	if productsError != nil {
		return productsError
	}
	register.Landscape = true
	register.Columns = []documents.RegisterColumn{
		{Title: exporting.label("product"), Kind: documents.Text, Weight: 3.6},
		{Title: exporting.label("sku"), Kind: documents.Text, Weight: 1.3},
		{Title: exporting.label("quantity"), Kind: documents.Integer, Sum: true},
		{Title: exporting.moneyTitle("takings"), Kind: documents.Money, Sum: true},
		{Title: exporting.moneyTitle("netSales"), Kind: documents.Money, Sum: true},
		{Title: exporting.moneyTitle("costOfGoods"), Kind: documents.Money, Sum: true},
		{Title: exporting.moneyTitle("grossProfit"), Kind: documents.Money, Sum: true},
		{Title: exporting.label("margin"), Kind: documents.Percent},
	}
	for _, productView := range productViews {
		register.Rows = append(register.Rows, documents.RegisterRow{Cells: []any{productLabel(productView.Name, productView.VariantLabel), productView.Sku, productView.Quantity, productView.Revenue, productView.NetRevenue, productView.CostTotal, productView.GrossProfit, marginOf(productView.GrossProfit, productView.NetRevenue)}})
	}
	sortName := exporting.label("sort." + productSort)
	register.Filters = append(register.Filters, documents.Field{Label: exporting.label("rankedBy"), Value: sortName})
	register.EmptyText = exporting.label("emptySales")
	register.Notes = []string{strings.ReplaceAll(exporting.label("productsNote"), "{sort}", strings.ToLower(sortName)), exporting.label("sumNoteCost")}
	return nil
}

func (service *Service) fillStaff(ctx context.Context, querier database.Querier, exporting exportContext, register *documents.Register) error {
	cashierViews, cashiersError := service.repository.Cashiers(ctx, querier, exporting.scope)
	if cashiersError != nil {
		return cashiersError
	}
	register.Columns = []documents.RegisterColumn{
		{Title: exporting.label("staff"), Kind: documents.Text, Weight: 3.4},
		{Title: exporting.label("sales"), Kind: documents.Integer, Sum: true},
		{Title: exporting.moneyTitle("takings"), Kind: documents.Money, Sum: true},
		{Title: exporting.moneyTitle("averageSale"), Kind: documents.Money},
	}
	for _, cashierView := range cashierViews {
		register.Rows = append(register.Rows, documents.RegisterRow{Cells: []any{cashierView.Name, cashierView.SaleCount, cashierView.Total, cashierView.AverageSale}})
	}
	register.EmptyText = exporting.label("emptySales")
	register.Notes = []string{exporting.label("staffNote")}
	return nil
}

func (service *Service) fillShops(ctx context.Context, querier database.Querier, exporting exportContext, register *documents.Register) error {
	shopViews, shopsError := service.repository.Shops(ctx, querier, exporting.scope)
	if shopsError != nil {
		return shopsError
	}
	register.Columns = []documents.RegisterColumn{
		{Title: exporting.label("shop"), Kind: documents.Text, Weight: 3},
		{Title: exporting.label("sales"), Kind: documents.Integer, Sum: true},
		{Title: exporting.moneyTitle("takings"), Kind: documents.Money, Sum: true},
		{Title: exporting.moneyTitle("vat"), Kind: documents.Money, Sum: true},
		{Title: exporting.moneyTitle("costOfGoods"), Kind: documents.Money, Sum: true},
		{Title: exporting.moneyTitle("grossProfit"), Kind: documents.Money, Sum: true},
	}
	for _, shopView := range shopViews {
		register.Rows = append(register.Rows, documents.RegisterRow{Cells: []any{shopView.Name, shopView.SaleCount, shopView.Total, shopView.TaxTotal, shopView.CostTotal, shopView.GrossProfit}})
	}
	register.EmptyText = exporting.label("emptySales")
	register.Notes = []string{exporting.label("shopsNote")}
	return nil
}

func (service *Service) fillStock(ctx context.Context, querier database.Querier, exporting exportContext, register *documents.Register) error {
	stockLines, stockError := service.repository.StockLines(ctx, querier, exporting.scope)
	if stockError != nil {
		return stockError
	}
	stockTotals, totalsError := service.repository.StockTotals(ctx, querier, exporting.scope)
	if totalsError != nil {
		return totalsError
	}
	register.Landscape = true
	register.Columns = []documents.RegisterColumn{
		{Title: exporting.label("product"), Kind: documents.Text, Weight: 3.4},
		{Title: exporting.label("sku"), Kind: documents.Text, Weight: 1.2},
		{Title: exporting.label("category"), Kind: documents.Text, Weight: 1.6},
		{Title: exporting.label("onHand"), Kind: documents.Integer, Sum: true},
		{Title: exporting.moneyTitle("costPrice"), Kind: documents.Money},
		{Title: exporting.moneyTitle("valueAtCost"), Kind: documents.Money, Sum: true},
		{Title: exporting.moneyTitle("sellingPrice"), Kind: documents.Money},
		{Title: exporting.moneyTitle("valueAtPrice"), Kind: documents.Money, Sum: true},
		{Title: exporting.label("status"), Kind: documents.Text, Weight: 1.4},
	}
	for _, stockLine := range stockLines {
		statusText := ""
		switch {
		case stockLine.Quantity <= 0:
			statusText = exporting.label("outOfStock")
		case stockLine.Quantity <= stockLine.MinimumStock:
			statusText = exporting.label("runningLow")
		}
		register.Rows = append(register.Rows, documents.RegisterRow{Cells: []any{productLabel(stockLine.Name, stockLine.VariantLabel), stockLine.Sku, stockLine.Category, stockLine.Quantity, stockLine.CostPrice, stockLine.ValueAtCost, stockLine.Price, stockLine.ValueAtPrice, statusText}})
	}
	register.Summary = []documents.Field{
		{Label: exporting.label("productsInStock"), Value: stockTotals.ProductCount, Kind: documents.Integer},
		{Label: exporting.label("unitsInStock"), Value: stockTotals.Units, Kind: documents.Integer},
		{Label: exporting.label("stockValueCost"), Value: stockTotals.ValueAtCost, Kind: documents.Money, Strong: true},
		{Label: exporting.label("stockValuePrice"), Value: stockTotals.ValueAtPrice, Kind: documents.Money},
	}
	register.EmptyText = exporting.label("emptyStock")
	register.Notes = []string{exporting.label("stockNote")}
	return nil
}

func (service *Service) fillDeadStock(ctx context.Context, querier database.Querier, principal *identity.Principal, exporting exportContext, register *documents.Register) error {
	companySettings, settingsError := service.settingsRepository.FindSettings(ctx, querier, principal.CompanyId)
	if settingsError != nil {
		return settingsError
	}
	if companySettings == nil {
		return ErrMissingSettings
	}
	soldBefore := time.Now().UTC().AddDate(0, 0, -companySettings.DeadStockDays)
	deadViews, deadError := service.repository.DeadStock(ctx, querier, exporting.scope, soldBefore, documentDeadStockLimit)
	if deadError != nil {
		return deadError
	}
	register.Columns = []documents.RegisterColumn{
		{Title: exporting.label("product"), Kind: documents.Text, Weight: 3.4},
		{Title: exporting.label("sku"), Kind: documents.Text, Weight: 1.2},
		{Title: exporting.label("lastSold"), Kind: documents.Date, Weight: 1.6},
		{Title: exporting.label("onHand"), Kind: documents.Integer, Sum: true},
		{Title: exporting.moneyTitle("valueAtCost"), Kind: documents.Money, Sum: true, Weight: 1.6},
	}
	valueTiedUp := int64(0)
	for _, deadView := range deadViews {
		valueTiedUp += deadView.ValueAtCost
		var lastSoldCell any = exporting.label("neverSold")
		if deadView.LastSoldAt != nil {
			lastSoldCell = *deadView.LastSoldAt
		}
		register.Rows = append(register.Rows, documents.RegisterRow{Cells: []any{productLabel(deadView.Name, deadView.VariantLabel), deadView.Sku, lastSoldCell, deadView.Quantity, deadView.ValueAtCost}})
	}
	daysText := strconv.Itoa(companySettings.DeadStockDays)
	register.Summary = []documents.Field{
		{Label: exporting.label("deadCount"), Value: int64(len(deadViews)), Kind: documents.Integer},
		{Label: exporting.label("valueAtCost"), Value: valueTiedUp, Kind: documents.Money, Strong: true},
		{Label: exporting.label("notSoldFor"), Value: int64(companySettings.DeadStockDays), Kind: documents.Integer},
	}
	register.EmptyText = exporting.label("emptyDead")
	register.Notes = []string{strings.ReplaceAll(exporting.label("deadNote"), "{days}", daysText), exporting.label("deadValueNote")}
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
	switch report {
	case ReportInventory:
		return "stock-on-hand-" + scope.ToDate
	case ReportDeadStock:
		return "stock-not-selling-" + scope.ToDate
	case ReportSummary:
		return "sales-report-" + scope.FromDate + "-to-" + scope.ToDate
	default:
		return "sales-" + report + "-" + scope.FromDate + "-to-" + scope.ToDate
	}
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
