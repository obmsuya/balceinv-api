package suppliers

import (
	"time"

	"github.com/google/uuid"
)

type SupplierRequest struct {
	Name             string  `json:"name" validate:"required,max=120"`
	ContactPerson    *string `json:"contact_person" validate:"omitnil,max=120"`
	Phone            *string `json:"phone" validate:"omitnil,max=40"`
	Email            *string `json:"email" validate:"omitnil,max=160"`
	Tin              *string `json:"tin" validate:"omitnil,max=40"`
	Vrn              *string `json:"vrn" validate:"omitnil,max=40"`
	Address          *string `json:"address" validate:"omitnil,max=300"`
	PaymentTermsDays int     `json:"payment_terms_days" validate:"gte=0,lte=365"`
	OpeningBalance   int64   `json:"opening_balance" validate:"gte=0,lte=1000000000000000"`
	Notes            *string `json:"notes" validate:"omitnil,max=1000"`
	IsActive         *bool   `json:"is_active"`
}

type PurchaseLineRequest struct {
	ProductId string `json:"product_id" validate:"required,uuid"`
	Quantity  int    `json:"quantity" validate:"required,gte=1,lte=1000000"`
	UnitCost  int64  `json:"unit_cost" validate:"gte=0,lte=10000000000"`
}

type PurchaseRequest struct {
	ClientRef             string                `json:"client_ref" validate:"required,min=8,max=64"`
	SupplierId            *string               `json:"supplier_id" validate:"omitnil,uuid"`
	ShopId                *string               `json:"shop_id" validate:"omitnil,uuid"`
	PurchaseOrderId       *string               `json:"purchase_order_id" validate:"omitnil,uuid"`
	SupplierInvoiceNumber *string               `json:"supplier_invoice_number" validate:"omitnil,max=60"`
	InvoiceDate           *string               `json:"invoice_date" validate:"omitnil,max=10"`
	ReceivedAt            *time.Time            `json:"received_at"`
	PricesIncludeVat      bool                  `json:"prices_include_vat"`
	AmountPaid            int64                 `json:"amount_paid" validate:"gte=0"`
	PaymentMethod         string                `json:"payment_method" validate:"omitempty,oneof=cash bank mobile"`
	PaymentReference      *string               `json:"payment_reference" validate:"omitnil,max=100"`
	Note                  *string               `json:"note" validate:"omitnil,max=500"`
	Lines                 []PurchaseLineRequest `json:"lines" validate:"required,min=1,max=200,dive"`
}

type PaymentRequest struct {
	SupplierId string     `json:"supplier_id" validate:"required,uuid"`
	PurchaseId *string    `json:"purchase_id" validate:"omitnil,uuid"`
	ShopId     *string    `json:"shop_id" validate:"omitnil,uuid"`
	Amount     int64      `json:"amount" validate:"required,gte=1,lte=1000000000000000"`
	Method     string     `json:"method" validate:"required,oneof=cash bank mobile"`
	Reference  *string    `json:"reference" validate:"omitnil,max=100"`
	PaidAt     *time.Time `json:"paid_at"`
}

type ReasonRequest struct {
	Reason string `json:"reason" validate:"required,max=300"`
}

type ReturnLineRequest struct {
	ProductId string `json:"product_id" validate:"required,uuid"`
	Quantity  int    `json:"quantity" validate:"required,gte=1,lte=1000000"`
	UnitCost  int64  `json:"unit_cost" validate:"gte=0,lte=10000000000"`
}

type ReturnRequest struct {
	SupplierId string              `json:"supplier_id" validate:"required,uuid"`
	ShopId     *string             `json:"shop_id" validate:"omitnil,uuid"`
	Note       *string             `json:"note" validate:"omitnil,max=500"`
	Lines      []ReturnLineRequest `json:"lines" validate:"required,min=1,max=200,dive"`
}

type OrderLineRequest struct {
	ProductId        string `json:"product_id" validate:"required,uuid"`
	Quantity         int    `json:"quantity" validate:"required,gte=1,lte=1000000"`
	ExpectedUnitCost int64  `json:"expected_unit_cost" validate:"gte=0,lte=10000000000"`
}

type OrderRequest struct {
	SupplierId   string             `json:"supplier_id" validate:"required,uuid"`
	ShopId       *string            `json:"shop_id" validate:"omitnil,uuid"`
	ExpectedDate *string            `json:"expected_date" validate:"omitnil,max=10"`
	Note         *string            `json:"note" validate:"omitnil,max=500"`
	Send         bool               `json:"send"`
	Lines        []OrderLineRequest `json:"lines" validate:"required,min=1,max=200,dive"`
}

