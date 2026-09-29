package customers

import (
	"time"

	"github.com/google/uuid"
)

const (
	PaymentCash   = "cash"
	PaymentCard   = "card"
	PaymentMobile = "mobile"
)

type Customer struct {
	Id             uuid.UUID
	CompanyId      uuid.UUID
	Name           string
	Phone          *string
	Email          *string
	Address        *string
	Tin            *string
	CreditLimit    *int64
	OpeningBalance int64
	Notes          *string
	IsActive       bool
	LastVisitAt    *time.Time
	CreatedAt      time.Time
	UpdatedAt      time.Time
}

type Debt struct {
	CustomerId uuid.UUID
	OccurredAt time.Time
	Amount     int64
}

type Payment struct {
	Id         uuid.UUID
	CompanyId  uuid.UUID
	CustomerId uuid.UUID
	ShopId     *uuid.UUID
	Amount     int64
	Method     string
	Reference  *string
	ReceivedAt time.Time
	CreatedBy  uuid.UUID
	VoidedAt   *time.Time
	VoidedBy   *uuid.UUID
	VoidReason *string
}

type CustomerFilter struct {
	SearchText      string
	SearchPhone     string
	IncludeInactive bool
	SortRecent      bool
}

type StatementEntry struct {
	At        time.Time
	Kind      string
	SaleId    *uuid.UUID
	PaymentId *uuid.UUID
	Reference string
	Debit     int64
	Credit    int64
}
