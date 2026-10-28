package invoices

import (
	"time"

	"github.com/google/uuid"
)

const (
	KindProforma        = "proforma"
	KindQuotation       = "quotation"
	KindDeliveryNote    = "delivery_note"
	KindSupplierInvoice = "supplier_invoice"

	StatusDraft     = "draft"
	StatusIssued    = "issued"
	StatusAccepted  = "accepted"
	StatusCancelled = "cancelled"

	PaymentUnpaid   = "unpaid"
	PaymentPartPaid = "part_paid"
	PaymentPaid     = "paid"

	MethodOffset = "offset"

	ReasonDelivered = "invoice_delivered"
	ReasonCancelled = "invoice_cancelled"
)

var kindPrefixes = map[string]string{
	KindInvoice:         "INV",
	KindProforma:        "PRO",
	KindQuotation:       "QUO",
	KindDeliveryNote:    "DN",
	KindSupplierInvoice: "SINV",
}

type Invoice struct {
	Id                 uuid.UUID
	CompanyId          uuid.UUID
	ShopId             uuid.UUID
	Kind               string
	Number             string
	Status             string
	CustomerId         *uuid.UUID
	SupplierId         *uuid.UUID
	IssueDate          string
	DueDate            *string
	Reference          *string
	Notes              *string
	Terms              *string
	Subtotal           int64
	DiscountTotal      int64
	Total              int64
	TaxTotal           int64
	TaxRateBasisPoints int
	SourceInvoiceId    *uuid.UUID
	CreatedBy          uuid.UUID
	CreatedAt          time.Time
	UpdatedAt          time.Time
}

type Line struct {
	Id             uuid.UUID
	Position       int
	ProductId      *uuid.UUID
	Description    string
	Unit           string
	Quantity       int
	UnitPrice      int64
	DiscountAmount int64
	LineTotal      int64
}

type Payment struct {
	Id                uuid.UUID
	CompanyId         uuid.UUID
	InvoiceId         uuid.UUID
	Method            string
	Amount            int64
	CustomerPaymentId *uuid.UUID
	SupplierReceiptId *uuid.UUID
	CreatedBy         uuid.UUID
	CreatedAt         time.Time
}

type Filter struct {
	Kinds         []string
	Status        string
	PaymentStatus string
	CustomerId    *uuid.UUID
	SupplierId    *uuid.UUID
	SearchText    string
}

func (invoice Invoice) isBill() bool {
	return invoice.Kind == KindInvoice || invoice.Kind == KindSupplierInvoice
}
