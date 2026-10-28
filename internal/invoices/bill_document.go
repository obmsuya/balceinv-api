package invoices

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/identity"
	"github.com/chrisostomemataba/balceinv-api/internal/documents"
	"github.com/google/uuid"
)

var billLabels = map[string]map[string]string{
	documents.English: {
		"title.invoice":          "Invoice",
		"title.proforma":         "Proforma invoice",
		"title.quotation":        "Quotation",
		"title.delivery_note":    "Delivery note",
		"title.supplier_invoice": "Invoice",
		"party.billTo":           "Bill to",
		"party.preparedFor":      "Prepared for",
		"party.deliverTo":        "Deliver to",
		"issueDate":              "Date",
		"dueDate":                "Due date",
		"validUntil":             "Valid until",
		"reference":              "Your reference",
		"shop":                   "Shop",
		"forInvoice":             "For invoice",
		"subtotal":               "Subtotal",
		"discount":               "Discount",
		"vat":                    "VAT {rate}% (included)",
		"total":                  "Total",
		"paid":                   "Paid",
		"balanceDue":             "Balance due",
		"status.draft":           "Draft",
		"status.cancelled":       "Cancelled",
		"status.accepted":        "Accepted",
		"status.paid":            "Paid",
		"status.part_paid":       "Part paid",
		"status.unpaid":          "Unpaid",
		"status.overdue":         "Overdue",
		"sign.preparedBy":        "Prepared by",
		"sign.receivedBy":        "Received by (name & signature)",
		"sign.acceptedBy":        "Accepted by (name & signature)",
		"sign.dispatchedBy":      "Dispatched by",
		"sign.driver":            "Driver",
		"sign.stamp":             "Stamp",
	},
	documents.Swahili: {
		"title.invoice":          "Ankara",
		"title.proforma":         "Ankara ya awali",
		"title.quotation":        "Nukuu ya bei",
		"title.delivery_note":    "Hati ya kupokelea",
		"title.supplier_invoice": "Ankara",
		"party.billTo":           "Kwa",
		"party.preparedFor":      "Imeandaliwa kwa",
		"party.deliverTo":        "Peleka kwa",
		"issueDate":              "Tarehe",
		"dueDate":                "Tarehe ya kulipa",
		"validUntil":             "Inatumika hadi",
		"reference":              "Kumbukumbu yako",
		"shop":                   "Duka",
		"forInvoice":             "Ya ankara",
		"subtotal":               "Jumla ndogo",
		"discount":               "Punguzo",
		"vat":                    "VAT {rate}% (imejumuishwa)",
		"total":                  "Jumla",
		"paid":                   "Imelipwa",
		"balanceDue":             "Kiasi kinachodaiwa",
		"status.draft":           "Rasimu",
		"status.cancelled":       "Imeghairiwa",
		"status.accepted":        "Imekubaliwa",
		"status.paid":            "Imelipwa",
		"status.part_paid":       "Imelipwa kiasi",
		"status.unpaid":          "Haijalipwa",
		"status.overdue":         "Imechelewa",
		"sign.preparedBy":        "Imeandaliwa na",
		"sign.receivedBy":        "Imepokelewa na (jina na sahihi)",
		"sign.acceptedBy":        "Imekubaliwa na (jina na sahihi)",
		"sign.dispatchedBy":      "Imetolewa na",
		"sign.driver":            "Dereva",
		"sign.stamp":             "Muhuri",
	},
}

