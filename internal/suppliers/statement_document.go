package suppliers

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

var agingDocumentLabels = map[string]map[string]string{
	documents.English: {
		"title":     "What we owe suppliers",
		"asAt":      "As at {date}",
		"supplier":  "Supplier",
		"current":   "Not yet due",
		"days1":     "1–30 days late",
		"days31":    "31–60 days late",
		"days61":    "61–90 days late",
		"days90":    "Over 90 days late",
		"owed":      "We owe",
		"total":     "Total",
		"count":     "Suppliers we owe",
		"totalOwed": "Total we owe",
		"late":      "Late",
		"empty":     "We owe no supplier anything",
		"noteAge":   "Each purchase is due on its due date; payments settle the oldest purchase first.",
		"noteCheck": "Compare each line with the supplier's own statement before paying.",
	},
	documents.Swahili: {
		"title":     "Madeni kwa wasambazaji",
		"asAt":      "Kufikia {date}",
		"supplier":  "Msambazaji",
		"current":   "Bado hayajafika muda",
		"days1":     "Yamechelewa siku 1–30",
		"days31":    "Yamechelewa siku 31–60",
		"days61":    "Yamechelewa siku 61–90",
		"days90":    "Yamechelewa zaidi ya siku 90",
		"owed":      "Tunadaiwa",
		"total":     "Jumla",
		"count":     "Wasambazaji tunaowadai",
		"totalOwed": "Jumla tunayodaiwa",
		"late":      "Yaliyochelewa",
		"empty":     "Hatudaiwi na msambazaji yeyote",
		"noteAge":   "Kila ununuzi unadaiwa siku yake ya mwisho ya kulipa; malipo yanafuta ununuzi wa zamani zaidi kwanza.",
		"noteCheck": "Linganisha kila mstari na mwenendo wa msambazaji mwenyewe kabla ya kulipa.",
	},
}

type AgingDocumentRequest struct {
	Format   string
	Language string
	AsOf     string
}

func (service *Service) AgingDocument(ctx context.Context, querier database.Querier, principal *identity.Principal, request AgingDocumentRequest) (documents.File, error) {
	exportFormat, formatError := documents.NormalizeFormat(request.Format)
	if formatError != nil {
		return documents.File{}, formatError
	}
	agingView, agingError := service.Aging(ctx, querier, principal, request.AsOf)
	if agingError != nil {
		return documents.File{}, agingError
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
		labelText, isKnown := agingDocumentLabels[language][key]
		if !isKnown {
			return agingDocumentLabels[documents.English][key]
		}
		return labelText
	}
	moneyTitle := func(key string) string {
		if branding.CurrencyCode == "" {
			return label(key)
		}
		return label(key) + " (" + branding.CurrencyCode + ")"
	}
	asOfDay, parseError := time.Parse(dateLayout, agingView.AsOf[:min(len(agingView.AsOf), 10)])
	if parseError != nil {
		return documents.File{}, ErrInvalidDate
	}

	registerRows := []documents.RegisterRow{}
	for _, supplierAging := range agingView.Suppliers {
		if supplierAging.Balance == 0 {
			continue
		}
		registerRows = append(registerRows, documents.RegisterRow{Cells: []any{supplierAging.Name, blankWhenZero(supplierAging.Aging.Current), blankWhenZero(supplierAging.Aging.Days1To30), blankWhenZero(supplierAging.Aging.Days31To60), blankWhenZero(supplierAging.Aging.Days61To90), blankWhenZero(supplierAging.Aging.DaysOver90), supplierAging.Balance}})
	}
	lateTotal := agingView.Totals.Days1To30 + agingView.Totals.Days31To60 + agingView.Totals.Days61To90 + agingView.Totals.DaysOver90
	register := documents.Register{
		Language:    language,
		Title:       label("title"),
		Subtitle:    strings.ReplaceAll(label("asAt"), "{date}", documents.FormatDate(language, asOfDay)),
		GeneratedBy: viewer.Name,
		GeneratedAt: time.Now(),
		Landscape:   true,
		Summary: []documents.Field{
			{Label: label("count"), Value: int64(len(registerRows)), Kind: documents.Integer},
			{Label: label("totalOwed"), Value: agingView.TotalBalance, Kind: documents.Money, Strong: true},
			{Label: label("late"), Value: lateTotal, Kind: documents.Money},
		},
		Columns: []documents.RegisterColumn{
			{Title: label("supplier"), Kind: documents.Text, Weight: 3},
			{Title: moneyTitle("current"), Kind: documents.Money, Sum: true},
			{Title: moneyTitle("days1"), Kind: documents.Money, Sum: true},
			{Title: moneyTitle("days31"), Kind: documents.Money, Sum: true},
			{Title: moneyTitle("days61"), Kind: documents.Money, Sum: true},
			{Title: moneyTitle("days90"), Kind: documents.Money, Sum: true},
			{Title: moneyTitle("owed"), Kind: documents.Money, Sum: true},
		},
		Rows:       registerRows,
		TotalLabel: label("total"),
		EmptyText:  label("empty"),
		Notes:      []string{label("noteAge"), label("noteCheck")},
		SheetName:  label("title"),
	}
	return documents.RenderRegister(branding, register, exportFormat, "suppliers-we-owe-"+asOfDay.Format(dateLayout))
}
