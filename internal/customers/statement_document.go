package customers

import (
	"context"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/identity"
	"github.com/chrisostomemataba/balceinv-api/internal/documents"
	"github.com/google/uuid"
)

var statementDocumentLabels = map[string]map[string]string{
	documents.English: {
		"title":            "Customer statement",
		"sheet":            "Customer statement",
		"customer":         "Customer",
		"phone":            "Phone",
		"date":             "Date",
		"reference":        "Reference",
		"details":          "Details",
		"charged":          "Bought on credit",
		"paid":             "Paid",
		"balance":          "Balance",
		"opening":          "Owed at the start",
		"closing":          "Amount due",
		"total":            "Total for the period",
		"kind.credit_sale": "Sale on credit",
		"kind.payment":     "Payment",
		"method.cash":      "Cash",
		"method.mobile":    "Mobile money",
		"method.card":      "Card",
		"method.bank":      "Bank",
		"empty":            "No sales on credit or payments in this period",
		"notePay":          "Please pay the amount due at the shop or by mobile money, and give your phone number so it is recorded against your name.",
		"noteQuery":        "If anything here looks wrong, please tell us so we can check it together.",
	},
	documents.Swahili: {
		"title":            "Mwenendo wa deni",
		"sheet":            "Mwenendo wa deni",
		"customer":         "Mteja",
		"phone":            "Simu",
		"date":             "Tarehe",
		"reference":        "Kumbukumbu",
		"details":          "Maelezo",
		"charged":          "Mauzo ya mkopo",
		"paid":             "Malipo",
		"balance":          "Salio",
		"opening":          "Deni mwanzoni",
		"closing":          "Kiasi kinachodaiwa",
		"total":            "Jumla ya kipindi",
		"kind.credit_sale": "Mauzo ya mkopo",
		"kind.payment":     "Malipo",
		"method.cash":      "Taslimu",
		"method.mobile":    "Pesa ya simu",
		"method.card":      "Kadi",
		"method.bank":      "Benki",
		"empty":            "Hakuna mauzo ya mkopo wala malipo katika kipindi hiki",
		"notePay":          "Tafadhali lipa kiasi kinachodaiwa dukani au kwa pesa ya simu, na utaje namba yako ya simu ili malipo yarekodiwe kwa jina lako.",
		"noteQuery":        "Ukiona kosa lolote hapa, tafadhali tujulishe ili tukague pamoja.",
	},
}

type StatementDocumentRequest struct {
	Format   string
	Language string
	From     string
	To       string
}

