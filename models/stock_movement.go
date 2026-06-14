package models

import "time"

// StockMovement maps to the `stock_movements` table.
// Every time stock changes — whether from a sale, a purchase, a manual adjustment,
// or damage — a record is inserted here. This gives you a full audit trail
// of every stock change ever made, which is essential for inventory accuracy.
type StockMovement struct {
	ID          uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	ProductID   uint      `gorm:"not null;index"           json:"product_id"`
	Change      int       `gorm:"not null"                 json:"change"`       // positive = stock added, negative = stock removed
	NewQuantity int       `gorm:"column:new_quantity;not null" json:"new_quantity"` // stock level after this change
	Reason      string    `gorm:"not null"                 json:"reason"`      // sale | purchase | adjust | damage
	Reference   *string   `gorm:"default:null"             json:"reference"`   // receipt number or purchase ref
	UserID      *uint     `gorm:"default:null"             json:"user_id"`
	CreatedAt   time.Time `gorm:"autoCreateTime"           json:"created_at"`

	// Associations
	Product Product `gorm:"foreignKey:ProductID" json:"product,omitempty"`
	User    *User   `gorm:"foreignKey:UserID"    json:"user,omitempty"`
}

func (StockMovement) TableName() string { return "stock_movements" }