package users

import (
	"time"

	"github.com/google/uuid"
)

type RoleSummary struct {
	Id      uuid.UUID `json:"id"`
	Name    string    `json:"name"`
	IsOwner bool      `json:"is_owner"`
}

type UserView struct {
	Id                 uuid.UUID   `json:"id"`
	Name               string      `json:"name"`
	Email              string      `json:"email"`
	RoleId             uuid.UUID   `json:"role_id"`
	Role               RoleSummary `json:"role"`
	ShopIds            []uuid.UUID `json:"shop_ids"`
	Locale             *string     `json:"locale"`
	IsActive           bool        `json:"is_active"`
	MustChangePassword bool        `json:"must_change_password"`
	CreatedAt          time.Time   `json:"created_at"`
	UpdatedAt          time.Time   `json:"updated_at"`
}

type CreateUserRequest struct {
	Name     string   `json:"name" validate:"required,max=120"`
	Email    string   `json:"email" validate:"required,email,max=254"`
	Password string   `json:"password" validate:"required,min=8,max=72"`
	RoleId   string   `json:"role_id" validate:"required,uuid"`
	ShopIds  []string `json:"shop_ids" validate:"omitempty,dive,uuid"`
}

type UpdateUserRequest struct {
	Name     string   `json:"name" validate:"required,max=120"`
	Email    string   `json:"email" validate:"required,email,max=254"`
	RoleId   string   `json:"role_id" validate:"required,uuid"`
	ShopIds  []string `json:"shop_ids" validate:"omitempty,dive,uuid"`
	IsActive *bool    `json:"is_active"`
}

type UpdatePasswordRequest struct {
	UserId      string `json:"user_id" validate:"required,uuid"`
	NewPassword string `json:"new_password" validate:"required,min=8,max=72"`
}

type AssignRoleRequest struct {
	UserId string `json:"user_id" validate:"required,uuid"`
	RoleId string `json:"role_id" validate:"required,uuid"`
}
