package accounting

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
	ExportProfitAndLoss = "profit-and-loss"
	ExportBalanceSheet  = "balance-sheet"
	ExportTrialBalance  = "trial-balance"
	ExportStatement     = "statement"
	ExportVat           = "vat"
)

var ErrUnknownExport = errors.New("this report cannot be exported")

type ExportRequest struct {
	Report   string
	Format   string
	Language string
	Account  string
	Range    ReportRequest
}

var exportLabels = map[string]map[string]string{
	documents.English: {
		"title.profit-and-loss":        "Profit and loss",
		"title.balance-sheet":          "Balance sheet",
		"title.trial-balance":          "Trial balance",
		"title.statement":              "Account statement",
		"title.vat":                    "VAT report",
		"asAt":                         "As at {date}",
		"account":                      "Account",
		"code":                         "Code",
		"amount":                       "Amount",
		"income":                       "Income",
		"expenses":                     "Expenses",
		"totalIncome":                  "Total income",
		"costOfGoods":                  "Cost of goods sold",
		"grossProfit":                  "Gross profit",
		"totalExpenses":                "Total expenses",
		"netProfit":                    "Net profit",
		"assets":                       "Assets",
		"liabilities":                  "Liabilities",
		"equity":                       "Equity",
		"profitToDate":                 "Profit to date",
		"totalAssets":                  "Total assets",
		"totalLiabilities":             "Total liabilities",
		"totalEquity":                  "Total equity",
		"liabilitiesAndEquity":         "Liabilities and equity",
		"debit":                        "Debit",
		"credit":                       "Credit",
		"moneyIn":                      "Money in",
		"moneyOut":                     "Money out",
		"balance":                      "Balance",
		"date":                         "Date",
		"entry":                        "Entry",
		"description":                  "Description",
		"openingBalance":               "Opening balance",
		"closingBalance":               "Closing balance",
		"month":                        "Month",
		"vatCharged":                   "VAT charged",
		"vatReclaimable":               "VAT reclaimable",
		"vatToPay":                     "VAT to pay",
		"dueDate":                      "Pay by",
		"period":                       "Period",
		"shop":                         "Shop",
		"allShops":                     "All shops",
		"lines":                        "Lines",
		"source.sale":                  "Sale",
		"source.sale_void":             "Sale cancelled",
		"source.stock_adjustment":      "Stock change",
		"source.stock_transfer":        "Stock sent between shops",
		"source.expense":               "Money out",
		"source.owner_in":              "Owner put money in",
		"source.owner_out":             "Owner took money out",
		"source.money_move":            "Money moved",
		"source.other_income":          "Other money in",
		"source.opening":               "Starting balances",
		"source.manual":                "Manual entry",
		"source.reversal":              "Reversal",
		"source.purchase":              "Stock bought",
		"source.purchase_cancel":       "Purchase cancelled",
		"source.supplier_payment":      "Paid a supplier",
		"source.supplier_payment_void": "Supplier payment cancelled",
		"source.supplier_return":       "Returned to a supplier",
		"source.customer_payment":      "Customer paid",
		"source.customer_payment_void": "Customer payment cancelled",
		"source.order_deposit":         "Order deposit",
		"source.order_refund":          "Order deposit refunded",
	},
	documents.Swahili: {
		"title.profit-and-loss":        "Faida na hasara",
		"title.balance-sheet":          "Mizania",
		"title.trial-balance":          "Mizania ya majaribio",
		"title.statement":              "Mwenendo wa akaunti",
		"title.vat":                    "Ripoti ya VAT",
		"asAt":                         "Kufikia {date}",
		"account":                      "Akaunti",
		"code":                         "Namba",
		"amount":                       "Kiasi",
		"income":                       "Mapato",
		"expenses":                     "Matumizi",
		"totalIncome":                  "Jumla ya mapato",
		"costOfGoods":                  "Gharama ya bidhaa zilizouzwa",
		"grossProfit":                  "Faida ghafi",
		"totalExpenses":                "Jumla ya matumizi",
		"netProfit":                    "Faida halisi",
		"assets":                       "Mali",
		"liabilities":                  "Madeni",
		"equity":                       "Mtaji",
		"profitToDate":                 "Faida hadi sasa",
		"totalAssets":                  "Jumla ya mali",
		"totalLiabilities":             "Jumla ya madeni",
		"totalEquity":                  "Jumla ya mtaji",
		"liabilitiesAndEquity":         "Madeni na mtaji",
		"debit":                        "Debiti",
		"credit":                       "Krediti",
		"moneyIn":                      "Pesa iliyoingia",
		"moneyOut":                     "Pesa iliyotoka",
		"balance":                      "Salio",
		"date":                         "Tarehe",
		"entry":                        "Rekodi",
		"description":                  "Maelezo",
		"openingBalance":               "Salio la kuanzia",
		"closingBalance":               "Salio la mwisho",
		"month":                        "Mwezi",
		"vatCharged":                   "VAT iliyotozwa",
		"vatReclaimable":               "VAT ya kurudishiwa",
		"vatToPay":                     "VAT ya kulipa",
		"dueDate":                      "Lipa kabla ya",
		"period":                       "Kipindi",
		"shop":                         "Duka",
		"allShops":                     "Maduka yote",
		"lines":                        "Mistari",
		"source.sale":                  "Mauzo",
		"source.sale_void":             "Mauzo yaliyoghairiwa",
		"source.stock_adjustment":      "Marekebisho ya stoku",
		"source.stock_transfer":        "Uhamisho wa stoku",
		"source.expense":               "Pesa iliyotoka",
		"source.owner_in":              "Mtaji ulioongezwa",
		"source.owner_out":             "Pesa aliyochukua mmiliki",
		"source.money_move":            "Pesa iliyohamishwa",
		"source.other_income":          "Pesa nyingine iliyoingia",
		"source.opening":               "Salio la kuanzia",
		"source.manual":                "Ingizo la mkono",
		"source.reversal":              "Rekodi iliyobatilishwa",
		"source.purchase":              "Stoku iliyonunuliwa",
		"source.purchase_cancel":       "Ununuzi ulioghairiwa",
		"source.supplier_payment":      "Malipo kwa msambazaji",
		"source.supplier_payment_void": "Malipo kwa msambazaji yaliyoghairiwa",
		"source.supplier_return":       "Bidhaa zilizorudishwa kwa msambazaji",
		"source.customer_payment":      "Malipo ya mteja",
		"source.customer_payment_void": "Malipo ya mteja yaliyoghairiwa",
		"source.order_deposit":         "Malipo ya awali ya oda",
		"source.order_refund":          "Malipo ya awali yaliyorudishwa",
	},
}