func (service *BillService) Document(ctx context.Context, querier database.Querier, principal *identity.Principal, invoiceId uuid.UUID, request DocumentRequest) (documents.File, error) {
	requestedFormat := strings.ToLower(strings.TrimSpace(request.Format))
	if requestedFormat != "" && requestedFormat != documents.FormatPdf {
		return documents.File{}, documents.ErrUnknownFormat
	}
	invoiceView, getError := service.Get(ctx, querier, principal, invoiceId)
	if getError != nil {
		return documents.File{}, getError
	}
	branding, brandingError := documents.LoadBranding(ctx, querier, service.documentService.objectStore, principal.CompanyId)
	if brandingError != nil {
		return documents.File{}, brandingError
	}
	viewer, viewerError := documents.LoadViewer(ctx, querier, principal.CompanyId, principal.UserId)
	if viewerError != nil {
		return documents.File{}, viewerError
	}

	language := documents.ResolveLanguage(request.Language, viewer.Language, branding.DefaultLanguage)
	invoiceDocument := BuildDocument(invoiceView, branding, language, viewer.Name)
	return documents.RenderInvoice(branding, invoiceDocument, invoiceView.Number)
}

func BuildDocument(invoiceView InvoiceView, branding documents.Branding, language string, generatedBy string) documents.InvoiceDocument {
	label := func(key string) string { return billLabels[language][key] }
	isDeliveryNote := invoiceView.Kind == KindDeliveryNote
	isOffer := invoiceView.Kind == KindQuotation || invoiceView.Kind == KindProforma
	isBill := invoiceView.Kind == KindInvoice || invoiceView.Kind == KindSupplierInvoice

	invoiceDocument := documents.InvoiceDocument{
		Language:    language,
		Title:       label("title." + invoiceView.Kind),
		Number:      invoiceView.Number,
		Party:       documentParty(invoiceView, label),
		Facts:       documentFacts(invoiceView, label),
		HidePrices:  isDeliveryNote,
		Terms:       textOf(invoiceView.Terms),
		GeneratedBy: generatedBy,
		GeneratedAt: time.Now(),
	}
	invoiceDocument.Status, invoiceDocument.StatusTone = documentStatus(invoiceView, label)
	if notes := textOf(invoiceView.Notes); notes != "" {
		invoiceDocument.Notes = []string{notes}
	}

	for _, lineView := range invoiceView.Lines {
		detail := ""
		if lineView.Sku != nil {
			detail = *lineView.Sku
		}
		invoiceDocument.Lines = append(invoiceDocument.Lines, documents.InvoiceLine{
			Description: lineView.Description,
			Detail:      detail,
			Quantity:    int64(lineView.Quantity),
			Unit:        lineView.Unit,
			UnitPrice:   lineView.UnitPrice,
			Discount:    lineView.DiscountAmount,
			Amount:      lineView.LineTotal,
		})
	}

	switch {
	case isDeliveryNote:
		invoiceDocument.Signatures = []string{label("sign.dispatchedBy"), label("sign.driver"), label("sign.receivedBy")}
	case isOffer:
		invoiceDocument.Signatures = []string{label("sign.preparedBy"), label("sign.acceptedBy"), label("sign.stamp")}
	default:
		invoiceDocument.Signatures = []string{label("sign.preparedBy"), label("sign.receivedBy"), label("sign.stamp")}
	}
	if isDeliveryNote {
		return invoiceDocument
	}

	invoiceDocument.Totals = documentTotals(invoiceView, isBill, label)
	invoiceDocument.AmountInWords = documents.AmountInWords(language, invoiceView.Total, branding)
	if invoiceView.Kind != KindQuotation {
		invoiceDocument.PaymentDetails = branding.PaymentDetails
	}
	return invoiceDocument
}

func documentParty(invoiceView InvoiceView, label func(string) string) documents.InvoiceParty {
	heading := label("party.billTo")
	switch invoiceView.Kind {
	case KindQuotation, KindProforma:
		heading = label("party.preparedFor")
	case KindDeliveryNote:
		heading = label("party.deliverTo")
	}
	party := documents.InvoiceParty{Heading: heading, Name: invoiceView.Party.Name}
	contactParts := []string{}
	for _, contactPart := range []*string{invoiceView.Party.Phone, invoiceView.Party.Email} {
		if textOf(contactPart) != "" {
			contactParts = append(contactParts, textOf(contactPart))
		}
	}
	if len(contactParts) > 0 {
		party.Lines = append(party.Lines, strings.Join(contactParts, " · "))
	}
	if address := textOf(invoiceView.Party.Address); address != "" {
		party.Lines = append(party.Lines, address)
	}
	if tin := textOf(invoiceView.Party.Tin); tin != "" {
		party.Lines = append(party.Lines, "TIN "+tin)
	}
	return party
}

