package orders

import (
	"time"

	"github.com/google/uuid"
)

const (
	StatusOpen      = "open"
	StatusReady     = "ready"
	StatusCollected = "collected"
	StatusCancelled = "cancelled"

	KindDeposit = "deposit"
	KindRefund  = "refund"

	ReasonReserved  = "order_reserved"
	ReasonCancelled = "order_cancelled"
)

type Order struct {
	Id                 uuid.UUID
	CompanyId          uuid.UUID
	ShopId             uuid.UUID
	CustomerId         uuid.UUID
	Number             int64
	Status             string
	DueDate            *string
	Note               *string
	Subtotal           int64
	DiscountTotal      int64
	Total              int64
	TaxTotal           int64
	TaxRateBasisPoints int
	CreatedBy          uuid.UUID
	CreatedAt          time.Time
}

type OrderPayment struct {
	Id        uuid.UUID
	CompanyId uuid.UUID
	OrderId   uuid.UUID
	Kind      string
	Method    string
	Amount    int64
	CreatedBy uuid.UUID
	CreatedAt time.Time
}

type OrderFilter struct {
	Status     string
	SearchText string
}
