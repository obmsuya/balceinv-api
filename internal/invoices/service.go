package invoices

import (
	"context"
	"errors"
	"strconv"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/identity"
	"github.com/chrisostomemataba/balceinv-api/internal/common/storage"
	"github.com/chrisostomemataba/balceinv-api/internal/documents"
	"github.com/chrisostomemataba/balceinv-api/internal/sales"
	"github.com/google/uuid"
)

const (
	KindInvoice = "invoice"
	KindReceipt = "receipt"
)

var ErrUnknownKind = errors.New("choose invoice or receipt")

type DocumentRequest struct {
	Format   string
	Language string
	Kind     string
}

var invoiceLabels = map[string]map[string]string{
	documents.English: {
		"invoice":     "Invoice",
		"receipt":     "Receipt",
		"date":        "Date",
		"servedBy":    "Served by",
		"shop":        "Shop",
		"note":        "Note",
		"items":       "Items",
		"item":        "Item",
		"quantity":    "Qty",
		"unitPrice":   "Unit price",
		"amount":      "Amount",
		"subtotal":    "Subtotal",
		"discounts":   "Discounts",
		"total":       "Total",
		"includesVat": "Includes VAT {rate}%",
		"paid":        "Paid ({method})",
		"change":      "Change",
		"cash":        "Cash",
		"card":        "Card",
		"mobile":      "Mobile money",
		"wholesale":   "wholesale",
		"efd":         "EFD verification",
		"efdPending":  "EFD receipt to follow",
		"thanks":      "Thank you for shopping with us",
	},
	documents.Swahili: {
		"invoice":     "Ankara",
		"receipt":     "Risiti",
		"date":        "Tarehe",
		"servedBy":    "Umehudumiwa na",
		"shop":        "Duka",
		"note":        "Maelezo",
		"items":       "Bidhaa",
		"item":        "Bidhaa",
		"quantity":    "Idadi",
		"unitPrice":   "Bei ya kipande",
		"amount":      "Kiasi",
		"subtotal":    "Jumla ndogo",
		"discounts":   "Punguzo",
		"total":       "Jumla",
		"includesVat": "Inajumuisha VAT {rate}%",
		"paid":        "Umelipa ({method})",
		"change":      "Chenji",
		"cash":        "Taslimu",
		"card":        "Kadi",
		"mobile":      "Pesa ya simu",
		"wholesale":   "jumla",
		"efd":         "Uthibitisho wa EFD",
		"efdPending":  "Risiti ya EFD itafuata",
		"thanks":      "Asante kwa kununua kwetu",
	},
}

type Service struct {
	salesService *sales.Service
	objectStore  storage.Store
}

func NewService(salesService *sales.Service, objectStore storage.Store) *Service {
	return &Service{
		salesService: salesService,
		objectStore:  objectStore,
	}
}

