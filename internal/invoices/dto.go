package invoices

import (
	"time"

	"github.com/google/uuid"
)

type LineRequest struct {
	ProductId      *string `json:"product_id" validate:"omitnil,uuid"`
	Description    string  `json:"description" validate:"max=300"`
	Unit           string  `json:"unit" validate:"max=20"`
	Quantity       int     `json:"quantity" validate:"required,gte=1,lte=1000000"`
	UnitPrice      *int64  `json:"unit_price" validate:"omitnil,gte=0,lte=1000000000000000"`
	DiscountAmount int64   `json:"discount_amount" validate:"gte=0,lte=1000000000000000"`
}

type InvoiceRequest struct {
	Kind       string        `json:"kind" validate:"required,oneof=invoice proforma quotation delivery_note supplier_invoice"`
	CustomerId *string       `json:"customer_id" validate:"omitnil,uuid"`
	SupplierId *string       `json:"supplier_id" validate:"omitnil,uuid"`
	IssueDate  *string       `json:"issue_date" validate:"omitnil,max=10"`
	DueDate    *string       `json:"due_date" validate:"omitnil,max=10"`
	Reference  *string       `json:"reference" validate:"omitnil,max=60"`
	Notes      *string       `json:"notes" validate:"omitnil,max=1000"`
	Terms      *string       `json:"terms" validate:"omitnil,max=1000"`
	Lines      []LineRequest `json:"lines" validate:"required,min=1,max=200,dive"`
}

type PaymentRequest struct {
	Method    string  `json:"method" validate:"required,oneof=cash card mobile bank offset"`
	Amount    int64   `json:"amount" validate:"required,gt=0,lte=1000000000000000"`
	Reference *string `json:"reference" validate:"omitnil,max=80"`
}

type ReasonRequest struct {
	Reason string `json:"reason" validate:"required,max=200"`
}

type ConvertRequest struct {
	Kind string `json:"kind" validate:"required,oneof=invoice delivery_note"`
}

type LineView struct {
	Id             uuid.UUID  `json:"id"`
	ProductId      *uuid.UUID `json:"product_id"`
	Sku            *string    `json:"sku"`
	Description    string     `json:"description"`
	Unit           string     `json:"unit"`
	Quantity       int        `json:"quantity"`
	UnitPrice      int64      `json:"unit_price"`
	DiscountAmount int64      `json:"discount_amount"`
	LineTotal      int64      `json:"line_total"`
}

type PaymentView struct {
	Id            uuid.UUID  `json:"id"`
	Method        string     `json:"method"`
	Amount        int64      `json:"amount"`
	Reference     *string    `json:"reference"`
	IsVoided      bool       `json:"is_voided"`
	CreatedAt     time.Time  `json:"created_at"`
	CreatedByName string     `json:"created_by_name"`
	MoneyRecordId *uuid.UUID `json:"money_record_id"`
}

type PartyView struct {
	Id      uuid.UUID `json:"id"`
	Type    string    `json:"type"`
	Name    string    `json:"name"`
	Phone   *string   `json:"phone"`
	Email   *string   `json:"email"`
	Address *string   `json:"address"`
	Tin     *string   `json:"tin"`
}

type RelatedView struct {
	Id     uuid.UUID `json:"id"`
	Kind   string    `json:"kind"`
	Number string    `json:"number"`
	Status string    `json:"status"`
}

type InvoiceView struct {
	Id                 uuid.UUID     `json:"id"`
	Kind               string        `json:"kind"`
	Number             string        `json:"number"`
	Status             string        `json:"status"`
	PaymentStatus      *string       `json:"payment_status"`
	IsOverdue          bool          `json:"is_overdue"`
	Party              PartyView     `json:"party"`
	ShopId             uuid.UUID     `json:"shop_id"`
	ShopName           string        `json:"shop_name"`
	IssueDate          string        `json:"issue_date"`
	DueDate            *string       `json:"due_date"`
	Reference          *string       `json:"reference"`
	Notes              *string       `json:"notes"`
	Terms              *string       `json:"terms"`
	Subtotal           int64         `json:"subtotal"`
	DiscountTotal      int64         `json:"discount_total"`
	TaxTotal           int64         `json:"tax_total"`
	TaxRateBasisPoints int           `json:"tax_rate_basis_points"`
	Total              int64         `json:"total"`
	PaidTotal          int64         `json:"paid_total"`
	BalanceDue         int64         `json:"balance_due"`
	LineCount          int           `json:"line_count"`
	Lines              []LineView    `json:"lines,omitempty"`
	Payments           []PaymentView `json:"payments,omitempty"`
	Source             *RelatedView  `json:"source"`
	Derived            []RelatedView `json:"derived,omitempty"`
	DeliveredAt        *time.Time    `json:"delivered_at"`
	DeliveredByName    *string       `json:"delivered_by_name"`
	CancelledAt        *time.Time    `json:"cancelled_at"`
	CancelledByName    *string       `json:"cancelled_by_name"`
	CancelReason       *string       `json:"cancel_reason"`
	CreatedByName      string        `json:"created_by_name"`
	CreatedAt          time.Time     `json:"created_at"`
	UpdatedAt          time.Time     `json:"updated_at"`
}

type SummaryView struct {
	OwedToYou      int64 `json:"owed_to_you"`
	OwedCount      int   `json:"owed_count"`
	Overdue        int64 `json:"overdue"`
	OverdueCount   int   `json:"overdue_count"`
	PaidThisMonth  int64 `json:"paid_this_month"`
	AwaitingCount  int   `json:"awaiting_delivery_count"`
	OpenQuoteCount int   `json:"open_quote_count"`
}
