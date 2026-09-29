package transfers

import (
	"time"

	"github.com/google/uuid"
)

type TransferItemRequest struct {
	ProductId string `json:"product_id" validate:"required,uuid"`
	Quantity  int    `json:"quantity" validate:"required,gte=1,lte=1000000"`
}

type TransferRequest struct {
	FromShopId *string               `json:"from_shop_id" validate:"omitnil,uuid"`
	ToShopId   string                `json:"to_shop_id" validate:"required,uuid"`
	Note       *string               `json:"note" validate:"omitnil,max=200"`
	Items      []TransferItemRequest `json:"items" validate:"required,min=1,max=200,dive"`
}

type TransferItemView struct {
	ProductId    uuid.UUID `json:"product_id"`
	ProductName  string    `json:"product_name"`
	VariantLabel string    `json:"variant_label"`
	Sku          string    `json:"sku"`
	Unit         string    `json:"unit"`
	Quantity     int       `json:"quantity"`
}

type TransferView struct {
	Id           uuid.UUID          `json:"id"`
	FromShopId   uuid.UUID          `json:"from_shop_id"`
	FromShopName string             `json:"from_shop_name"`
	ToShopId     uuid.UUID          `json:"to_shop_id"`
	ToShopName   string             `json:"to_shop_name"`
	Note         *string            `json:"note"`
	UserName     *string            `json:"user_name"`
	ItemCount    int                `json:"item_count"`
	TotalUnits   int                `json:"total_units"`
	CreatedAt    time.Time          `json:"created_at"`
	Items        []TransferItemView `json:"items,omitempty"`
}
