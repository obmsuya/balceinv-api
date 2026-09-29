package access

import (
	"time"

	"github.com/google/uuid"
)

type PermissionView struct {
	Id          string `json:"id"`
	Resource    string `json:"resource"`
	Action      string `json:"action"`
	Description string `json:"description"`
}

type RoleView struct {
	Id            uuid.UUID `json:"id"`
	Name          string    `json:"name"`
	IsOwner       bool      `json:"is_owner"`
	UserCount     int64     `json:"user_count"`
	PermissionIds []string  `json:"permission_ids"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type CreateRoleRequest struct {
	Name          string   `json:"name" validate:"required,max=60"`
	PermissionIds []string `json:"permission_ids" validate:"omitempty,dive,required,max=64"`
}

type UpdateRoleRequest struct {
	Name string `json:"name" validate:"required,max=60"`
}

type AssignRolePermissionsRequest struct {
	RoleId        string   `json:"role_id" validate:"required,uuid"`
	PermissionIds []string `json:"permission_ids" validate:"omitempty,dive,required,max=64"`
}

type AssignUserPermissionsRequest struct {
	UserId        string   `json:"user_id" validate:"required,uuid"`
	PermissionIds []string `json:"permission_ids" validate:"omitempty,dive,required,max=64"`
}