func (service *Service) StatementDocument(ctx context.Context, querier database.Querier, principal *identity.Principal, customerId uuid.UUID, request StatementDocumentRequest) (documents.File, error) {
	exportFormat, formatError := documents.NormalizeFormat(request.Format)
	if formatError != nil {
		return documents.File{}, formatError
	}
	statementView, statementError := service.Statement(ctx, querier, principal, customerId, request.From, request.To)
	if statementError != nil {
		return documents.File{}, statementError
	}
	branding, brandingError := documents.LoadBranding(ctx, querier, service.objectStore, principal.CompanyId)
	if brandingError != nil {
		return documents.File{}, brandingError
	}
	viewer, viewerError := documents.LoadViewer(ctx, querier, principal.CompanyId, principal.UserId)
	if viewerError != nil {
		return documents.File{}, viewerError
	}
	language := documents.ResolveLanguage(request.Language, viewer.Language, branding.DefaultLanguage)
	label := func(key string) string {
		labelText, isKnown := statementDocumentLabels[language][key]
		if !isKnown {
			return statementDocumentLabels[documents.English][key]
		}
		return labelText
	}
	moneyTitle := func(key string) string {
		if branding.CurrencyCode == "" {
			return label(key)
		}
		return label(key) + " (" + branding.CurrencyCode + ")"
	}

	firstDay, firstError := time.Parse(dateLayout, statementView.From)
	lastDay, lastError := time.Parse(dateLayout, statementView.To)
	if firstError != nil || lastError != nil {
		return documents.File{}, ErrInvalidRange
	}

	registerRows := []documents.RegisterRow{}
	for _, entry := range statementView.Entries {
		reference := entry.Reference
		if methodName, isMethod := statementDocumentLabels[language]["method."+reference]; isMethod {
			reference = methodName
		}
		registerRows = append(registerRows, documents.RegisterRow{Cells: []any{entry.At, reference, label("kind." + entry.Kind), blankWhenZero(entry.Debit), blankWhenZero(entry.Credit), entry.Balance}})
	}
	filters := []documents.Field{{Label: label("customer"), Value: statementView.Customer.Name}}
	if statementView.Customer.Phone != nil && *statementView.Customer.Phone != "" {
		filters = append(filters, documents.Field{Label: label("phone"), Value: *statementView.Customer.Phone})
	}
	openingBalance := statementView.OpeningBalance
	register := documents.Register{
		Language:    language,
		Title:       label("title"),
		Subtitle:    documents.PeriodText(language, firstDay, lastDay),
		Filters:     filters,
		GeneratedBy: viewer.Name,
		GeneratedAt: time.Now(),
		Summary: []documents.Field{
			{Label: label("opening"), Value: statementView.OpeningBalance, Kind: documents.Money},
			{Label: label("charged"), Value: statementView.TotalDebits, Kind: documents.Money},
			{Label: label("paid"), Value: statementView.TotalCredits, Kind: documents.Money},
			{Label: label("closing"), Value: statementView.ClosingBalance, Kind: documents.Money, Strong: true},
		},
		Columns: []documents.RegisterColumn{
			{Title: label("date"), Kind: documents.Date},
			{Title: label("reference"), Kind: documents.Text, Weight: 2.2},
			{Title: label("details"), Kind: documents.Text, Weight: 2},
			{Title: moneyTitle("charged"), Kind: documents.Money, Sum: true, Effect: documents.AddsToBalance},
			{Title: moneyTitle("paid"), Kind: documents.Money, Sum: true, Effect: documents.TakesFromBalance},
			{Title: moneyTitle("balance"), Kind: documents.Money, Balance: true},
		},
		Rows:           registerRows,
		OpeningLabel:   label("opening"),
		OpeningBalance: &openingBalance,
		TotalLabel:     label("total"),
		ClosingLabel:   label("closing"),
		EmptyText:      label("empty"),
		Notes:          []string{label("notePay"), label("noteQuery")},
		SheetName:      label("sheet"),
	}
	fileName := documents.SafeFileName("customer-statement-" + statementView.Customer.Name + "-" + statementView.From + "-to-" + statementView.To)
	return documents.RenderRegister(branding, register, exportFormat, fileName)
}

func blankWhenZero(amount int64) any {
	if amount == 0 {
		return nil
	}
	return amount
}

var debtorsDocumentLabels = map[string]map[string]string{
	documents.English: {
		"title":     "Customers who owe",
		"asAt":      "As at {date}",
		"customer":  "Customer",
		"phone":     "Phone",
		"days0to30": "0–30 days",
		"days31":    "31–60 days",
		"days61":    "61–90 days",
		"days90":    "Over 90 days",
		"owes":      "Owes",
		"total":     "Total",
		"count":     "Customers who owe",
		"totalOwed": "Total owed",
		"overdue":   "Owed for over 30 days",
		"empty":     "No customer owes anything",
		"noteAge":   "Each debt is aged from the day of the sale; payments clear the oldest debt first.",
		"noteChase": "Send a reminder from each customer's page, or share their statement.",
	},
	documents.Swahili: {
		"title":     "Wateja wanaodaiwa",
		"asAt":      "Kufikia {date}",
		"customer":  "Mteja",
		"phone":     "Simu",
		"days0to30": "Siku 0–30",
		"days31":    "Siku 31–60",
		"days61":    "Siku 61–90",
		"days90":    "Zaidi ya siku 90",
		"owes":      "Deni",
		"total":     "Jumla",
		"count":     "Wateja wanaodaiwa",
		"totalOwed": "Jumla ya madeni",
		"overdue":   "Madeni ya zaidi ya siku 30",
		"empty":     "Hakuna mteja anayedaiwa",
		"noteAge":   "Umri wa kila deni unahesabiwa kuanzia siku ya mauzo; malipo yanafuta deni la zamani zaidi kwanza.",
		"noteChase": "Tuma ukumbusho kutoka ukurasa wa kila mteja, au shiriki mwenendo wa deni lake.",
	},
}

