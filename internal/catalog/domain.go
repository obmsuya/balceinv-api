package catalog

import (
	"time"

	"github.com/google/uuid"
)

type CatalogProduct struct {
	Id           uuid.UUID
	BusinessType string
	Name         string
	NameKey      string
	Category     *string
	SubCategory  *string
	Unit         string
	SkuPrefix    string
	DefaultPrice int64
	Metadata     []byte
	CreatedAt    time.Time
	UpdatedAt    time.Time
}

type BusinessTypeCount struct {
	BusinessType string `json:"business_type"`
	Count        int64  `json:"count"`
}
