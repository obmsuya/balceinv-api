package models

type Role struct {
	ID   uint   `gorm:"primaryKey;autoIncrement" json:"id"`
	Name string `gorm:"uniqueIndex;not null"     json:"name"`

	Users           []User           `gorm:"foreignKey:RoleID" json:"users,omitempty"`
	RolePermissions []RolePermission `gorm:"foreignKey:RoleID" json:"permissions,omitempty"`
}

func (Role) TableName() string { return "roles" }