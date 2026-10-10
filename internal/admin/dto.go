package admin

import (
	"time"

	"github.com/google/uuid"
)

const (
	RoleAdmin   = "admin"
	RoleSupport = "support"
)

type Staff struct {
	Id           uuid.UUID
	Email        string
	Name         string
	PasswordHash string
	TotpSecret   string
	Role         string
	IsActive     bool
}

type SignInRequest struct {
	Email    string `json:"email" validate:"required,email,max=254"`
	Password string `json:"password" validate:"required,max=200"`
	Code     string `json:"code" validate:"required,min=6,max=8"`
}

type StaffView struct {
	Id    uuid.UUID `json:"id"`
	Email string    `json:"email"`
	Name  string    `json:"name"`
	Role  string    `json:"role"`
}

type ShopRowView struct {
	CompanyId        uuid.UUID  `json:"company_id"`
	Name             string     `json:"name"`
	Phone            *string    `json:"phone"`
	OwnerName        *string    `json:"owner_name"`
	OwnerEmail       *string    `json:"owner_email"`
	UserCount        int64      `json:"user_count"`
	ShopCount        int64      `json:"shop_count"`
	SalesLast30Days  int64      `json:"sales_last_30_days"`
	LastSaleAt       *time.Time `json:"last_sale_at"`
	IsTrial          *bool      `json:"is_trial"`
	SubscriptionEnds *time.Time `json:"subscription_ends_at"`
	CreatedAt        time.Time  `json:"created_at"`
}

type ShopUserView struct {
	Id                 uuid.UUID `json:"id"`
	Name               string    `json:"name"`
	Email              string    `json:"email"`
	RoleName           string    `json:"role_name"`
	IsOwner            bool      `json:"is_owner"`
	IsActive           bool      `json:"is_active"`
	MustChangePassword bool      `json:"must_change_password"`
}

type ShopDetailView struct {
	ShopRowView
	SubscriptionId string         `json:"subscription_id"`
	CurrencyCode   string         `json:"currency_code"`
	ShopNames      []string       `json:"shop_names"`
	Users          []ShopUserView `json:"users"`
	RecentAudit    []AuditView    `json:"recent_audit"`
}

type ShopPage struct {
	Items []ShopRowView `json:"items"`
	Total int64         `json:"total"`
}

type CreateShopRequest struct {
	BusinessName     string `json:"business_name" validate:"required,max=120"`
	ShopName         string `json:"shop_name" validate:"omitempty,max=80"`
	OwnerName        string `json:"owner_name" validate:"required,max=120"`
	OwnerEmail       string `json:"owner_email" validate:"required,email,max=254"`
	CurrencyCode     string `json:"currency_code" validate:"omitempty,len=3,uppercase"`
	CurrencyDecimals *int   `json:"currency_decimals" validate:"omitnil,oneof=0 2"`
}

type CreatedShopView struct {
	CompanyId       uuid.UUID `json:"company_id"`
	OwnerEmail      string    `json:"owner_email"`
	OneTimePassword string    `json:"one_time_password"`
}

type ExtendTrialRequest struct {
	Days int `json:"days" validate:"required,gte=1,lte=90"`
}

type ExtendedTrialView struct {
	TrialEndsAt time.Time `json:"trial_ends_at"`
}

type PasswordResetView struct {
	Email           string `json:"email"`
	OneTimePassword string `json:"one_time_password"`
}

type SupportMessageView struct {
	Id            uuid.UUID  `json:"id"`
	CompanyId     uuid.UUID  `json:"company_id"`
	CompanyName   string     `json:"company_name"`
	UserName      *string    `json:"user_name"`
	Topic         string     `json:"topic"`
	Message       string     `json:"message"`
	ContactEmail  *string    `json:"contact_email"`
	ContactPhone  *string    `json:"contact_phone"`
	EmailStatus   string     `json:"email_status"`
	CreatedAt     time.Time  `json:"created_at"`
	HandledAt     *time.Time `json:"handled_at"`
	HandledByName *string    `json:"handled_by_name"`
}

type AuditView struct {
	Id          uuid.UUID  `json:"id"`
	StaffName   string     `json:"staff_name"`
	Action      string     `json:"action"`
	CompanyId   *uuid.UUID `json:"company_id"`
	CompanyName *string    `json:"company_name"`
	Details     string     `json:"details"`
	CreatedAt   time.Time  `json:"created_at"`
}