var systemAccountNames = map[string]map[string]string{
	documents.English: {
		KeyCash: "Cash", KeyMobileMoney: "Mobile money", KeyBank: "Bank", KeyCardClearing: "Card payments (waiting for the bank)",
		KeyReceivable: "Customers owe us", KeyInventory: "Stock", KeyVatInput: "VAT reclaimable", KeyPayable: "We owe suppliers",
		KeyVatOutput: "VAT charged", KeyCustomerDeposits: "Customer deposits", KeyOwnerCapital: "Owner's capital",
		KeyOwnerDrawings: "Owner's drawings", KeyRetainedEarnings: "Retained profit", KeySales: "Sales", KeyOtherIncome: "Other income",
		KeyStockGains: "Stock gains", KeyCogs: "Cost of goods sold", KeyStockLosses: "Stock losses", KeyRent: "Rent", KeySalaries: "Salaries",
		KeyUtilities: "Electricity & water (LUKU)", KeyTransport: "Transport", KeyPhoneInternet: "Phone & internet",
		KeyMoneyCharges: "Bank & mobile money charges", KeyRepairs: "Repairs", KeyLicencesLevies: "Licences & levies",
		KeySupplies: "Shop supplies", KeyMarketing: "Marketing", KeyOtherExpenses: "Other expenses",
	},
	documents.Swahili: {
		KeyCash: "Taslimu", KeyMobileMoney: "Pesa ya simu", KeyBank: "Benki", KeyCardClearing: "Malipo ya kadi (yanasubiri benki)",
		KeyReceivable: "Wateja wanaodaiwa", KeyInventory: "Stoku", KeyVatInput: "VAT ya kurudishiwa", KeyPayable: "Madeni kwa wasambazaji",
		KeyVatOutput: "VAT iliyotozwa", KeyCustomerDeposits: "Malipo ya awali ya wateja", KeyOwnerCapital: "Mtaji wa mmiliki",
		KeyOwnerDrawings: "Pesa aliyochukua mmiliki", KeyRetainedEarnings: "Faida iliyobakizwa", KeySales: "Mauzo", KeyOtherIncome: "Mapato mengine",
		KeyStockGains: "Ongezeko la stoku", KeyCogs: "Gharama ya bidhaa zilizouzwa", KeyStockLosses: "Hasara ya stoku", KeyRent: "Kodi ya pango",
		KeySalaries: "Mishahara", KeyUtilities: "Umeme na maji (LUKU)", KeyTransport: "Usafiri", KeyPhoneInternet: "Simu na intaneti",
		KeyMoneyCharges: "Ada za benki na pesa ya simu", KeyRepairs: "Matengenezo", KeyLicencesLevies: "Leseni na ushuru",
		KeySupplies: "Vifaa vya duka", KeyMarketing: "Matangazo", KeyOtherExpenses: "Matumizi mengine",
	},
}

