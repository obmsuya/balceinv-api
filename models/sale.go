package models

import "time"

// Sale maps to the `sales` table.
// Each sale is one checkout transaction — one receipt, one or many items.
type Sale struct {
	ID            uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	ReceiptNumber string    `gorm:"column:receipt_number;uniqueIndex;not null" json:"receipt_number"`
	UserID        uint      `gorm:"not null;index"           json:"user_id"`
	TotalAmount   float64   `gorm:"column:total_amount;not null" json:"total_amount"`
	PaymentType   string    `gorm:"column:payment_type;not null" json:"payment_type"` // cash | card | mobile
	SaleType      string    `gorm:"column:sale_type;default:retail" json:"sale_type"` // retail | wholesale
	TaxAmount     float64   `gorm:"column:tax_amount;default:0"  json:"tax_amount"`
	CreatedAt     time.Time `gorm:"autoCreateTime"           json:"created_at"`

	// Associations
	User  User       `gorm:"foreignKey:UserID"  json:"user,omitempty"`
	Items []SaleItem `gorm:"foreignKey:SaleID"  json:"items,omitempty"`
}

func (Sale) TableName() string { return "sales" }

// SaleItem maps to the `sale_items` table.
// Each row is one product line within a sale.
// unitPrice is stored at the time of sale so price changes never affect history.
type SaleItem struct {
	ID         uint    `gorm:"primaryKey;autoIncrement" json:"id"`
	SaleID     uint    `gorm:"not null;index"           json:"sale_id"`
	ProductID  uint    `gorm:"not null;index"           json:"product_id"`
	Quantity   int     `gorm:"not null"                 json:"quantity"`
	UnitPrice  float64 `gorm:"column:unit_price;not null" json:"unit_price"`
	TotalPrice float64 `gorm:"column:total_price;not null" json:"total_price"`
	IsWholesale bool   `gorm:"column:is_wholesale;default:false" json:"is_wholesale"`

	// Associations
	Product Product `gorm:"foreignKey:ProductID" json:"product,omitempty"`
	Sale    Sale    `gorm:"foreignKey:SaleID"    json:"-"`
}

func (SaleItem) TableName() string { return "sale_items" }