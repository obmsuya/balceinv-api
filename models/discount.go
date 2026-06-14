package models

import "time"

type Discount struct {
	ID           uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Name         string    `gorm:"not null"                 json:"name"`
	ProductID    *uint     `gorm:"default:null;index"       json:"product_id"`
	DiscountType string    `gorm:"column:discount_type;not null" json:"discount_type"`
	Value        float64   `gorm:"not null"                 json:"value"`
	StartsAt     time.Time `gorm:"column:starts_at;not null" json:"starts_at"`
	EndsAt       time.Time `gorm:"column:ends_at;not null"   json:"ends_at"`
	IsActive     bool      `gorm:"column:is_active;default:true" json:"is_active"`
	CreatedBy    uint      `gorm:"column:created_by;not null" json:"created_by"`
	CreatedAt    time.Time `gorm:"autoCreateTime"            json:"created_at"`
	UpdatedAt    time.Time `gorm:"autoUpdateTime"            json:"updated_at"`

	Product *Product `gorm:"foreignKey:ProductID" json:"product,omitempty"`
	Creator User     `gorm:"foreignKey:CreatedBy" json:"creator,omitempty"`
}

func (Discount) TableName() string { return "discounts" }