func documentFacts(invoiceView InvoiceView, label func(string) string) []documents.Field {
	facts := []documents.Field{{Label: label("issueDate"), Value: civilDate(invoiceView.IssueDate), Kind: documents.Date}}
	if invoiceView.DueDate != nil {
		dueLabel := label("dueDate")
		if invoiceView.Kind == KindQuotation || invoiceView.Kind == KindProforma {
			dueLabel = label("validUntil")
		}
		facts = append(facts, documents.Field{Label: dueLabel, Value: civilDate(*invoiceView.DueDate), Kind: documents.Date, Strong: true})
	}
	if invoiceView.Kind == KindDeliveryNote && invoiceView.Source != nil {
		facts = append(facts, documents.Field{Label: label("forInvoice"), Value: invoiceView.Source.Number})
	}
	if reference := textOf(invoiceView.Reference); reference != "" {
		facts = append(facts, documents.Field{Label: label("reference"), Value: reference})
	}
	facts = append(facts, documents.Field{Label: label("shop"), Value: invoiceView.ShopName})
	return facts
}

func documentStatus(invoiceView InvoiceView, label func(string) string) (string, documents.Tone) {
	switch invoiceView.Status {
	case StatusDraft:
		return label("status.draft"), documents.ToneNeutral
	case StatusCancelled:
		return label("status.cancelled"), documents.ToneBad
	case StatusAccepted:
		return label("status.accepted"), documents.ToneGood
	}
	if invoiceView.PaymentStatus == nil {
		return "", documents.ToneNeutral
	}
	switch {
	case *invoiceView.PaymentStatus == PaymentPaid:
		return label("status.paid"), documents.ToneGood
	case invoiceView.IsOverdue:
		return label("status.overdue"), documents.ToneBad
	case *invoiceView.PaymentStatus == PaymentPartPaid:
		return label("status.part_paid"), documents.ToneWarning
	default:
		return label("status.unpaid"), documents.ToneWarning
	}
}

func documentTotals(invoiceView InvoiceView, isBill bool, label func(string) string) []documents.Field {
	totals := []documents.Field{}
	if invoiceView.DiscountTotal > 0 {
		totals = append(totals,
			documents.Field{Label: label("subtotal"), Value: invoiceView.Subtotal, Kind: documents.Money},
			documents.Field{Label: label("discount"), Value: -invoiceView.DiscountTotal, Kind: documents.Money},
		)
	}
	if invoiceView.TaxTotal > 0 {
		rateText := strconv.FormatFloat(float64(invoiceView.TaxRateBasisPoints)/100, 'f', -1, 64)
		totals = append(totals, documents.Field{Label: strings.ReplaceAll(label("vat"), "{rate}", rateText), Value: invoiceView.TaxTotal, Kind: documents.Money})
	}
	isOpenBill := isBill && invoiceView.Status != StatusCancelled
	if !isOpenBill {
		return append(totals, documents.Field{Label: label("total"), Value: invoiceView.Total, Kind: documents.Money, Strong: true})
	}
	return append(totals,
		documents.Field{Label: label("total"), Value: invoiceView.Total, Kind: documents.Money},
		documents.Field{Label: label("paid"), Value: -invoiceView.PaidTotal, Kind: documents.Money},
		documents.Field{Label: label("balanceDue"), Value: invoiceView.BalanceDue, Kind: documents.Money, Strong: true},
	)
}

func civilDate(dateText string) time.Time {
	parsedDate, _ := time.Parse(time.DateOnly, dateText)
	return time.Date(parsedDate.Year(), parsedDate.Month(), parsedDate.Day(), 12, 0, 0, 0, time.UTC)
}

func textOf(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
