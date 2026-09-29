package tenancy

import (
	"time"

	"github.com/google/uuid"
)

type Company struct {
	Id               uuid.UUID
	Name             string
	BusinessType     string
	Phone            *string
	Address          *string
	Tin              *string
	CurrencyCode     string
	CurrencyDecimals int
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type Shop struct {
	Id            uuid.UUID
	CompanyId     uuid.UUID
	Name          string
	ReceiptPrefix string
	IsActive      bool
	CreatedAt     time.Time
	UpdatedAt     time.Time
}
