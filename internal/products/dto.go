package products

import (
	"time"

	"github.com/google/uuid"
)

type BarcodeView struct {
	Code     string `json:"code"`
	PackSize int    `json:"pack_size"`
}

type ProductView struct {
	Id                  uuid.UUID      `json:"id"`
	ParentId            *uuid.UUID     `json:"parent_id"`
	Sku                 string         `json:"sku"`
	Name                string         `json:"name"`
	VariantLabel        string         `json:"variant_label"`
	Price               int64          `json:"price"`
	CostPrice           int64          `json:"cost_price"`
	WholesalePrice      *int64         `json:"wholesale_price"`
	WholesaleMin        int            `json:"wholesale_min"`
	Category            *string        `json:"category"`
	Unit                string         `json:"unit"`
	PiecesPerUnit       int            `json:"pieces_per_unit"`
	ImageUrl            *string        `json:"image_url"`
	Metadata            map[string]any `json:"metadata"`
	PreferredSupplierId *uuid.UUID     `json:"preferred_supplier_id"`
	IsActive            bool           `json:"is_active"`
	Quantity            *int           `json:"quantity"`
	MinStock            *int           `json:"min_stock"`
	VariantCount        int            `json:"variant_count"`
	Barcodes            []BarcodeView  `json:"barcodes"`
	CreatedAt           time.Time      `json:"created_at"`
	UpdatedAt           time.Time      `json:"updated_at"`
}

type AddonView struct {
	Id        uuid.UUID `json:"id"`
	ProductId uuid.UUID `json:"product_id"`
	Name      string    `json:"name"`
	Price     int64     `json:"price"`
	IsActive  bool      `json:"is_active"`
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

type BarcodeInput struct {
	Code     string `json:"code" validate:"required,max=64"`
	PackSize int    `json:"pack_size" validate:"omitempty,gte=1,lte=10000"`
}

type ProductFields struct {
	Sku                 string         `json:"sku" validate:"required,max=64"`
	Name                string         `json:"name" validate:"required,max=160"`
	VariantLabel        string         `json:"variant_label" validate:"max=80"`
	Price               *int64         `json:"price" validate:"required,gte=0"`
	CostPrice           int64          `json:"cost_price" validate:"gte=0"`
	WholesalePrice      *int64         `json:"wholesale_price" validate:"omitnil,gte=0"`
	WholesaleMin        int            `json:"wholesale_min" validate:"omitempty,gte=1,lte=1000000"`
	Category            *string        `json:"category" validate:"omitnil,max=80"`
	Unit                string         `json:"unit" validate:"max=20"`
	PiecesPerUnit       int            `json:"pieces_per_unit" validate:"omitempty,gte=1,lte=100000"`
	Metadata            map[string]any `json:"metadata"`
	Barcodes            []BarcodeInput `json:"barcodes" validate:"omitempty,max=20,dive"`
	MinStock            *int           `json:"min_stock" validate:"omitnil,gte=0,lte=1000000000"`
	PreferredSupplierId *string        `json:"preferred_supplier_id" validate:"omitnil,max=36"`
}

type CreateProductRequest struct {
	ProductFields
	ParentId        *string `json:"parent_id" validate:"omitnil,uuid"`
	OpeningQuantity int     `json:"opening_quantity" validate:"gte=0,lte=1000000000"`
}

type UpdateProductRequest struct {
	ProductFields
	IsActive *bool `json:"is_active"`
}

type AddonRequest struct {
	Name     string `json:"name" validate:"required,max=80"`
	Price    *int64 `json:"price" validate:"required,gte=0"`
	IsActive *bool  `json:"is_active"`
}

type ListFilter struct {
	SearchText      string
	Category        string
	IncludeArchived bool
}

type ImportProblem struct {
	Row     int    `json:"row"`
	Column  string `json:"column,omitempty"`
	Problem string `json:"problem"`
}

type ImportResultView struct {
	RowsRead      int             `json:"rows_read"`
	Created       int             `json:"created"`
	Problems      []ImportProblem `json:"problems"`
	ProblemsTotal int             `json:"problems_total"`
}

type LookupView struct {
	Product  ProductView `json:"product"`
	PackSize int         `json:"pack_size"`
}
