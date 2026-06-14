package models

import "time"

type Product struct {
	ID             uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	Name           string     `gorm:"not null"                 json:"name"`
	SKU            string     `gorm:"uniqueIndex;not null"     json:"sku"`
	Barcode        *string    `gorm:"uniqueIndex;default:null" json:"barcode"`

	ParentID       *uint      `gorm:"default:null;index"       json:"parent_id"`
	VariantLabel   string     `gorm:"column:variant_label;default:''" json:"variant_label"`

	Price          float64    `gorm:"not null"                 json:"price"`
	CostPrice      float64    `gorm:"column:cost_price;not null" json:"cost_price"`

	Quantity       int        `gorm:"not null;default:0"       json:"quantity"`
	MinStock       int        `gorm:"column:min_stock;default:5" json:"min_stock"`

	WholesalePrice *float64   `gorm:"column:wholesale_price;default:null" json:"wholesale_price"`
	WholesaleMin   int        `gorm:"column:wholesale_min;default:10"    json:"wholesale_min"`

	Category       *string    `gorm:"default:null"             json:"category"`

	Unit           string     `gorm:"default:pcs"              json:"unit"`
	PiecesPerUnit  int        `gorm:"column:pieces_per_unit;default:1" json:"pieces_per_unit"`

	Image          *string    `gorm:"type:text;default:null"   json:"image"`
	Metadata       JSONMap    `gorm:"type:text;default:null"   json:"metadata"`

	CreatedAt      time.Time  `gorm:"autoCreateTime"           json:"created_at"`
	UpdatedAt      time.Time  `gorm:"autoUpdateTime"           json:"updated_at"`

	Parent         *Product        `gorm:"foreignKey:ParentID"  json:"parent,omitempty"`
	Variants       []Product       `gorm:"foreignKey:ParentID"  json:"variants,omitempty"`
	Addons         []ProductAddon  `gorm:"foreignKey:ProductID" json:"addons,omitempty"`
	Barcodes       []Barcode       `gorm:"foreignKey:ProductID" json:"barcodes,omitempty"`
	StockMovements []StockMovement `gorm:"foreignKey:ProductID" json:"stock_movements,omitempty"`
	PriceHistory   []PriceHistory  `gorm:"foreignKey:ProductID" json:"price_history,omitempty"`
	StockAlerts    []StockAlert    `gorm:"foreignKey:ProductID" json:"stock_alerts,omitempty"`
}

func (Product) TableName() string { return "products" }

type Barcode struct {
	ID        uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	ProductID uint      `gorm:"not null;index"           json:"product_id"`
	Code      string    `gorm:"uniqueIndex;not null"     json:"code"`
	PackSize  int       `gorm:"default:1"                json:"pack_size"`
	IsActive  bool      `gorm:"default:true"             json:"is_active"`
	CreatedAt time.Time `gorm:"autoCreateTime"           json:"created_at"`
}

func (Barcode) TableName() string { return "barcodes" }

type PriceHistory struct {
	ID        uint       `gorm:"primaryKey;autoIncrement" json:"id"`
	ProductID uint       `gorm:"not null;index"           json:"product_id"`
	OldPrice  *float64   `gorm:"default:null"             json:"old_price"`
	NewPrice  *float64   `gorm:"default:null"             json:"new_price"`
	UserID    *uint      `gorm:"default:null"             json:"user_id"`
	CreatedAt time.Time  `gorm:"autoCreateTime"           json:"created_at"`
}

func (PriceHistory) TableName() string { return "price_history" }

type StockAlert struct {
	ID              uint      `gorm:"primaryKey;autoIncrement" json:"id"`
	ProductID       uint      `gorm:"not null;index"           json:"product_id"`
	CurrentQuantity int       `gorm:"column:current_quantity;not null" json:"current_quantity"`
	Threshold       int       `gorm:"not null"                 json:"threshold"`
	AlertType       string    `gorm:"column:alert_type;not null" json:"alert_type"`
	IsSeen          bool      `gorm:"column:is_seen;default:false" json:"is_seen"`
	CreatedAt       time.Time `gorm:"autoCreateTime"           json:"created_at"`

	Product Product `gorm:"foreignKey:ProductID" json:"product,omitempty"`
}

func (StockAlert) TableName() string { return "stock_alerts" }