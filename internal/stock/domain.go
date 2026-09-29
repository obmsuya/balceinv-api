package stock

import (
	"time"

	"github.com/google/uuid"
)

type Movement struct {
	Id            uuid.UUID
	CompanyId     uuid.UUID
	ShopId        uuid.UUID
	ProductId     uuid.UUID
	Change        int
	QuantityAfter int
	Reason        string
	Reference     *string
	UserId        *uuid.UUID
	CreatedAt     time.Time
}
