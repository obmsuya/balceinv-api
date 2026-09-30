package auth

import (
	"github.com/chrisostomemataba/balceinv-api/internal/access"
	"github.com/chrisostomemataba/balceinv-api/internal/features"
	"github.com/chrisostomemataba/balceinv-api/internal/tenancy"
	"github.com/google/uuid"
)

type LoginRequest struct {
	Email    string `json:"email" validate:"required,max=254"`
	Password string `json:"password" validate:"required,max=72"`
}

type SwitchShopRequest struct {
	ShopId string `json:"shop_id" validate:"required,uuid"`
}

type LanguageRequest struct {
	Locale *string `json:"locale" validate:"omitnil,oneof=en sw"`
}

type TourRequest struct {
	Tour string `json:"tour" validate:"required,max=40,alpha,lowercase"`
}

type CurrentUserView struct {
	Id                 uuid.UUID               `json:"id"`
	Name               string                  `json:"name"`
	Email              string                  `json:"email"`
	Role               string                  `json:"role"`
	RoleId             uuid.UUID               `json:"role_id"`
	IsOwner            bool                    `json:"is_owner"`
	CompanyId          uuid.UUID               `json:"company_id"`
	CompanyName        string                  `json:"company_name"`
	Branding           BrandingView            `json:"branding"`
	ShopId             *uuid.UUID              `json:"shop_id"`
	Shops              []tenancy.ShopSummary   `json:"shops"`
	Permissions        []access.PermissionView `json:"permissions"`
	Locale             *string                 `json:"locale"`
	MustChangePassword bool                    `json:"must_change_password"`
	Features           features.FeaturesView   `json:"features"`
	SeenTours          []string                `json:"seen_tours"`
}

type BrandingView struct {
	LogoUrl          *string `json:"logo_url"`
	PrimaryColor     string  `json:"primary_color"`
	CurrencyCode     string  `json:"currency_code"`
	CurrencyDecimals int     `json:"currency_decimals"`
	Timezone         string  `json:"timezone"`
	DefaultLocale    string  `json:"default_locale"`
}

type LoginView struct {
	User         CurrentUserView `json:"user"`
	SessionToken string          `json:"session_token,omitempty"`
}
