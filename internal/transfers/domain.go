package transfers

import (
	"time"

	"github.com/google/uuid"
)

type Transfer struct {
	Id         uuid.UUID
	CompanyId  uuid.UUID
	FromShopId uuid.UUID
	ToShopId   uuid.UUID
	Note       *string
	UserId     *uuid.UUID
	CreatedAt  time.Time
}

type TransferItem struct {
	CompanyId  uuid.UUID
	TransferId uuid.UUID
	ProductId  uuid.UUID
	Quantity   int
}
