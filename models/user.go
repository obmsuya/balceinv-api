package models

import "time"

// PasswordHash is marked json:"-" so it is NEVER included in any API response —
// this is the Go equivalent of excluding it from your Drizzle select queries.
type User struct {
	ID           uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Name         string    `gorm:"not null"                 json:"name"`
	Email        string    `gorm:"uniqueIndex;not null"     json:"email"`
	PasswordHash string    `gorm:"column:password_hash;not null" json:"-"`
	RoleID       uint      `gorm:"not null"                 json:"role_id"`
	CompanyID    uint      `gorm:"not null;index"           json:"company_id"`
	CreatedAt    time.Time `gorm:"autoCreateTime"           json:"created_at"`
	UpdatedAt    time.Time `gorm:"autoUpdateTime"           json:"updated_at"`

	// Associations
	Role            Role             `gorm:"foreignKey:RoleID"  json:"role,omitempty"`
	Sessions        []Session        `gorm:"foreignKey:UserID"  json:"-"`
	UserPermissions []UserPermission `gorm:"foreignKey:UserID"  json:"permissions,omitempty"`
}

func (User) TableName() string { return "users" }

type Session struct {
	ID           uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID       uint      `gorm:"not null;index"           json:"user_id"`
	RefreshToken string    `gorm:"uniqueIndex;not null"     json:"refresh_token"`
	ExpiresAt    time.Time `gorm:"not null"                 json:"expires_at"`
	CreatedAt    time.Time `gorm:"autoCreateTime"           json:"created_at"`
}

func (Session) TableName() string { return "sessions" }


type LoginLog struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	UserID    uint      `gorm:"not null;index"           json:"user_id"`
	LoginTime time.Time `gorm:"autoCreateTime"           json:"login_time"`
}

func (LoginLog) TableName() string { return "login_logs" }