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

var orderDocumentLabels = map[string]map[string]string{
	documents.English: {
		"title":          "Purchase order",
		"supplier":       "Supplier",
		"orderDate":      "Order date",
		"deliverBy":      "Deliver by",
		"deliverTo":      "Deliver to",
		"contact":        "Contact",
		"total":          "Expected total",
		"draft":          "Draft",
		"sent":           "Sent",
		"partlyReceived": "Partly received",
		"received":       "Received",
		"cancelled":      "Cancelled",
		"preparedBy":     "Prepared by",
		"authorisedBy":   "Authorised by",
		"acceptedBy":     "Accepted by supplier",
		"pleaseQuote":    "Please supply the items above and quote this order number on your invoice and delivery note.",
	},
	documents.Swahili: {
		"title":          "Oda kwa msambazaji",
		"supplier":       "Msambazaji",
		"orderDate":      "Tarehe ya oda",
		"deliverBy":      "Leta kabla ya",
		"deliverTo":      "Peleka",
		"contact":        "Mhusika",
		"total":          "Jumla inayotarajiwa",
		"draft":          "Rasimu",
		"sent":           "Imetumwa",
		"partlyReceived": "Imepokelewa kiasi",
		"received":       "Imepokelewa",
		"cancelled":      "Imeghairiwa",
		"preparedBy":     "Imeandaliwa na",
		"authorisedBy":   "Imeidhinishwa na",
		"acceptedBy":     "Imekubaliwa na msambazaji",
		"pleaseQuote":    "Tafadhali leta bidhaa zilizo hapo juu na uandike namba ya oda hii kwenye ankara na hati ya kupokelea.",
	},
}

var orderStatusTones = map[string]documents.Tone{
	OrderDraft:          documents.ToneNeutral,
	OrderSent:           documents.ToneWarning,
	OrderPartlyReceived: documents.ToneWarning,
	OrderReceived:       documents.ToneGood,
	OrderCancelled:      documents.ToneBad,
}

var orderStatusLabelKeys = map[string]string{
	OrderDraft:          "draft",
	OrderSent:           "sent",
	OrderPartlyReceived: "partlyReceived",
	OrderReceived:       "received",
	OrderCancelled:      "cancelled",
}

type OrderDocumentRequest struct {
	Format   string
	Language string
}

func (service *Service) OrderDocument(ctx context.Context, querier database.Querier, principal *identity.Principal, orderId uuid.UUID, request OrderDocumentRequest) (documents.File, error) {
	requestedFormat := strings.ToLower(strings.TrimSpace(request.Format))
	if requestedFormat != "" && requestedFormat != documents.FormatPdf {
		return documents.File{}, documents.ErrUnknownFormat
	}
	orderView, orderError := service.GetOrder(ctx, querier, principal, orderId)
	if orderError != nil {
		return documents.File{}, orderError
	}
	supplierView, supplierError := service.GetSupplier(ctx, querier, principal, orderView.SupplierId)
	if supplierError != nil {
		return documents.File{}, supplierError
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
	orderDocument := buildOrderDocument(orderView, supplierView, branding, language, viewer.Name)
	return documents.RenderInvoice(branding, orderDocument, orderView.OrderNumber)
}

func buildOrderDocument(orderView OrderView, supplierView SupplierView, branding documents.Branding, language string, generatedBy string) documents.InvoiceDocument {
	label := func(key string) string { return orderDocumentLabels[language][key] }

	party := documents.InvoiceParty{Heading: label("supplier"), Name: supplierView.Name}
	for _, partyLine := range []string{
		joinPresent(" · ", supplierView.Phone, supplierView.Email),
		textOf(supplierView.Address),
		joinPresent(" · ", prefixed("TIN ", supplierView.Tin), prefixed("VRN ", supplierView.Vrn)),
		prefixedText(label("contact")+": ", textOf(supplierView.ContactPerson)),
	} {
		if partyLine != "" {
			party.Lines = append(party.Lines, partyLine)
		}
	}

	facts := []documents.Field{{Label: label("orderDate"), Value: orderView.CreatedAt, Kind: documents.Date}}
	if orderView.ExpectedDate != nil {
		expectedDate, parseError := time.Parse(dateLayout, *orderView.ExpectedDate)
		if parseError == nil {
			facts = append(facts, documents.Field{Label: label("deliverBy"), Value: time.Date(expectedDate.Year(), expectedDate.Month(), expectedDate.Day(), 12, 0, 0, 0, time.UTC), Kind: documents.Date, Strong: true})
		}
	}
	facts = append(facts, documents.Field{Label: label("deliverTo"), Value: orderView.ShopName})

	lines := []documents.InvoiceLine{}
	for _, orderLine := range orderView.Lines {
		description := orderLine.ProductName
		if orderLine.VariantLabel != "" {
			description += " · " + orderLine.VariantLabel
		}
		quantity := int64(orderLine.QuantityOrdered)
		lines = append(lines, documents.InvoiceLine{
			Description: description,
			Detail:      orderLine.Sku,
			Quantity:    quantity,
			Unit:        orderLine.Unit,
			UnitPrice:   orderLine.ExpectedUnitCost,
			Amount:      quantity * orderLine.ExpectedUnitCost,
		})
	}

	orderDocument := documents.InvoiceDocument{
		Language:      language,
		Title:         label("title"),
		Number:        orderView.OrderNumber,
		Status:        label(orderStatusLabelKeys[orderView.Status]),
		StatusTone:    orderStatusTones[orderView.Status],
		Party:         party,
		Facts:         facts,
		Lines:         lines,
		Totals:        []documents.Field{{Label: label("total"), Value: orderView.ExpectedTotal, Kind: documents.Money, Strong: true}},
		AmountInWords: documents.AmountInWords(language, orderView.ExpectedTotal, branding),
		Terms:         label("pleaseQuote"),
		Signatures:    []string{label("preparedBy"), label("authorisedBy"), label("acceptedBy")},
		GeneratedBy:   generatedBy,
		GeneratedAt:   time.Now(),
	}
	if note := textOf(orderView.Note); note != "" {
		orderDocument.Notes = []string{note}
	}
	return orderDocument
}

func joinPresent(separator string, values ...*string) string {
	presentValues := []string{}
	for _, value := range values {
		if textOf(value) != "" {
			presentValues = append(presentValues, textOf(value))
		}
	}
	return strings.Join(presentValues, separator)
}

func prefixed(prefix string, value *string) *string {
	if textOf(value) == "" {
		return nil
	}
	prefixedValue := prefix + textOf(value)
	return &prefixedValue
}

func prefixedText(prefix string, value string) string {
	if value == "" {
		return ""
	}
	return prefix + value
}

func textOf(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
