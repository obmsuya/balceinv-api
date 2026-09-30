package customers

import (
	"context"
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
