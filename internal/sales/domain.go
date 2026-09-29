package sales

import (
	"time"

	"github.com/google/uuid"
)

const (
	PaymentCash   = "cash"
	PaymentCard   = "card"
	PaymentMobile = "mobile"
)

type Sale struct {
	Id                 uuid.UUID
	CompanyId          uuid.UUID
	ShopId             uuid.UUID
	UserId             uuid.UUID
	ClientRef          string
	RequestHash        string
	ReceiptNumber      string
	Subtotal           int64
	DiscountTotal      int64
	Total              int64
	TaxTotal           int64
	TaxRateBasisPoints int
	AmountPaid         int64
	ChangeGiven        int64
	CurrencyCode       string
	CurrencyDecimals   int
	Note               *string
	CreatedAt          time.Time
}

type Payment struct {
	Method string
	Amount int64
}

type ShopCounter struct {
	ReceiptPrefix string
	Number        int64
}
