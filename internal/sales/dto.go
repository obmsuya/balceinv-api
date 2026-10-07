package sales

import (
	"time"

	"github.com/google/uuid"
)

type LineRequest struct {
	ProductId      string                 `json:"product_id" validate:"required,uuid"`
	Quantity       int                    `json:"quantity" validate:"required,gte=1,lte=100000"`
	AddonIds       []string               `json:"addon_ids" validate:"omitempty,max=20,dive,uuid"`
	ManualDiscount *ManualDiscountRequest `json:"manual_discount" validate:"omitnil"`
}

type ManualDiscountRequest struct {
	Kind  string `json:"kind" validate:"required,oneof=percent amount"`
	Value int64  `json:"value" validate:"required,gt=0,lte=1000000000000000"`
}

type PaymentRequest struct {
	Method string `json:"method" validate:"required,oneof=cash card mobile credit"`
	Amount int64  `json:"amount" validate:"required,gt=0,lte=1000000000000000"`
}

type QuoteRequest struct {
	Items []LineRequest `json:"items" validate:"required,min=1,max=200,dive"`
}

type SaleRequest struct {
	ClientRef  string           `json:"client_ref" validate:"required,min=8,max=64"`
	CustomerId *string          `json:"customer_id" validate:"omitnil,uuid"`
	Items      []LineRequest    `json:"items" validate:"required,min=1,max=200,dive"`
	Payments   []PaymentRequest `json:"payments" validate:"omitempty,max=4,dive"`
	Note       *string          `json:"note" validate:"omitnil,max=200"`
}

type OrderSaleRequest struct {
	ClientRef          string
	ShopId             uuid.UUID
	CustomerId         uuid.UUID
	Lines              []PricedLine
	TaxRateBasisPoints int
	Payments           []PaymentRequest
	Note               *string
}

type AddonView struct {
	AddonId   uuid.UUID `json:"addon_id"`
	Name      string    `json:"name"`
	UnitPrice int64     `json:"unit_price"`
}

type LineView struct {
	ProductId       uuid.UUID   `json:"product_id"`
	ProductName     string      `json:"product_name"`
	VariantLabel    string      `json:"variant_label"`
	Sku             string      `json:"sku"`
	Unit            string      `json:"unit"`
	Quantity        int         `json:"quantity"`
	UnitPrice       int64       `json:"unit_price"`
	IsWholesale     bool        `json:"is_wholesale"`
	Addons          []AddonView `json:"addons"`
	AddonsUnitTotal int64       `json:"addons_unit_total"`
	DiscountName    *string     `json:"discount_name"`
	DiscountAmount  int64       `json:"discount_amount"`
	ManualDiscount  int64       `json:"manual_discount_amount"`
	LineTotal       int64       `json:"line_total"`
	InStock         *int        `json:"in_stock,omitempty"`
}

type TillOptionsView struct {
	NumpadEnabled             bool `json:"numpad_enabled"`
	CustomerDisplayEnabled    bool `json:"customer_display_enabled"`
	EfdEnabled                bool `json:"efd_enabled"`
	PrintReceiptAutomatically bool `json:"print_receipt_automatically"`
}

type QuoteView struct {
	Lines              []LineView `json:"lines"`
	Subtotal           int64      `json:"subtotal"`
	DiscountTotal      int64      `json:"discount_total"`
	Total              int64      `json:"total"`
	TaxTotal           int64      `json:"tax_total"`
	TaxRateBasisPoints int        `json:"tax_rate_basis_points"`
}

type PaymentView struct {
	Method string `json:"method"`
	Amount int64  `json:"amount"`
}

