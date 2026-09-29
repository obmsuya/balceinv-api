package discounts

import (
	"time"

	"github.com/google/uuid"
)

const (
	KindPercent = "percent"
	KindFixed   = "fixed"
)

type Discount struct {
	Id        uuid.UUID
	CompanyId uuid.UUID
	Name      string
	ProductId *uuid.UUID
	Kind      string
	Value     int64
	StartsAt  time.Time
	EndsAt    time.Time
	IsActive  bool
	CreatedBy *uuid.UUID
	CreatedAt time.Time
	UpdatedAt time.Time
}