func (service *Service) SaleDocument(ctx context.Context, querier database.Querier, principal *identity.Principal, saleId uuid.UUID, request DocumentRequest) (documents.File, error) {
	requestedFormat := strings.ToLower(strings.TrimSpace(request.Format))
	if requestedFormat != "" && requestedFormat != documents.FormatPdf {
		return documents.File{}, documents.ErrUnknownFormat
	}
	documentKind := strings.ToLower(strings.TrimSpace(request.Kind))
	if documentKind == "" {
		documentKind = KindInvoice
	}
	if documentKind != KindInvoice && documentKind != KindReceipt {
		return documents.File{}, ErrUnknownKind
	}

	receiptView, receiptError := service.salesService.Receipt(ctx, querier, principal.CompanyId, saleId)
	if receiptError != nil {
		return documents.File{}, receiptError
	}
	branding, brandingError := documents.LoadBranding(ctx, querier, service.objectStore, principal.CompanyId)
	if brandingError != nil {
		return documents.File{}, brandingError
	}
	viewer, viewerError := documents.LoadViewer(ctx, querier, principal.CompanyId, principal.UserId)
	if viewerError != nil {
		return documents.File{}, viewerError
	}

	saleView := receiptView.Sale
	branding.CurrencyCode = saleView.CurrencyCode
	branding.CurrencyDecimals = saleView.CurrencyDecimals
	if receiptView.Shop.Address != nil && strings.TrimSpace(*receiptView.Shop.Address) != "" {
		branding.Address = strings.TrimSpace(*receiptView.Shop.Address)
	}
	if receiptView.Shop.Phone != nil && strings.TrimSpace(*receiptView.Shop.Phone) != "" {
		branding.Phone = strings.TrimSpace(*receiptView.Shop.Phone)
	}

	language := documents.ResolveLanguage(request.Language, receiptView.ReceiptLanguage)
	label := func(key string) string {
		return invoiceLabels[language][key]
	}

	saleDocument := documents.Document{
		Language:    language,
		Title:       label(documentKind),
		Number:      saleView.ReceiptNumber,
		Subtitle:    joinLines(branding.ReceiptHeader),
		GeneratedBy: viewer.Name,
		GeneratedAt: time.Now(),
		Details: []documents.Field{
			{Label: label("date"), Value: saleView.CreatedAt, Kind: documents.DateTime},
			{Label: label("servedBy"), Value: saleView.CashierName},
			{Label: label("shop"), Value: receiptView.Shop.Name},
		},
		Tables: []documents.Table{{
			Title: label("items"),
			Columns: []documents.Column{
				{Title: label("item"), Kind: documents.Text},
				{Title: label("quantity"), Kind: documents.Integer},
				{Title: label("unitPrice"), Kind: documents.Money},
				{Title: label("amount"), Kind: documents.Money},
			},
			Rows: lineRows(saleView.Items, label),
		}},
		Totals: totalFields(saleView, receiptView.ShowTax || branding.VatRegistered, label),
	}
	if saleView.Note != nil && strings.TrimSpace(*saleView.Note) != "" {
		saleDocument.Details = append(saleDocument.Details, documents.Field{Label: label("note"), Value: strings.TrimSpace(*saleView.Note)})
	}

	if saleView.Fiscal != nil {
		if saleView.Fiscal.Status == "sent" {
			captionLines := []string{label("efd")}
			if saleView.Fiscal.VerificationCode != nil {
				captionLines = append(captionLines, *saleView.Fiscal.VerificationCode)
			}
			saleDocument.QrCaption = strings.Join(captionLines, "\n")
			if saleView.Fiscal.VerificationUrl != nil {
				saleDocument.QrCode = *saleView.Fiscal.VerificationUrl
			}
		} else {
			saleDocument.Notes = append(saleDocument.Notes, label("efdPending"))
		}
	}
	footerText := branding.ReceiptFooter
	if footerText == "" {
		footerText = label("thanks")
	}
	saleDocument.Notes = append(saleDocument.Notes, footerText)

	return documents.Render(branding, saleDocument, documents.FormatPdf, documents.SafeFileName(documentKind+"-"+saleView.ReceiptNumber))
}

func lineRows(saleLines []sales.LineView, label func(string) string) [][]any {
	rows := [][]any{}
	for _, saleLine := range saleLines {
		itemName := saleLine.ProductName
		if saleLine.VariantLabel != "" {
			itemName += " · " + saleLine.VariantLabel
		}
		if saleLine.IsWholesale {
			itemName += " (" + label("wholesale") + ")"
		}
		quantity := int64(saleLine.Quantity)
		rows = append(rows, []any{itemName, quantity, saleLine.UnitPrice, quantity * saleLine.UnitPrice})
		for _, addon := range saleLine.Addons {
			rows = append(rows, []any{"  + " + addon.Name, quantity, addon.UnitPrice, quantity * addon.UnitPrice})
		}
		if saleLine.DiscountAmount != 0 && saleLine.DiscountName != nil {
			rows = append(rows, []any{"  − " + *saleLine.DiscountName, nil, nil, -saleLine.DiscountAmount})
		}
	}
	return rows
}

func totalFields(saleView sales.SaleView, showsTax bool, label func(string) string) []documents.Field {
	totals := []documents.Field{}
	if saleView.DiscountTotal != 0 {
		totals = append(totals,
			documents.Field{Label: label("subtotal"), Value: saleView.Subtotal, Kind: documents.Money},
			documents.Field{Label: label("discounts"), Value: -saleView.DiscountTotal, Kind: documents.Money},
		)
	}
	totals = append(totals, documents.Field{Label: label("total"), Value: saleView.Total, Kind: documents.Money, Strong: true})
	if showsTax && saleView.TaxTotal != 0 {
		rateText := strconv.FormatFloat(float64(saleView.TaxRateBasisPoints)/100, 'f', -1, 64)
		totals = append(totals, documents.Field{Label: strings.ReplaceAll(label("includesVat"), "{rate}", rateText), Value: saleView.TaxTotal, Kind: documents.Money})
	}
	for _, payment := range saleView.Payments {
		methodName := label(payment.Method)
		if methodName == "" {
			methodName = payment.Method
		}
		totals = append(totals, documents.Field{Label: strings.ReplaceAll(label("paid"), "{method}", methodName), Value: payment.Amount, Kind: documents.Money})
	}
	if saleView.ChangeGiven != 0 {
		totals = append(totals, documents.Field{Label: label("change"), Value: saleView.ChangeGiven, Kind: documents.Money})
	}
	return totals
}

func joinLines(multilineText string) string {
	lines := []string{}
	for _, line := range strings.Split(multilineText, "\n") {
		if strings.TrimSpace(line) != "" {
			lines = append(lines, strings.TrimSpace(line))
		}
	}
	return strings.Join(lines, "  ·  ")
}
