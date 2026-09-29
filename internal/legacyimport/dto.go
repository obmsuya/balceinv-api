package legacyimport

import (
	"time"

	"github.com/google/uuid"
)

type ImportRequest struct {
	BusinessName string `json:"business_name" validate:"omitempty,max=120"`
	OwnerId      *int64 `json:"owner_id"`
}

type OldUserView struct {
	Id    int64  `json:"id"`
	Name  string `json:"name"`
	Email string `json:"email"`
}

type PreviewCountsView struct {
	Products  int `json:"products"`
	Users     int `json:"users"`
	Sales     int `json:"sales"`
	SaleLines int `json:"sale_lines"`
	Suppliers int `json:"suppliers"`
	Discounts int `json:"discounts"`
}

type PreviewView struct {
	BusinessName     string            `json:"business_name"`
	CurrencyCode     string            `json:"currency_code"`
	CurrencyDecimals int               `json:"currency_decimals"`
	Counts           PreviewCountsView `json:"counts"`
	FirstSaleAt      *time.Time        `json:"first_sale_at"`
	LastSaleAt       *time.Time        `json:"last_sale_at"`
	OwnerChoices     []OldUserView     `json:"owner_choices"`
	DefaultOwnerId   *int64            `json:"default_owner_id"`
	PasswordsKept    bool              `json:"passwords_kept"`
}

type CountCheckView struct {
	Old     int64 `json:"old"`
	New     int64 `json:"new"`
	Matched bool  `json:"matched"`
}

type SelfCheckView struct {
	Products   CountCheckView  `json:"products"`
	Users      CountCheckView  `json:"users"`
	Sales      CountCheckView  `json:"sales"`
	SaleLines  CountCheckView  `json:"sale_lines"`
	Suppliers  *CountCheckView `json:"suppliers"`
	SalesValue CountCheckView  `json:"sales_value"`
	StockValue CountCheckView  `json:"stock_value"`
	Matched    bool            `json:"matched"`
}

type TemporaryPasswordView struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
}

type RenamedView struct {
	Name string `json:"name"`
	Old  string `json:"old"`
	New  string `json:"new"`
}

type NoteView struct {
	Code  string `json:"code"`
	Count int    `json:"count"`
}

type ImportResultView struct {
	CompanyId          uuid.UUID               `json:"company_id"`
	OwnerEmail         string                  `json:"owner_email"`
	PasswordsKept      bool                    `json:"passwords_kept"`
	TemporaryPasswords []TemporaryPasswordView `json:"temporary_passwords"`
	SelfCheck          SelfCheckView           `json:"self_check"`
	RenamedSkus        []RenamedView           `json:"renamed_skus"`
	RenamedBarcodes    []RenamedView           `json:"renamed_barcodes"`
	ChangedEmails      []RenamedView           `json:"changed_emails"`
	Notes              []NoteView              `json:"notes"`
}
