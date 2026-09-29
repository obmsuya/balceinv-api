package customers

import (
	"time"

	"github.com/google/uuid"
)

type CustomerRequest struct {
	Name           string  `json:"name" validate:"required,max=120"`
	Phone          *string `json:"phone" validate:"omitnil,max=20"`
	Email          *string `json:"email" validate:"omitnil,max=120"`
	Address        *string `json:"address" validate:"omitnil,max=200"`
	Tin            *string `json:"tin" validate:"omitnil,max=40"`
	CreditLimit    *int64  `json:"credit_limit" validate:"omitnil,gte=0,lte=1000000000000000"`
	OpeningBalance int64   `json:"opening_balance" validate:"gte=0,lte=1000000000000000"`
	Notes          *string `json:"notes" validate:"omitnil,max=500"`
}

type PaymentRequest struct {
	Amount    int64   `json:"amount" validate:"required,gt=0,lte=1000000000000000"`
	Method    string  `json:"method" validate:"required,oneof=cash card mobile"`
	Reference *string `json:"reference" validate:"omitnil,max=80"`
}

type VoidRequest struct {
	Reason string `json:"reason" validate:"required,max=200"`
}

type AgingView struct {
	Days0To30  int64 `json:"days_0_30"`
	Days31To60 int64 `json:"days_31_60"`
	Days61To90 int64 `json:"days_61_90"`
	DaysOver90 int64 `json:"days_over_90"`
}

type CustomerView struct {
	Id              uuid.UUID  `json:"id"`
	Name            string     `json:"name"`
	Phone           *string    `json:"phone"`
	Email           *string    `json:"email"`
	Address         *string    `json:"address"`
	Tin             *string    `json:"tin"`
	CreditLimit     *int64     `json:"credit_limit"`
	OpeningBalance  int64      `json:"opening_balance"`
	Notes           *string    `json:"notes"`
	IsActive        bool       `json:"is_active"`
	Balance         int64      `json:"balance"`
	AvailableCredit *int64     `json:"available_credit"`
	OverdueAmount   int64      `json:"overdue_amount"`
	OldestDebtAt    *time.Time `json:"oldest_debt_at"`
	Aging           AgingView  `json:"aging"`
	LastVisitAt     *time.Time `json:"last_visit_at"`
	CreatedAt       time.Time  `json:"created_at"`
	UpdatedAt       time.Time  `json:"updated_at"`
}

type DebtorsTotalsView struct {
	Balance int64     `json:"balance"`
	Aging   AgingView `json:"aging"`
}

type DebtorsView struct {
	AsOf      string            `json:"as_of"`
	Customers []CustomerView    `json:"customers"`
	Totals    DebtorsTotalsView `json:"totals"`
}

type PaymentView struct {
	Id            uuid.UUID  `json:"id"`
	CustomerId    uuid.UUID  `json:"customer_id"`
	Amount        int64      `json:"amount"`
	Method        string     `json:"method"`
	Reference     *string    `json:"reference"`
	ReceivedAt    time.Time  `json:"received_at"`
	ShopName      *string    `json:"shop_name"`
	CreatedByName string     `json:"created_by_name"`
	VoidedAt      *time.Time `json:"voided_at"`
	VoidedByName  *string    `json:"voided_by_name"`
	VoidReason    *string    `json:"void_reason"`
}

type CustomerSaleView struct {
	Id            uuid.UUID `json:"id"`
	ReceiptNumber string    `json:"receipt_number"`
	ShopName      string    `json:"shop_name"`
	Total         int64     `json:"total"`
	CreditAmount  int64     `json:"credit_amount"`
	CreatedAt     time.Time `json:"created_at"`
}

type StatementCustomerView struct {
	Id    uuid.UUID `json:"id"`
	Name  string    `json:"name"`
	Phone *string   `json:"phone"`
}

type StatementEntryView struct {
	At        time.Time  `json:"at"`
	Kind      string     `json:"kind"`
	SaleId    *uuid.UUID `json:"sale_id"`
	PaymentId *uuid.UUID `json:"payment_id"`
	Reference string     `json:"reference"`
	Debit     int64      `json:"debit"`
	Credit    int64      `json:"credit"`
	Balance   int64      `json:"balance"`
}

type StatementView struct {
	Customer       StatementCustomerView `json:"customer"`
	From           string                `json:"from"`
	To             string                `json:"to"`
	OpeningBalance int64                 `json:"opening_balance"`
	Entries        []StatementEntryView  `json:"entries"`
	TotalDebits    int64                 `json:"total_debits"`
	TotalCredits   int64                 `json:"total_credits"`
	ClosingBalance int64                 `json:"closing_balance"`
}
