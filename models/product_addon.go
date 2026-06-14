package models

import "time"

type ProductAddon struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	ProductID uint      `gorm:"not null;index"           json:"product_id"`
	Name      string    `gorm:"not null"                 json:"name"`
	Price     float64   `gorm:"not null;default:0"       json:"price"`
	IsActive  bool      `gorm:"column:is_active;default:true" json:"is_active"`
	CreatedAt time.Time `gorm:"autoCreateTime"           json:"created_at"`
	UpdatedAt time.Time `gorm:"autoUpdateTime"           json:"updated_at"`

	Product Product `gorm:"foreignKey:ProductID" json:"product,omitempty"`
}

func (ProductAddon) TableName() string { return "product_addons" }