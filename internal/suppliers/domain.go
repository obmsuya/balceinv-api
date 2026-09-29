package suppliers

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

const (
	PurchaseReceived  = "received"
	PurchaseCancelled = "cancelled"

	OrderDraft          = "draft"
	OrderSent           = "sent"
	OrderPartlyReceived = "partly_received"
	OrderReceived       = "received"
	OrderCancelled      = "cancelled"

	PaymentPaid     = "paid"
	PaymentPartPaid = "part_paid"
	PaymentUnpaid   = "unpaid"

	EntryOpeningBalance = "opening_balance"
	EntryPurchase       = "purchase"
	EntryPayment        = "payment"
	EntryReturn         = "return"

	documentPurchase       = "purchase"
	documentOrder          = "purchase_order"
	documentPayment        = "supplier_payment"
	documentReturn         = "supplier_return"
	stockReasonPurchase    = "purchase"
	stockReasonReturn      = "return"
	dateLayout             = "2006-01-02"
	maximumStatementDays   = 366
	defaultStatementDays   = 90
	futureTolerance        = 5 * time.Minute
	basisPointsDenominator = 10000
	MaximumAttachmentBytes = 2 << 20
)

var documentPrefixes = map[string]string{
	documentPurchase: "PUR",
	documentOrder:    "PO",
	documentPayment:  "PAY",
	documentReturn:   "RET",
}

var (
	ErrFeatureOff            = errors.New("this feature is turned off in Settings → Features")
	ErrNoActiveShop          = errors.New("choose a shop first")
	ErrShopNotFound          = errors.New("shop not found or closed")
	ErrShopNotAssigned       = errors.New("you can only record stock for a shop you work in")
	ErrSupplierNotFound      = errors.New("supplier not found")
	ErrSupplierInactive      = errors.New("this supplier is turned off; turn it back on to use it")
	ErrSupplierNameTaken     = errors.New("another active supplier already has this name")
	ErrNameRequired          = errors.New("enter the supplier's name")
	ErrInvalidPhone          = errors.New("enter a phone number like 0712 345 678 or +255 712 345 678")
	ErrInvalidEmail          = errors.New("enter a valid email address")
	ErrProductNotFound       = errors.New("one of the products was not found")
	ErrDuplicateProduct      = errors.New("each product can appear only once")
	ErrMustPayInFull         = errors.New("stock without a supplier must be paid in full when it arrives")
	ErrPaidMoreThanTotal     = errors.New("the amount paid now cannot be more than the total")
	ErrOverpayment           = errors.New("this is more than you owe this supplier")
	ErrPurchaseNotFound      = errors.New("stock arrival not found")
	ErrAlreadyCancelled      = errors.New("this was already cancelled")
	ErrStockAlreadyUsed      = errors.New("some of this stock was already sold or moved, so it cannot be cancelled")
	ErrPaymentNotFound       = errors.New("payment not found")
	ErrPaymentAlreadyVoided  = errors.New("this payment was already voided")
	ErrPaymentLocked         = errors.New("this payment belongs to stock without a supplier; cancel that stock arrival instead")
	ErrReturnNotFound        = errors.New("return not found")
	ErrOrderNotFound         = errors.New("order not found")
	ErrOrderClosed           = errors.New("this order is already fully received or cancelled")
	ErrOrderNotDraft         = errors.New("only a draft order can be marked as sent")
	ErrOrderSupplierMismatch = errors.New("the stock must come from the supplier on the order")
	ErrDateInFuture          = errors.New("the date cannot be in the future")
	ErrInvalidDate           = errors.New("use dates like 2026-09-30")
	ErrInvalidRange          = errors.New("use dates like 2026-09-29, with the start on or before the end and at most 366 days apart")
	ErrReasonRequired        = errors.New("say why")
	ErrInvalidFilter         = errors.New("the filter is not valid")
	ErrClientRefInFlight     = errors.New("this stock arrival is already being saved; try again in a moment")
	ErrMissingSettings       = errors.New("company settings are missing")
)

type Supplier struct {
	Id               uuid.UUID
	CompanyId        uuid.UUID
	Name             string
	ContactPerson    *string
	Phone            *string
	Email            *string
	Tin              *string
	Vrn              *string
	Address          *string
	PaymentTermsDays int
	OpeningBalance   int64
	Notes            *string
	IsActive         bool
	CreatedBy        *uuid.UUID
	CreatedAt        time.Time
	UpdatedBy        *uuid.UUID
	UpdatedAt        time.Time
}

type Purchase struct {
	Id                    uuid.UUID
	CompanyId             uuid.UUID
	PurchaseNumber        string
	ShopId                uuid.UUID
	SupplierId            *uuid.UUID
	SupplierInvoiceNumber *string
	InvoiceDate           *string
	ReceivedAt            time.Time
	PricesIncludeVat      bool
	Subtotal              int64
	VatTotal              int64
	Total                 int64
	Note                  *string
	PurchaseOrderId       *uuid.UUID
	ClientRef             string
	CreatedBy             uuid.UUID
	CreatedAt             time.Time
}

type PricedLine struct {
	ProductId uuid.UUID
	Quantity  int
	UnitCost  int64
	VatAmount int64
	LineTotal int64
}

type CostedProduct struct {
	Id                  uuid.UUID
	CostPrice           int64
	OnHandQuantity      int64
	PreferredSupplierId *uuid.UUID
}

type Payment struct {
	Id            uuid.UUID
	CompanyId     uuid.UUID
	PaymentNumber string
	SupplierId    *uuid.UUID
	PurchaseId    *uuid.UUID
	ShopId        uuid.UUID
	Amount        int64
	Method        string
	Reference     *string
	PaidAt        time.Time
	CreatedBy     uuid.UUID
	CreatedAt     time.Time
}

type SupplierReturn struct {
	Id           uuid.UUID
	CompanyId    uuid.UUID
	ReturnNumber string
	SupplierId   uuid.UUID
	ShopId       uuid.UUID
	Total        int64
	Note         *string
	ReturnedAt   time.Time
	CreatedBy    uuid.UUID
	CreatedAt    time.Time
}

type ReturnLine struct {
	ProductId uuid.UUID
	Quantity  int
	UnitCost  int64
}

type PurchaseOrder struct {
	Id           uuid.UUID
	CompanyId    uuid.UUID
	OrderNumber  string
	SupplierId   uuid.UUID
	ShopId       uuid.UUID
	Status       string
	ExpectedDate *string
	Note         *string
	CreatedBy    uuid.UUID
	CreatedAt    time.Time
	SentAt       *time.Time
}

type OrderLine struct {
	ProductId        uuid.UUID
	QuantityOrdered  int
	ExpectedUnitCost int64
}

type LedgerEntry struct {
	Kind       string
	DocumentId *uuid.UUID
	Reference  string
	DatedAt    time.Time
	Debit      int64
	Credit     int64
	PurchaseId *uuid.UUID
}

type OpenDebit struct {
	PurchaseId *uuid.UUID
	DatedAt    time.Time
	Amount     int64
	Remaining  int64
}
