package notifications

import (
	"time"

	"github.com/google/uuid"
)

const (
	KindLowStock   = "low_stock"
	KindOutOfStock = "out_of_stock"
)

type Notification struct {
	Id        uuid.UUID
	CompanyId uuid.UUID
	ShopId    uuid.UUID
	ProductId uuid.UUID
	Kind      string
	Quantity  int
	MinStock  int
	IsRead    bool
	CreatedAt time.Time
	ReadAt    *time.Time
}
