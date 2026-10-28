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
		"invoice":         "Invoice",
		"receipt":         "Receipt",
		"date":            "Date",
		"servedBy":        "Served by",
		"shop":            "Shop",
		"note":            "Note",
		"items":           "Items",
		"item":            "Item",
		"quantity":        "Qty",
		"unitPrice":       "Unit price",
		"amount":          "Amount",
		"subtotal":        "Subtotal",
		"discounts":       "Discounts",
		"cashierDiscount": "Cashier discount",
		"total":           "Total",
		"includesVat":     "Includes VAT {rate}%",
		"paid":            "Paid ({method})",
		"change":          "Change",
		"cash":            "Cash",
		"card":            "Card",
		"mobile":          "Mobile money",
		"order":           "Order",
		"credit":          "Balance owed (pay later)",
		"wholesale":       "wholesale",
		"efd":             "EFD verification",
		"efdPending":      "EFD receipt to follow",
		"thanks":          "Thank you for shopping with us",
		"billTo":          "Bill to",
		"walkIn":          "Walk-in customer",
		"paidStatus":      "Paid",
		"owedStatus":      "Balance owed",
	},
	documents.Swahili: {
		"invoice":         "Ankara",
		"receipt":         "Risiti",
		"date":            "Tarehe",
		"servedBy":        "Umehudumiwa na",
		"shop":            "Duka",
		"note":            "Maelezo",
		"items":           "Bidhaa",
		"item":            "Bidhaa",
		"quantity":        "Idadi",
		"unitPrice":       "Bei ya kipande",
		"amount":          "Kiasi",
		"subtotal":        "Jumla ndogo",
		"discounts":       "Punguzo",
		"cashierDiscount": "Punguzo la keshia",
		"total":           "Jumla",
		"includesVat":     "Inajumuisha VAT {rate}%",
		"paid":            "Umelipa ({method})",
		"change":          "Chenji",
		"cash":            "Taslimu",
		"card":            "Kadi",
		"mobile":          "Pesa ya simu",
		"order":           "Oda",
		"credit":          "Deni (lipa baadaye)",
		"wholesale":       "jumla",
		"efd":             "Uthibitisho wa EFD",
		"efdPending":      "Risiti ya EFD itafuata",
		"thanks":          "Asante kwa kununua kwetu",
		"billTo":          "Kwa",
		"walkIn":          "Mteja wa papo hapo",
		"paidStatus":      "Imelipwa",
		"owedStatus":      "Ana deni",
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

	receiptView, receiptError := service.salesService.ViewReceipt(ctx, querier, principal, saleId)
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

	totals := totalFields(saleView, receiptView.ShowTax || branding.VatRegistered, label)
	isOnCredit := false
	for _, payment := range saleView.Payments {
		isOnCredit = isOnCredit || payment.Method == sales.PaymentCredit
	}
	statusText, statusTone := label("paidStatus"), documents.ToneGood
	if isOnCredit {
		statusText, statusTone = label("owedStatus"), documents.ToneWarning
	}

	saleDocument := documents.InvoiceDocument{
		Language:      language,
		Title:         label(documentKind),
		Number:        saleView.ReceiptNumber,
		Status:        statusText,
		StatusTone:    statusTone,
		Party:         saleParty(saleView, label),
		Facts:         saleDetails(saleView, receiptView.Shop.Name, label),
		Lines:         invoiceLines(saleView.Items, label),
		Totals:        totals,
		AmountInWords: documents.AmountInWords(language, saleView.Total, branding),
		GeneratedBy:   viewer.Name,
		GeneratedAt:   time.Now(),
	}
	if isOnCredit {
		saleDocument.PaymentDetails = branding.PaymentDetails
	}
	if headerText := joinLines(branding.ReceiptHeader); headerText != "" {
		saleDocument.Notes = append(saleDocument.Notes, headerText)
	}
	if saleView.Note != nil && strings.TrimSpace(*saleView.Note) != "" {
		saleDocument.Notes = append(saleDocument.Notes, strings.TrimSpace(*saleView.Note))
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

	return documents.RenderInvoice(branding, saleDocument, documentKind+"-"+saleView.ReceiptNumber)
}

func saleParty(saleView sales.SaleView, label func(string) string) documents.InvoiceParty {
	party := documents.InvoiceParty{Heading: label("billTo"), Name: label("walkIn")}
	if saleView.CustomerName != nil {
		party.Name = *saleView.CustomerName
	}
	if saleView.CustomerPhone != nil && *saleView.CustomerPhone != "" {
		party.Lines = append(party.Lines, *saleView.CustomerPhone)
	}
	return party
}

func invoiceLines(saleLines []sales.LineView, label func(string) string) []documents.InvoiceLine {
	lines := []documents.InvoiceLine{}
	for _, saleLine := range saleLines {
		itemName := saleLine.ProductName
		if saleLine.VariantLabel != "" {
			itemName += " · " + saleLine.VariantLabel
		}
		detailParts := []string{}
		if saleLine.IsWholesale {
			detailParts = append(detailParts, label("wholesale"))
		}
		if saleLine.DiscountName != nil && saleLine.DiscountAmount != saleLine.ManualDiscount {
			detailParts = append(detailParts, *saleLine.DiscountName)
		}
		if saleLine.ManualDiscount != 0 {
			detailParts = append(detailParts, label("cashierDiscount"))
		}
		quantity := int64(saleLine.Quantity)
		lines = append(lines, documents.InvoiceLine{
			Description: itemName,
			Detail:      strings.Join(detailParts, " · "),
			Quantity:    quantity,
			Unit:        saleLine.Unit,
			UnitPrice:   saleLine.UnitPrice,
			Discount:    saleLine.DiscountAmount,
			Amount:      quantity*saleLine.UnitPrice - saleLine.DiscountAmount,
		})
		for _, addon := range saleLine.Addons {
			lines = append(lines, documents.InvoiceLine{
				Description: "+ " + addon.Name,
				Quantity:    quantity,
				Unit:        saleLine.Unit,
				UnitPrice:   addon.UnitPrice,
				Amount:      quantity * addon.UnitPrice,
			})
		}
	}
	return lines
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
		isOnCredit := payment.Method == "credit"
		if isOnCredit {
			totals = append(totals, documents.Field{Label: label("credit"), Value: payment.Amount, Kind: documents.Money, Strong: true})
			continue
		}
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

func saleDetails(saleView sales.SaleView, shopName string, label func(string) string) []documents.Field {
	details := []documents.Field{
		{Label: label("date"), Value: saleView.CreatedAt, Kind: documents.DateTime},
		{Label: label("servedBy"), Value: saleView.CashierName},
		{Label: label("shop"), Value: shopName},
	}
	if saleView.OrderNumber != nil {
		details = append(details, documents.Field{Label: label("order"), Value: *saleView.OrderNumber})
	}
	return details
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
