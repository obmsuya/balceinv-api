package shops

import (
	"time"

	"github.com/google/uuid"
)

type ShopView struct {
	Id            uuid.UUID `json:"id"`
	Name          string    `json:"name"`
	Address       *string   `json:"address"`
	Phone         *string   `json:"phone"`
	ReceiptPrefix string    `json:"receipt_prefix"`
	IsActive      bool      `json:"is_active"`
	CreatedAt     time.Time `json:"created_at"`
	UpdatedAt     time.Time `json:"updated_at"`
}

type ShopRequest struct {
	Name          string  `json:"name" validate:"required,max=80"`
	Address       *string `json:"address" validate:"omitnil,max=200"`
	Phone         *string `json:"phone" validate:"omitnil,max=40"`
	ReceiptPrefix string  `json:"receipt_prefix" validate:"omitempty,max=12,alphanum"`
	IsActive      *bool   `json:"is_active"`
}

func toView(shop Shop) ShopView {
	return ShopView{
		Id:            shop.Id,
		Name:          shop.Name,
		Address:       shop.Address,
		Phone:         shop.Phone,
		ReceiptPrefix: shop.ReceiptPrefix,
		IsActive:      shop.IsActive,
		CreatedAt:     shop.CreatedAt,
		UpdatedAt:     shop.UpdatedAt,
	}
}