type SaleView struct {
	Id                 uuid.UUID     `json:"id"`
	ReceiptNumber      string        `json:"receipt_number"`
	ClientRef          string        `json:"client_ref"`
	ShopId             uuid.UUID     `json:"shop_id"`
	ShopName           string        `json:"shop_name"`
	UserId             uuid.UUID     `json:"user_id"`
	CashierName        string        `json:"cashier_name"`
	CustomerId         *uuid.UUID    `json:"customer_id"`
	CustomerName       *string       `json:"customer_name"`
	CustomerPhone      *string       `json:"customer_phone"`
	CreditAmount       int64         `json:"credit_amount"`
	OrderNumber        *string       `json:"order_number"`
	Subtotal           int64         `json:"subtotal"`
	DiscountTotal      int64         `json:"discount_total"`
	Total              int64         `json:"total"`
	TaxTotal           int64         `json:"tax_total"`
	TaxRateBasisPoints int           `json:"tax_rate_basis_points"`
	AmountPaid         int64         `json:"amount_paid"`
	ChangeGiven        int64         `json:"change_given"`
	CurrencyCode       string        `json:"currency_code"`
	CurrencyDecimals   int           `json:"currency_decimals"`
	Note               *string       `json:"note"`
	CreatedAt          time.Time     `json:"created_at"`
	Items              []LineView    `json:"items"`
	Payments           []PaymentView `json:"payments"`
	Fiscal             *FiscalView   `json:"fiscal"`
	VoidedAt           *time.Time    `json:"voided_at"`
	VoidReason         *string       `json:"void_reason"`
	VoidedByName       *string       `json:"voided_by_name"`
	CreditNote         *FiscalView   `json:"credit_note"`
}

type VoidRequest struct {
	Reason string `json:"reason" validate:"required,min=3,max=200"`
}

type FiscalView struct {
	Status           string     `json:"status"`
	Attempts         int        `json:"attempts"`
	VerificationCode *string    `json:"verification_code"`
	VerificationUrl  *string    `json:"verification_url"`
	LastError        *string    `json:"last_error"`
	SentAt           *time.Time `json:"sent_at"`
}

type FiscalResult struct {
	Status           string
	VerificationCode *string
	VerificationUrl  *string
	LastError        *string
	SentAt           *time.Time
}

type SendWaitingView struct {
	Sent         int   `json:"sent"`
	Failed       int   `json:"failed"`
	StillWaiting int64 `json:"still_waiting"`
}

type SaleSummaryView struct {
	Id             uuid.UUID  `json:"id"`
	ReceiptNumber  string     `json:"receipt_number"`
	Total          int64      `json:"total"`
	DiscountTotal  int64      `json:"discount_total"`
	UnitCount      int64      `json:"unit_count"`
	PaymentMethods []string   `json:"payment_methods"`
	CashierName    string     `json:"cashier_name"`
	FiscalStatus   *string    `json:"fiscal_status"`
	VoidedAt       *time.Time `json:"voided_at"`
	CreatedAt      time.Time  `json:"created_at"`
}

type TotalsView struct {
	SaleCount     int64 `json:"sale_count"`
	Total         int64 `json:"total"`
	TaxTotal      int64 `json:"tax_total"`
	DiscountTotal int64 `json:"discount_total"`
}

type SaleFilter struct {
	SearchText    string
	From          *time.Time
	To            *time.Time
	FiscalWaiting bool
}

type ReceiptCompanyView struct {
	Name          string  `json:"name"`
	Address       *string `json:"address"`
	Phone         *string `json:"phone"`
	Tin           *string `json:"tin"`
	LogoUrl       *string `json:"logo_url"`
	ReceiptHeader *string `json:"receipt_header"`
	ReceiptFooter *string `json:"receipt_footer"`
}

type ReceiptShopView struct {
	Name    string  `json:"name"`
	Address *string `json:"address"`
	Phone   *string `json:"phone"`
}

type ReceiptView struct {
	Sale             SaleView           `json:"sale"`
	Company          ReceiptCompanyView `json:"company"`
	Shop             ReceiptShopView    `json:"shop"`
	ShowTax          bool               `json:"show_tax"`
	ShowBarcodes     bool               `json:"show_barcodes"`
	ReceiptLanguage  string             `json:"receipt_language"`
	PaperWidthMillis int                `json:"paper_width_millimeters"`
}
