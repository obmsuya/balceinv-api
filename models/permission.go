package models

import "time"

// Permission maps to the `permissions` table.
// action is restricted to: 'view', 'create', 'edit', 'delete'
type Permission struct {
	ID          uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Name        string    `gorm:"uniqueIndex;not null"     json:"name"`
	Resource    string    `gorm:"not null"                 json:"resource"`
	Action      string    `gorm:"not null"                 json:"action"` // view | create | edit | delete
	Description *string   `gorm:"default:null"             json:"description"`
	CreatedAt   time.Time `gorm:"autoCreateTime"           json:"created_at"`
}

func (Permission) TableName() string { return "permissions" }

// RolePermission maps to the `role_permissions` join table.
// It links a Role to a Permission (many-to-many through this table).
type RolePermission struct {
	ID           uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	RoleID       uint       `gorm:"not null;index"           json:"role_id"`
	PermissionID uint       `gorm:"not null;index"           json:"permission_id"`
	CreatedAt    time.Time  `gorm:"autoCreateTime"           json:"created_at"`

	// Belongs-to associations — GORM populates these when you use Preload()
	Role       Role       `gorm:"foreignKey:RoleID"       json:"role,omitempty"`
	Permission Permission `gorm:"foreignKey:PermissionID" json:"permission,omitempty"`
}

func (RolePermission) TableName() string { return "role_permissions" }

// UserPermission maps to the `user_permissions` join table.
// Allows granting permissions directly to a user, bypassing their role.
type UserPermission struct {
	ID           uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID       uint      `gorm:"not null;index"           json:"user_id"`
	PermissionID uint      `gorm:"not null;index"           json:"permission_id"`
	CreatedAt    time.Time `gorm:"autoCreateTime"           json:"created_at"`

	Permission Permission `gorm:"foreignKey:PermissionID" json:"permission,omitempty"`
}

func (UserPermission) TableName() string { return "user_permissions" }