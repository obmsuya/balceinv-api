package legacyimport

import "time"

type oldCompany struct {
	Name          string
	BusinessType  string
	Phone         *string
	Address       *string
	Tin           *string
	ReceiptHeader *string
	ReceiptFooter *string
}

type oldSettings struct {
	TaxRate                   float64
	Currency                  string
	DateFormat                string
	LowStockThreshold         int
	EmailNotificationsEnabled bool
	NotificationEmail         *string
	AlertSoundEnabled         bool
	AlertOnLowStock           bool
	AlertOnOutOfStock         bool
	AlertOnDeadStock          bool
	DeadStockDays             int
	PrintReceiptAutomatically bool
	ShowTaxOnReceipt          bool
	ShowBarcodesOnReceipt     bool
	PrinterEnabled            bool
	PrinterPort               string
	PrinterModel              string
	PrinterBaudRate           int
	PrinterPaperWidth         int
	OpenCashDrawer            bool
	EfdEnabled                bool
}

type oldRole struct {
	Id            int64
	Name          string
	PermissionIds []string
}

type oldUser struct {
	Id            int64
	Name          string
	Email         string
	PasswordHash  string
	RoleId        int64
	CreatedAt     time.Time
	PermissionIds []string
}

type oldProduct struct {
	Id             int64
	Name           string
	Sku            string
	Barcode        *string
	ParentId       *int64
	VariantLabel   string
	Price          float64
	CostPrice      float64
	WholesalePrice *float64
	WholesaleMin   int
	Quantity       int
	MinStock       int
	Category       *string
	Unit           string
	PiecesPerUnit  int
	Image          *string
	Metadata       *string
	IsDeleted      bool
	CreatedAt      time.Time
}

type oldBarcode struct {
	ProductId int64
	Code      string
	PackSize  int
}

type oldAddon struct {
	ProductId int64
	Name      string
	Price     float64
	IsActive  bool
}

type oldSale struct {
	Id            int64
	ReceiptNumber string
	UserId        int64
	TotalAmount   float64
	TaxAmount     float64
	AmountPaid    float64
	PaymentType   string
	CreatedAt     time.Time
	Lines         []oldSaleLine
}

type oldSaleLine struct {
	ProductId   int64
	Quantity    int
	UnitPrice   float64
	IsWholesale bool
}

type oldDiscount struct {
	Name         string
	ProductId    *int64
	DiscountType string
	Value        float64
	StartsAt     time.Time
	EndsAt       time.Time
	IsActive     bool
	CreatedBy    *int64
}

type oldSupplier struct {
	Name      string
	Phone     *string
	Notes     *string
	CreatedAt time.Time
}

type oldData struct {
	Company       *oldCompany
	Settings      *oldSettings
	Roles         map[int64]oldRole
	Users         []oldUser
	Products      []oldProduct
	Barcodes      []oldBarcode
	Addons        []oldAddon
	Sales         []oldSale
	Discounts     []oldDiscount
	Suppliers     []oldSupplier
	PurchaseCount int64
}

type oldTotals struct {
	Products   int64
	Users      int64
	Sales      int64
	SaleLines  int64
	Suppliers  int64
	SalesValue int64
	StockValue int64
}
