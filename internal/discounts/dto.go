package discounts

import (
	"time"

	"github.com/google/uuid"
)

const (
	StatusScheduled = "scheduled"
	StatusActive    = "active"
	StatusExpired   = "expired"
	StatusStopped   = "stopped"
)

type DiscountRequest struct {
	Name      string    `json:"name" validate:"required,max=80"`
	ProductId *string   `json:"product_id" validate:"omitnil,uuid"`
	Kind      string    `json:"kind" validate:"required,oneof=percent fixed"`
	Value     int64     `json:"value" validate:"required,gt=0"`
	StartsAt  time.Time `json:"starts_at" validate:"required"`
	EndsAt    time.Time `json:"ends_at" validate:"required"`
	IsActive  *bool     `json:"is_active"`
}

type DiscountView struct {
	Id           uuid.UUID  `json:"id"`
	Name         string     `json:"name"`
	ProductId    *uuid.UUID `json:"product_id"`
	ProductName  *string    `json:"product_name"`
	VariantLabel *string    `json:"variant_label"`
	Kind         string     `json:"kind"`
	Value        int64      `json:"value"`
	StartsAt     time.Time  `json:"starts_at"`
	EndsAt       time.Time  `json:"ends_at"`
	IsActive     bool       `json:"is_active"`
	Status       string     `json:"status"`
	CreatedAt    time.Time  `json:"created_at"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

func statusAt(discount Discount, now time.Time) string {
	switch {
	case !discount.IsActive:
		return StatusStopped
	case now.Before(discount.StartsAt):
		return StatusScheduled
	case !now.Before(discount.EndsAt):
		return StatusExpired
	default:
		return StatusActive
	}
}
