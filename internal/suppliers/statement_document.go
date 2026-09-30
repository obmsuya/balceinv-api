package suppliers

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
		"title":                "Supplier statement",
		"sheet":                "Supplier statement",
		"supplier":             "Supplier",
		"date":                 "Date",
		"reference":            "Reference",
		"details":              "Details",
		"bought":               "Bought",
		"paid":                 "Paid or returned",
		"balance":              "Balance",
		"opening":              "Owed at the start",
		"closing":              "Amount we owe",
		"total":                "Total for the period",
		"kind.opening_balance": "Owed from before",
		"kind.purchase":        "Stock bought",
		"kind.return":          "Returned to the supplier",
		"kind.payment":         "Payment",
		"empty":                "No purchases, returns or payments in this period",
		"noteCheck":            "Compare this with the supplier's own statement; any difference is usually a delivery or payment not yet recorded on one side.",
		"noteSource":           "Purchases and returns are shown at the invoice amount, including VAT where the supplier charged it.",
	},
	documents.Swahili: {
		"title":                "Mwenendo wa akaunti ya msambazaji",
		"sheet":                "Mwenendo wa msambazaji",
		"supplier":             "Msambazaji",
		"date":                 "Tarehe",
		"reference":            "Kumbukumbu",
		"details":              "Maelezo",
		"bought":               "Kilichonunuliwa",
		"paid":                 "Kilicholipwa au kurudishwa",
		"balance":              "Salio",
		"opening":              "Deni mwanzoni",
		"closing":              "Kiasi tunachodaiwa",
		"total":                "Jumla ya kipindi",
		"kind.opening_balance": "Deni la zamani",
		"kind.purchase":        "Stoku iliyonunuliwa",
		"kind.return":          "Bidhaa zilizorudishwa kwa msambazaji",
		"kind.payment":         "Malipo",
		"empty":                "Hakuna manunuzi, marejesho wala malipo katika kipindi hiki",
		"noteCheck":            "Linganisha na mwenendo wa msambazaji mwenyewe; tofauti yoyote kwa kawaida ni bidhaa au malipo ambayo bado hayajarekodiwa upande mmoja.",
		"noteSource":           "Manunuzi na marejesho yameonyeshwa kwa kiasi cha ankara, pamoja na VAT pale msambazaji alipoitoza.",
	},
}

type StatementDocumentRequest struct {
	Format   string
	Language string
	From     string
	To       string
}

func (service *Service) StatementDocument(ctx context.Context, querier database.Querier, principal *identity.Principal, supplierId uuid.UUID, request StatementDocumentRequest) (documents.File, error) {
	exportFormat, formatError := documents.NormalizeFormat(request.Format)
	if formatError != nil {
		return documents.File{}, formatError
	}
	statementView, statementError := service.Statement(ctx, querier, principal, supplierId, request.From, request.To)
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
	totalBought, totalPaid := int64(0), int64(0)
	for _, line := range statementView.Lines {
		totalBought += line.Debit
		totalPaid += line.Credit
		registerRows = append(registerRows, documents.RegisterRow{Cells: []any{line.Date, line.Reference, label("kind." + line.Kind), blankWhenZero(line.Debit), blankWhenZero(line.Credit), line.Balance}})
	}
	openingBalance := statementView.OpeningBalance
	register := documents.Register{
		Language:    language,
		Title:       label("title"),
		Subtitle:    documents.PeriodText(language, firstDay, lastDay),
		Filters:     []documents.Field{{Label: label("supplier"), Value: statementView.SupplierName}},
		GeneratedBy: viewer.Name,
		GeneratedAt: time.Now(),
		Summary: []documents.Field{
			{Label: label("opening"), Value: statementView.OpeningBalance, Kind: documents.Money},
			{Label: label("bought"), Value: totalBought, Kind: documents.Money},
			{Label: label("paid"), Value: totalPaid, Kind: documents.Money},
			{Label: label("closing"), Value: statementView.ClosingBalance, Kind: documents.Money, Strong: true},
		},
		Columns: []documents.RegisterColumn{
			{Title: label("date"), Kind: documents.Date},
			{Title: label("reference"), Kind: documents.Text, Weight: 2.2},
			{Title: label("details"), Kind: documents.Text, Weight: 2.2},
			{Title: moneyTitle("bought"), Kind: documents.Money, Sum: true, Effect: documents.AddsToBalance},
			{Title: moneyTitle("paid"), Kind: documents.Money, Sum: true, Effect: documents.TakesFromBalance},
			{Title: moneyTitle("balance"), Kind: documents.Money, Balance: true},
		},
		Rows:           registerRows,
		OpeningLabel:   label("opening"),
		OpeningBalance: &openingBalance,
		TotalLabel:     label("total"),
		ClosingLabel:   label("closing"),
		EmptyText:      label("empty"),
		Notes:          []string{label("noteSource"), label("noteCheck")},
		SheetName:      label("sheet"),
	}
	fileName := documents.SafeFileName("supplier-statement-" + statementView.SupplierName + "-" + statementView.From + "-to-" + statementView.To)
	return documents.RenderRegister(branding, register, exportFormat, fileName)
}

func blankWhenZero(amount int64) any {
	if amount == 0 {
		return nil
	}
	return amount
}