type exporter struct {
	branding documents.Branding
	language string
}

func (exporting exporter) label(key string) string {
	labelText, isKnown := exportLabels[exporting.language][key]
	if !isKnown {
		return exportLabels[documents.English][key]
	}
	return labelText
}

func (exporting exporter) moneyTitle(key string) string {
	if exporting.branding.CurrencyCode == "" {
		return exporting.label(key)
	}
	return exporting.label(key) + " (" + exporting.branding.CurrencyCode + ")"
}

func (exporting exporter) accountName(code string, systemKey *string, name *string) string {
	displayName := ""
	if name != nil {
		displayName = *name
	}
	if systemKey != nil {
		displayName = systemAccountNames[exporting.language][*systemKey]
	}
	if code == "" {
		return displayName
	}
	return code + " · " + displayName
}

func (exporting exporter) amountsTable(title string, rows []AccountAmountView) documents.Table {
	tableRows := [][]any{}
	for _, row := range rows {
		tableRows = append(tableRows, []any{exporting.accountName(row.Code, row.SystemKey, row.Name), row.Amount})
	}
	return documents.Table{
		Title: title,
		Columns: []documents.Column{
			{Title: exporting.label("account"), Kind: documents.Text},
			{Title: exporting.moneyTitle("amount"), Kind: documents.Money, Sum: true},
		},
		Rows: tableRows,
	}
}

func (service *Service) Export(ctx context.Context, querier database.Querier, principal *identity.Principal, request ExportRequest) (documents.File, error) {
	exportFormat, formatError := documents.NormalizeFormat(request.Format)
	if formatError != nil {
		return documents.File{}, formatError
	}
	branding, brandingError := documents.LoadBranding(ctx, querier, service.objectStore, principal.CompanyId)
	if brandingError != nil {
		return documents.File{}, brandingError
	}
	viewer, viewerError := documents.LoadViewer(ctx, querier, principal.CompanyId, principal.UserId)
	if viewerError != nil {
		return documents.File{}, viewerError
	}
	exporting := exporter{
		branding: branding,
		language: documents.ResolveLanguage(request.Language, viewer.Language, branding.DefaultLanguage),
	}

	if request.Report == ExportProfitAndLoss {
		statement, fileName, statementError := service.profitAndLossStatement(ctx, querier, principal, request, exporting, viewer.Name)
		if statementError != nil {
			return documents.File{}, statementError
		}
		return documents.RenderStatement(branding, statement, exportFormat, fileName)
	}

	document := documents.Document{
		Language:    exporting.language,
		Title:       exporting.label("title." + request.Report),
		GeneratedBy: viewer.Name,
		GeneratedAt: time.Now(),
	}
	fileName, fillError := service.fillExport(ctx, querier, principal, request, exporting, &document)
	if fillError != nil {
		return documents.File{}, fillError
	}
	return documents.Render(branding, document, exportFormat, fileName)
}

