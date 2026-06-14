package models

import "time"

// Supplier maps to the `suppliers` table.
// Kept deliberately simple — just name, phone, and notes as your schema intended.
type Supplier struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	Name      string    `gorm:"not null"                 json:"name"`
	Phone     *string   `gorm:"default:null"             json:"phone"`
	Notes     *string   `gorm:"default:null"             json:"notes"`
	CreatedAt time.Time `gorm:"autoCreateTime"           json:"created_at"`

	Purchases []Purchase `gorm:"foreignKey:SupplierID" json:"purchases,omitempty"`
}

func (Supplier) TableName() string { return "suppliers" }

// Purchase maps to the `purchases` table.
// Represents a stock purchase from a supplier — the inbound side of inventory.
type Purchase struct {
	ID            uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	SupplierID    *uint     `gorm:"default:null"             json:"supplier_id"`
	InvoiceNumber *string   `gorm:"column:invoice_number;default:null" json:"invoice_number"`
	TotalCost     float64   `gorm:"column:total_cost;not null" json:"total_cost"`
	SupplierName  *string   `gorm:"column:supplier_name;default:null" json:"supplier_name"`
	UserID        *uint     `gorm:"default:null"             json:"user_id"`
	CreatedAt     time.Time `gorm:"autoCreateTime"           json:"created_at"`

	// Associations
	Supplier *Supplier      `gorm:"foreignKey:SupplierID" json:"supplier,omitempty"`
	User     *User          `gorm:"foreignKey:UserID"     json:"user,omitempty"`
	Items    []PurchaseItem `gorm:"foreignKey:PurchaseID" json:"items,omitempty"`
}

func (Purchase) TableName() string { return "purchases" }

// PurchaseItem maps to the `purchase_items` table.
// Each row is one product line within a purchase order.
// productName is stored as text (not just a FK) so the record
// remains readable even if the product is later deleted.
type PurchaseItem struct {
	ID          uint    `gorm:"primaryKey;autoIncrement" json:"id"`
	PurchaseID  uint    `gorm:"not null;index"           json:"purchase_id"`
	ProductID   *uint   `gorm:"default:null"             json:"product_id"`
	ProductName string  `gorm:"column:product_name;not null" json:"product_name"`
	Quantity    int     `gorm:"not null"                 json:"quantity"`
	UnitCost    float64 `gorm:"column:unit_cost;not null" json:"unit_cost"`
	TotalCost   float64 `gorm:"column:total_cost;not null" json:"total_cost"`

	Product *Product `gorm:"foreignKey:ProductID" json:"product,omitempty"`
}

func (PurchaseItem) TableName() string { return "purchase_items" }