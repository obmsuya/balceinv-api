package notifications

import (
	"time"

	"github.com/google/uuid"
)

type NotificationView struct {
	Id              uuid.UUID  `json:"id"`
	Kind            string     `json:"kind"`
	ProductId       uuid.UUID  `json:"product_id"`
	ProductName     string     `json:"product_name"`
	VariantLabel    string     `json:"variant_label"`
	Sku             string     `json:"sku"`
	Unit            string     `json:"unit"`
	Quantity        int        `json:"quantity"`
	MinStock        int        `json:"min_stock"`
	CurrentQuantity int        `json:"current_quantity"`
	IsRead          bool       `json:"is_read"`
	CreatedAt       time.Time  `json:"created_at"`
	ReadAt          *time.Time `json:"read_at"`
}

type UnreadCountView struct {
	Count int64 `json:"count"`
}

type ChangedCountView struct {
	Changed int64 `json:"changed"`
}
