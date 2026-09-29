package features

import (
	"time"

	"github.com/google/uuid"
)

const (
	AccountingOff    = "off"
	AccountingSimple = "simple"
	AccountingFull   = "full"
)

type Features struct {
	CompanyId             uuid.UUID
	SuppliersEnabled      bool
	PurchaseOrdersEnabled bool
	CustomersEnabled      bool
	CreditSalesEnabled    bool
	CustomerOrdersEnabled bool
	AccountingMode        string
	VatRegistered         bool
	VatNumber             *string
	UpdatedBy             *uuid.UUID
	UpdatedAt             time.Time
}

func Defaults(companyId uuid.UUID) Features {
	return Features{
		CompanyId:      companyId,
		AccountingMode: AccountingOff,
	}
}

func (companyFeatures Features) AccountingEnabled() bool {
	return companyFeatures.AccountingMode != AccountingOff
}
