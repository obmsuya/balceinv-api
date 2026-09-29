package orders

import (
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/sales"
	"github.com/google/uuid"
)

type LineRequest struct {
	ProductId string `json:"product_id" validate:"required,uuid"`
	Quantity  int    `json:"quantity" validate:"required,gte=1,lte=100000"`
}

type MoneyRequest struct {
	Method string `json:"method" validate:"required,oneof=cash card mobile"`
	Amount int64  `json:"amount" validate:"required,gt=0,lte=1000000000000000"`
}

type OrderRequest struct {
	CustomerId string        `json:"customer_id" validate:"required,uuid"`
	Items      []LineRequest `json:"items" validate:"required,min=1,max=200,dive"`
	DueDate    *string       `json:"due_date" validate:"omitnil,max=10"`
	Note       *string       `json:"note" validate:"omitnil,max=200"`
	Deposit    *MoneyRequest `json:"deposit" validate:"omitnil"`
}

type CollectRequest struct {
	Payments []sales.PaymentRequest `json:"payments" validate:"omitempty,max=4,dive"`
}

type CancelRequest struct {
	Reason       string  `json:"reason" validate:"required,max=200"`
	RefundMethod *string `json:"refund_method" validate:"omitnil,oneof=cash card mobile"`
}

type OrderLineView struct {
	ProductId      uuid.UUID `json:"product_id"`
	ProductName    string    `json:"product_name"`
	VariantLabel   string    `json:"variant_label"`
	Sku            string    `json:"sku"`
	Unit           string    `json:"unit"`
	Quantity       int       `json:"quantity"`
	UnitPrice      int64     `json:"unit_price"`
	IsWholesale    bool      `json:"is_wholesale"`
	DiscountName   *string   `json:"discount_name"`
	DiscountAmount int64     `json:"discount_amount"`
	LineTotal      int64     `json:"line_total"`
}

type OrderPaymentView struct {
	Id            uuid.UUID `json:"id"`
	Kind          string    `json:"kind"`
	Method        string    `json:"method"`
	Amount        int64     `json:"amount"`
	CreatedAt     time.Time `json:"created_at"`
	CreatedByName string    `json:"created_by_name"`
}

type OrderView struct {
	Id                 uuid.UUID          `json:"id"`
	Number             string             `json:"number"`
	Status             string             `json:"status"`
	CustomerId         uuid.UUID          `json:"customer_id"`
	CustomerName       string             `json:"customer_name"`
	CustomerPhone      *string            `json:"customer_phone"`
	ShopId             uuid.UUID          `json:"shop_id"`
	ShopName           string             `json:"shop_name"`
	DueDate            *string            `json:"due_date"`
	Note               *string            `json:"note"`
	Subtotal           int64              `json:"subtotal"`
	DiscountTotal      int64              `json:"discount_total"`
	Total              int64              `json:"total"`
	TaxTotal           int64              `json:"tax_total"`
	TaxRateBasisPoints int                `json:"tax_rate_basis_points"`
	DepositTotal       int64              `json:"deposit_total"`
	BalanceDue         int64              `json:"balance_due"`
	LineCount          int64              `json:"line_count"`
	Lines              []OrderLineView    `json:"lines"`
	Payments           []OrderPaymentView `json:"payments"`
	SaleId             *uuid.UUID         `json:"sale_id"`
	SaleReceiptNumber  *string            `json:"sale_receipt_number"`
	CreatedByName      string             `json:"created_by_name"`
	CreatedAt          time.Time          `json:"created_at"`
	ReadyAt            *time.Time         `json:"ready_at"`
	CollectedAt        *time.Time         `json:"collected_at"`
	CancelledAt        *time.Time         `json:"cancelled_at"`
	CancelledByName    *string            `json:"cancelled_by_name"`
	CancelReason       *string            `json:"cancel_reason"`
}