type SupplierFilter struct {
	SearchText      string
	IncludeInactive bool
}

type DocumentFilter struct {
	SupplierId      *uuid.UUID
	PurchaseOrderId *uuid.UUID
	Status          string
}

type AgingBuckets struct {
	Current    int64 `json:"current"`
	Days1To30  int64 `json:"days_1_30"`
	Days31To60 int64 `json:"days_31_60"`
	Days61To90 int64 `json:"days_61_90"`
	DaysOver90 int64 `json:"days_over_90"`
}

type SupplierView struct {
	Id               uuid.UUID     `json:"id"`
	Name             string        `json:"name"`
	ContactPerson    *string       `json:"contact_person"`
	Phone            *string       `json:"phone"`
	Email            *string       `json:"email"`
	Tin              *string       `json:"tin"`
	Vrn              *string       `json:"vrn"`
	Address          *string       `json:"address"`
	PaymentTermsDays int           `json:"payment_terms_days"`
	OpeningBalance   int64         `json:"opening_balance"`
	Notes            *string       `json:"notes"`
	IsActive         bool          `json:"is_active"`
	CreatedAt        time.Time     `json:"created_at"`
	UpdatedAt        time.Time     `json:"updated_at"`
	Balance          int64         `json:"balance"`
	OverdueAmount    int64         `json:"overdue_amount"`
	Aging            *AgingBuckets `json:"aging,omitempty"`
}

type SupplierAgingView struct {
	SupplierId uuid.UUID    `json:"supplier_id"`
	Name       string       `json:"name"`
	IsActive   bool         `json:"is_active"`
	Balance    int64        `json:"balance"`
	Aging      AgingBuckets `json:"aging"`
}

type AgingView struct {
	AsOf         string              `json:"as_of"`
	TotalBalance int64               `json:"total_balance"`
	Totals       AgingBuckets        `json:"totals"`
	Suppliers    []SupplierAgingView `json:"suppliers"`
}

type StatementLineView struct {
	Date       time.Time  `json:"date"`
	Kind       string     `json:"kind"`
	Reference  string     `json:"reference"`
	DocumentId *uuid.UUID `json:"document_id"`
	Debit      int64      `json:"debit"`
	Credit     int64      `json:"credit"`
	Balance    int64      `json:"balance"`
}

type StatementView struct {
	SupplierId     uuid.UUID           `json:"supplier_id"`
	SupplierName   string              `json:"supplier_name"`
	From           string              `json:"from"`
	To             string              `json:"to"`
	OpeningBalance int64               `json:"opening_balance"`
	ClosingBalance int64               `json:"closing_balance"`
	Lines          []StatementLineView `json:"lines"`
}

type PurchaseLineView struct {
	ProductId    uuid.UUID `json:"product_id"`
	ProductName  string    `json:"product_name"`
	VariantLabel string    `json:"variant_label"`
	Sku          string    `json:"sku"`
	Unit         string    `json:"unit"`
	Quantity     int       `json:"quantity"`
	UnitCost     int64     `json:"unit_cost"`
	VatAmount    int64     `json:"vat_amount"`
	LineTotal    int64     `json:"line_total"`
}

type PurchaseView struct {
	Id                    uuid.UUID          `json:"id"`
	PurchaseNumber        string             `json:"purchase_number"`
	ShopId                uuid.UUID          `json:"shop_id"`
	ShopName              string             `json:"shop_name"`
	SupplierId            *uuid.UUID         `json:"supplier_id"`
	SupplierName          *string            `json:"supplier_name"`
	SupplierInvoiceNumber *string            `json:"supplier_invoice_number"`
	InvoiceDate           *string            `json:"invoice_date"`
	ReceivedAt            time.Time          `json:"received_at"`
	Status                string             `json:"status"`
	PricesIncludeVat      bool               `json:"prices_include_vat"`
	Subtotal              int64              `json:"subtotal"`
	VatTotal              int64              `json:"vat_total"`
	Total                 int64              `json:"total"`
	AmountPaid            int64              `json:"amount_paid"`
	AmountDue             int64              `json:"amount_due"`
	PaymentStatus         string             `json:"payment_status"`
	Note                  *string            `json:"note"`
	AttachmentUrl         *string            `json:"attachment_url"`
	PurchaseOrderId       *uuid.UUID         `json:"purchase_order_id"`
	PurchaseOrderNumber   *string            `json:"purchase_order_number"`
	ClientRef             string             `json:"client_ref"`
	LineCount             int                `json:"line_count"`
	CreatedByName         *string            `json:"created_by_name"`
	CreatedAt             time.Time          `json:"created_at"`
	CancelledByName       *string            `json:"cancelled_by_name"`
	CancelledAt           *time.Time         `json:"cancelled_at"`
	CancelReason          *string            `json:"cancel_reason"`
	Lines                 []PurchaseLineView `json:"lines,omitempty"`
	Payments              []PaymentView      `json:"payments,omitempty"`
}

