package settings

import (
	"time"

	"github.com/google/uuid"
)

const DefaultReceiptNumberFormat = "{SHOP}-{DATE}-{COUNTER}"

type Settings struct {
	CompanyId                    uuid.UUID
	TaxRateBasisPoints           int
	DateFormat                   string
	ReceiptNumberFormat          string
	ReceiptLanguage              string
	EfdEnabled                   bool
	EfdEndpoint                  *string
	EfdApiKey                    *string
	LowStockThreshold            int
	EmailNotificationsEnabled    bool
	NotificationEmail            *string
	AlertSoundEnabled            bool
	AlertOnLowStock              bool
	AlertOnOutOfStock            bool
	AlertOnDeadStock             bool
	DeadStockDays                int
	PrintReceiptAutomatically    bool
	ShowTaxOnReceipt             bool
	ShowBarcodesOnReceipt        bool
	PrinterEnabled               bool
	PrinterPort                  string
	PrinterModel                 string
	PrinterBaudRate              int
	PrinterPaperWidth            int
	OpenCashDrawer               bool
	TillNumpadEnabled            bool
	CustomerDisplayEnabled       bool
	TillDiscountLimitBasisPoints int
	UpdatedBy                    *uuid.UUID
	CreatedAt                    time.Time
	UpdatedAt                    time.Time
}

type CompanyProfile struct {
	Id               uuid.UUID
	Name             string
	BusinessType     string
	Phone            *string
	Address          *string
	Tin              *string
	LogoKey          *string
	PrimaryColor     string
	CurrencyCode     string
	CurrencyDecimals int
	Timezone         string
	DefaultLocale    string
	ReceiptHeader    *string
	ReceiptFooter    *string
	UpdatedAt        time.Time
}
