package tenancy

import "github.com/google/uuid"

type SetupRequest struct {
	BusinessName     string  `json:"business_name" validate:"required,max=120"`
	BusinessType     string  `json:"business_type" validate:"omitempty,max=40"`
	Phone            *string `json:"phone" validate:"omitempty,max=40"`
	Address          *string `json:"address" validate:"omitempty,max=200"`
	Tin              *string `json:"tin" validate:"omitempty,max=40"`
	ShopName         string  `json:"shop_name" validate:"omitempty,max=80"`
	CurrencyCode     string  `json:"currency_code" validate:"omitempty,len=3,uppercase"`
	CurrencyDecimals *int    `json:"currency_decimals" validate:"omitempty,oneof=0 2"`
	OwnerName        string  `json:"owner_name" validate:"required,max=120"`
	OwnerEmail       string  `json:"owner_email" validate:"required,email,max=254"`
	OwnerPassword    string  `json:"owner_password" validate:"required,min=8,max=72"`
	OwnerMustReset   bool    `json:"-"`
}

type SetupStatusView struct {
	Configured   bool `json:"configured"`
	OldDataFound bool `json:"old_data_found"`
	SignupOpen   bool `json:"signup_open"`
}

type SetupResultView struct {
	CompanyId uuid.UUID `json:"company_id"`
	ShopId    uuid.UUID `json:"shop_id"`
	UserId    uuid.UUID `json:"user_id"`
}

type Branding struct {
	Name             string
	LogoKey          *string
	PrimaryColor     string
	CurrencyCode     string
	CurrencyDecimals int
	Timezone         string
	DefaultLocale    string
}

type ShopSummary struct {
	Id   uuid.UUID `json:"id"`
	Name string    `json:"name"`
}