type PaymentView struct {
	Id             uuid.UUID  `json:"id"`
	PaymentNumber  string     `json:"payment_number"`
	SupplierId     *uuid.UUID `json:"supplier_id"`
	SupplierName   *string    `json:"supplier_name"`
	PurchaseId     *uuid.UUID `json:"purchase_id"`
	PurchaseNumber *string    `json:"purchase_number"`
	ShopId         uuid.UUID  `json:"shop_id"`
	ShopName       string     `json:"shop_name"`
	Amount         int64      `json:"amount"`
	Method         string     `json:"method"`
	Reference      *string    `json:"reference"`
	PaidAt         time.Time  `json:"paid_at"`
	CreatedByName  *string    `json:"created_by_name"`
	CreatedAt      time.Time  `json:"created_at"`
	IsVoided       bool       `json:"is_voided"`
	VoidedByName   *string    `json:"voided_by_name"`
	VoidedAt       *time.Time `json:"voided_at"`
	VoidReason     *string    `json:"void_reason"`
}

type ReturnLineView struct {
	ProductId    uuid.UUID `json:"product_id"`
	ProductName  string    `json:"product_name"`
	VariantLabel string    `json:"variant_label"`
	Sku          string    `json:"sku"`
	Unit         string    `json:"unit"`
	Quantity     int       `json:"quantity"`
	UnitCost     int64     `json:"unit_cost"`
	LineTotal    int64     `json:"line_total"`
}

type ReturnView struct {
	Id            uuid.UUID        `json:"id"`
	ReturnNumber  string           `json:"return_number"`
	SupplierId    uuid.UUID        `json:"supplier_id"`
	SupplierName  string           `json:"supplier_name"`
	ShopId        uuid.UUID        `json:"shop_id"`
	ShopName      string           `json:"shop_name"`
	Total         int64            `json:"total"`
	Note          *string          `json:"note"`
	ReturnedAt    time.Time        `json:"returned_at"`
	LineCount     int              `json:"line_count"`
	CreatedByName *string          `json:"created_by_name"`
	CreatedAt     time.Time        `json:"created_at"`
	Lines         []ReturnLineView `json:"lines,omitempty"`
}

type OrderLineView struct {
	ProductId         uuid.UUID `json:"product_id"`
	ProductName       string    `json:"product_name"`
	VariantLabel      string    `json:"variant_label"`
	Sku               string    `json:"sku"`
	Unit              string    `json:"unit"`
	CostPrice         int64     `json:"cost_price"`
	QuantityOrdered   int       `json:"quantity_ordered"`
	ExpectedUnitCost  int64     `json:"expected_unit_cost"`
	QuantityReceived  int       `json:"quantity_received"`
	QuantityRemaining int       `json:"quantity_remaining"`
}

type OrderView struct {
	Id              uuid.UUID       `json:"id"`
	OrderNumber     string          `json:"order_number"`
	SupplierId      uuid.UUID       `json:"supplier_id"`
	SupplierName    string          `json:"supplier_name"`
	ShopId          uuid.UUID       `json:"shop_id"`
	ShopName        string          `json:"shop_name"`
	Status          string          `json:"status"`
	ExpectedDate    *string         `json:"expected_date"`
	Note            *string         `json:"note"`
	ExpectedTotal   int64           `json:"expected_total"`
	LineCount       int             `json:"line_count"`
	CreatedByName   *string         `json:"created_by_name"`
	CreatedAt       time.Time       `json:"created_at"`
	SentAt          *time.Time      `json:"sent_at"`
	CancelledByName *string         `json:"cancelled_by_name"`
	CancelledAt     *time.Time      `json:"cancelled_at"`
	CancelReason    *string         `json:"cancel_reason"`
	Lines           []OrderLineView `json:"lines,omitempty"`
}

type LastCostView struct {
	ProductId  uuid.UUID  `json:"product_id"`
	UnitCost   *int64     `json:"unit_cost"`
	ReceivedAt *time.Time `json:"received_at"`
}
