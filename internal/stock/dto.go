package stock

import (
	"time"

	"github.com/google/uuid"
)

const (
	StatusOk  = "ok"
	StatusLow = "low"
	StatusOut = "out"
)

type LevelView struct {
	ProductId    uuid.UUID  `json:"product_id"`
	ParentId     *uuid.UUID `json:"parent_id"`
	Name         string     `json:"name"`
	VariantLabel string     `json:"variant_label"`
	Sku          string     `json:"sku"`
	Unit         string     `json:"unit"`
	Category     *string    `json:"category"`
	Price        int64      `json:"price"`
	CostPrice    int64      `json:"cost_price"`
	Quantity     int        `json:"quantity"`
	MinStock     int        `json:"min_stock"`
	Status       string     `json:"status"`
}

type SummaryView struct {
	ProductCount int64 `json:"product_count"`
	TotalUnits   int64 `json:"total_units"`
	ValueAtCost  int64 `json:"value_at_cost"`
	ValueAtPrice int64 `json:"value_at_price"`
	LowCount     int64 `json:"low_count"`
	OutCount     int64 `json:"out_count"`
}

type MovementView struct {
	Id            uuid.UUID  `json:"id"`
	ProductId     uuid.UUID  `json:"product_id"`
	ProductName   string     `json:"product_name"`
	VariantLabel  string     `json:"variant_label"`
	Sku           string     `json:"sku"`
	Unit          string     `json:"unit"`
	Change        int        `json:"change"`
	QuantityAfter int        `json:"quantity_after"`
	Reason        string     `json:"reason"`
	Reference     *string    `json:"reference"`
	UserId        *uuid.UUID `json:"user_id"`
	UserName      *string    `json:"user_name"`
	CreatedAt     time.Time  `json:"created_at"`
}

type LevelFilter struct {
	SearchText string
	Status     string
}

type MovementFilter struct {
	ProductId *uuid.UUID
	Reason    string
	From      *time.Time
	To        *time.Time
}

type AdjustmentRequest struct {
	ProductId string  `json:"product_id" validate:"required,uuid"`
	Reason    string  `json:"reason" validate:"required,oneof=purchase return adjustment damage"`
	Change    int     `json:"change" validate:"required,gte=-1000000,lte=1000000"`
	Reference *string `json:"reference" validate:"omitnil,max=200"`
}

func statusFor(quantity int, minimumStock int) string {
	if quantity == 0 {
		return StatusOut
	}
	if quantity <= minimumStock {
		return StatusLow
	}
	return StatusOk
}
