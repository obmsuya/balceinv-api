package products

import (
	"time"

	"github.com/google/uuid"
)

type Product struct {
	Id             uuid.UUID
	CompanyId      uuid.UUID
	ParentId       *uuid.UUID
	Sku            string
	Name           string
	VariantLabel   string
	Price          int64
	CostPrice      int64
	WholesalePrice *int64
	WholesaleMin   int
	Category       *string
	Unit           string
	PiecesPerUnit  int
	ImageKey       *string
	Metadata       []byte
	IsActive       bool
	CreatedAt      time.Time
	UpdatedAt      time.Time
	Quantity       *int
	MinimumStock   *int
	VariantCount   int
}

type Barcode struct {
	Id        uuid.UUID
	CompanyId uuid.UUID
	ProductId uuid.UUID
	Code      string
	PackSize  int
	CreatedAt time.Time
}

type Addon struct {
	Id        uuid.UUID
	CompanyId uuid.UUID
	ProductId uuid.UUID
	Name      string
	Price     int64
	IsActive  bool
	CreatedAt time.Time
	UpdatedAt time.Time
}

type PriceChange struct {
	Id        uuid.UUID
	CompanyId uuid.UUID
	ProductId uuid.UUID
	OldPrice  int64
	NewPrice  int64
	ChangedBy *uuid.UUID
	CreatedAt time.Time
}