type DebtorsDocumentRequest struct {
	Format   string
	Language string
	AsOf     string
}

func (service *Service) DebtorsDocument(ctx context.Context, querier database.Querier, principal *identity.Principal, request DebtorsDocumentRequest) (documents.File, error) {
	exportFormat, formatError := documents.NormalizeFormat(request.Format)
	if formatError != nil {
		return documents.File{}, formatError
	}
	debtorsView, debtorsError := service.Debtors(ctx, querier, principal, request.AsOf)
	if debtorsError != nil {
		return documents.File{}, debtorsError
	}
	branding, brandingError := documents.LoadBranding(ctx, querier, service.objectStore, principal.CompanyId)
	if brandingError != nil {
		return documents.File{}, brandingError
	}
	viewer, viewerError := documents.LoadViewer(ctx, querier, principal.CompanyId, principal.UserId)
	if viewerError != nil {
		return documents.File{}, viewerError
	}
	language := documents.ResolveLanguage(request.Language, viewer.Language, branding.DefaultLanguage)
	label := func(key string) string {
		labelText, isKnown := debtorsDocumentLabels[language][key]
		if !isKnown {
			return debtorsDocumentLabels[documents.English][key]
		}
		return labelText
	}
	moneyTitle := func(key string) string {
		if branding.CurrencyCode == "" {
			return label(key)
		}
		return label(key) + " (" + branding.CurrencyCode + ")"
	}
	asOfDay, parseError := time.Parse(dateLayout, debtorsView.AsOf[:min(len(debtorsView.AsOf), 10)])
	if parseError != nil {
		return documents.File{}, ErrInvalidRange
	}

	registerRows := []documents.RegisterRow{}
	for _, debtor := range debtorsView.Customers {
		phone := ""
		if debtor.Phone != nil {
			phone = *debtor.Phone
		}
		registerRows = append(registerRows, documents.RegisterRow{Cells: []any{debtor.Name, phone, blankWhenZero(debtor.Aging.Days0To30), blankWhenZero(debtor.Aging.Days31To60), blankWhenZero(debtor.Aging.Days61To90), blankWhenZero(debtor.Aging.DaysOver90), debtor.Balance}})
	}
	overdue := debtorsView.Totals.Aging.Days31To60 + debtorsView.Totals.Aging.Days61To90 + debtorsView.Totals.Aging.DaysOver90
	register := documents.Register{
		Language:    language,
		Title:       label("title"),
		Subtitle:    strings.ReplaceAll(label("asAt"), "{date}", documents.FormatDate(language, asOfDay)),
		GeneratedBy: viewer.Name,
		GeneratedAt: time.Now(),
		Landscape:   true,
		Summary: []documents.Field{
			{Label: label("count"), Value: int64(len(debtorsView.Customers)), Kind: documents.Integer},
			{Label: label("totalOwed"), Value: debtorsView.Totals.Balance, Kind: documents.Money, Strong: true},
			{Label: label("overdue"), Value: overdue, Kind: documents.Money},
		},
		Columns: []documents.RegisterColumn{
			{Title: label("customer"), Kind: documents.Text, Weight: 3},
			{Title: label("phone"), Kind: documents.Text, Weight: 1.6},
			{Title: moneyTitle("days0to30"), Kind: documents.Money, Sum: true},
			{Title: moneyTitle("days31"), Kind: documents.Money, Sum: true},
			{Title: moneyTitle("days61"), Kind: documents.Money, Sum: true},
			{Title: moneyTitle("days90"), Kind: documents.Money, Sum: true},
			{Title: moneyTitle("owes"), Kind: documents.Money, Sum: true},
		},
		Rows:       registerRows,
		TotalLabel: label("total"),
		EmptyText:  label("empty"),
		Notes:      []string{label("noteAge"), label("noteChase")},
		SheetName:  label("title"),
	}
	return documents.RenderRegister(branding, register, exportFormat, "customers-who-owe-"+asOfDay.Format(dateLayout))
}