func (service *Service) fillExport(ctx context.Context, querier database.Querier, principal *identity.Principal, request ExportRequest, exporting exporter, document *documents.Document) (string, error) {
	switch request.Report {
	case ExportBalanceSheet:
		report, reportError := service.BalanceSheet(ctx, querier, principal, request.Range.ToDate)
		if reportError != nil {
			return "", reportError
		}
		document.Subtitle = exporting.asAt(report.AsOf)
		equityRows := append(report.Equity, AccountAmountView{Code: "", Name: textOrNil(exporting.label("profitToDate")), Amount: report.ProfitToDate})
		document.Tables = []documents.Table{
			exporting.amountsTable(exporting.label("assets"), report.Assets),
			exporting.amountsTable(exporting.label("liabilities"), report.Liabilities),
			exporting.amountsTable(exporting.label("equity"), equityRows),
		}
		document.Totals = []documents.Field{
			{Label: exporting.label("totalAssets"), Value: report.TotalAssets, Kind: documents.Money, Strong: true},
			{Label: exporting.label("totalLiabilities"), Value: report.TotalLiabilities, Kind: documents.Money},
			{Label: exporting.label("totalEquity"), Value: report.TotalEquity, Kind: documents.Money},
			{Label: exporting.label("liabilitiesAndEquity"), Value: report.TotalLiabilities + report.TotalEquity, Kind: documents.Money, Strong: true},
		}
		return "balance-sheet-" + report.AsOf, nil
	case ExportTrialBalance:
		report, reportError := service.TrialBalance(ctx, querier, principal, request.Range.ToDate)
		if reportError != nil {
			return "", reportError
		}
		document.Subtitle = exporting.asAt(report.AsOf)
		trialRows := [][]any{}
		for _, row := range report.Rows {
			trialRows = append(trialRows, []any{exporting.accountName(row.Code, row.SystemKey, row.Name), row.DebitBalance, row.CreditBalance})
		}
		document.Tables = []documents.Table{{
			Title: exporting.label("title.trial-balance"),
			Columns: []documents.Column{
				{Title: exporting.label("account"), Kind: documents.Text},
				{Title: exporting.moneyTitle("debit"), Kind: documents.Money, Sum: true},
				{Title: exporting.moneyTitle("credit"), Kind: documents.Money, Sum: true},
			},
			Rows: trialRows,
		}}
		return "trial-balance-" + report.AsOf, nil
	case ExportStatement:
		report, reportError := service.Statement(ctx, querier, principal, request.Range, request.Account)
		if reportError != nil {
			return "", reportError
		}
		accountTitle := exporting.accountName(report.Account.Code, report.Account.SystemKey, report.Account.Name)
		document.Subtitle = accountTitle + " · " + periodText(exporting.language, report.FromDate, report.ToDate)
		inTitle, outTitle := "debit", "credit"
		if report.Account.IsMoney {
			inTitle, outTitle = "moneyIn", "moneyOut"
		}
		statementRows := [][]any{}
		for _, line := range report.Lines {
			description := exporting.label("source." + line.SourceType)
			if line.Memo != nil {
				description += " · " + *line.Memo
			}
			statementRows = append(statementRows, []any{exporting.dateCell(line.EntryDate), line.Number, description, line.Debit, line.Credit, line.Balance})
		}
		document.Cards = []documents.Field{
			{Label: exporting.label("openingBalance"), Value: report.OpeningBalance, Kind: documents.Money},
			{Label: exporting.label("closingBalance"), Value: report.ClosingBalance, Kind: documents.Money},
		}
		document.Tables = []documents.Table{{
			Title: exporting.label("lines"),
			Columns: []documents.Column{
				{Title: exporting.label("date"), Kind: documents.Date},
				{Title: exporting.label("entry"), Kind: documents.Text},
				{Title: exporting.label("description"), Kind: documents.Text},
				{Title: exporting.moneyTitle(inTitle), Kind: documents.Money, Sum: true},
				{Title: exporting.moneyTitle(outTitle), Kind: documents.Money, Sum: true},
				{Title: exporting.moneyTitle("balance"), Kind: documents.Money},
			},
			Rows: statementRows,
		}}
		document.Landscape = true
		return "statement-" + report.Account.Code + "-" + report.FromDate + "-to-" + report.ToDate, nil
	case ExportVat:
		report, reportError := service.VatReport(ctx, querier, principal, request.Range)
		if reportError != nil {
			return "", reportError
		}
		document.Subtitle = periodText(exporting.language, report.FromDate, report.ToDate)
		vatRows := [][]any{}
		for _, month := range report.Months {
			vatRows = append(vatRows, []any{month.Month, month.Charged, month.Reclaimable, month.ToPay, exporting.dateCell(month.DueDate)})
		}
		document.Tables = []documents.Table{{
			Title: exporting.label("title.vat"),
			Columns: []documents.Column{
				{Title: exporting.label("month"), Kind: documents.Text},
				{Title: exporting.moneyTitle("vatCharged"), Kind: documents.Money, Sum: true},
				{Title: exporting.moneyTitle("vatReclaimable"), Kind: documents.Money, Sum: true},
				{Title: exporting.moneyTitle("vatToPay"), Kind: documents.Money, Sum: true},
				{Title: exporting.label("dueDate"), Kind: documents.Date},
			},
			Rows: vatRows,
		}}
		return "vat-" + report.FromDate + "-to-" + report.ToDate, nil
	}
	return "", ErrUnknownExport
}

func (exporting exporter) dateCell(date string) any {
	parsedDate, parseError := time.ParseInLocation(dateLayout, date, exporting.branding.Location())
	if parseError != nil {
		return date
	}
	return parsedDate
}

func (exporting exporter) asAt(date string) string {
	parsedDate, parseError := time.Parse(dateLayout, date)
	if parseError != nil {
		return date
	}
	return strings.ReplaceAll(exporting.label("asAt"), "{date}", documents.FormatDate(exporting.language, parsedDate))
}

func periodText(language string, fromDate string, toDate string) string {
	firstDay, firstError := time.Parse(dateLayout, fromDate)
	lastDay, lastError := time.Parse(dateLayout, toDate)
	if firstError != nil || lastError != nil {
		return fromDate + " – " + toDate
	}
	return documents.PeriodText(language, firstDay, lastDay)
}
