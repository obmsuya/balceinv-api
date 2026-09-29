package shops

import (
	"time"

	"github.com/google/uuid"
)

type Shop struct {
	Id            uuid.UUID
	CompanyId     uuid.UUID
	Name          string
	Address       *string
	Phone         *string
	ReceiptPrefix string
	IsActive      bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
