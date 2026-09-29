package accounting

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

const (
	ModeOff    = "off"
	ModeSimple = "simple"
	ModeFull   = "full"

	StartToday   = "today"
	StartHistory = "history"

	dateLayout       = "2006-01-02"
	openingPartyMemo = "Balance from before the books"
)

const (
	TypeAsset     = "asset"
	TypeLiability = "liability"
	TypeEquity    = "equity"
	TypeIncome    = "income"
	TypeExpense   = "expense"
)

const (
	KeyCash             = "cash"
	KeyMobileMoney      = "mobile_money"
	KeyBank             = "bank"
	KeyCardClearing     = "card_clearing"
	KeyReceivable       = "receivable"
	KeyInventory        = "inventory"
	KeyVatInput         = "vat_input"
	KeyPayable          = "payable"
	KeyVatOutput        = "vat_output"
	KeyCustomerDeposits = "customer_deposits"
	KeyOwnerCapital     = "owner_capital"
	KeyOwnerDrawings    = "owner_drawings"
	KeyRetainedEarnings = "retained_earnings"
	KeySales            = "sales"
	KeyOtherIncome      = "other_income"
	KeyStockGains       = "stock_gains"
	KeyCogs             = "cogs"
	KeyStockLosses      = "stock_losses"
	KeyRent             = "rent"
	KeySalaries         = "salaries"
	KeyUtilities        = "utilities"
	KeyTransport        = "transport"
	KeyPhoneInternet    = "phone_internet"
	KeyMoneyCharges     = "money_charges"
	KeyRepairs          = "repairs"
	KeyLicencesLevies   = "licences_levies"
	KeySupplies         = "supplies"
	KeyMarketing        = "marketing"
	KeyOtherExpenses    = "other_expenses"
)

const (
	SourceSale                = "sale"
	SourceSaleVoid            = "sale_void"
	SourceStockAdjustment     = "stock_adjustment"
	SourceStockTransfer       = "stock_transfer"
	SourceExpense             = "expense"
	SourceOwnerIn             = "owner_in"
	SourceOwnerOut            = "owner_out"
	SourceMoneyMove           = "money_move"
	SourceOtherIncome         = "other_income"
	SourceOpening             = "opening"
	SourceManual              = "manual"
	SourceReversal            = "reversal"
	SourcePurchase            = "purchase"
	SourcePurchaseCancel      = "purchase_cancel"
	SourceSupplierPayment     = "supplier_payment"
	SourceSupplierPaymentVoid = "supplier_payment_void"
	SourceSupplierReturn      = "supplier_return"
	SourceCustomerPayment     = "customer_payment"
	SourceCustomerPaymentVoid = "customer_payment_void"
	SourceOrderDeposit        = "order_deposit"
	SourceOrderRefund         = "order_refund"
)

const (
	PartyCustomer = "customer"
	PartySupplier = "supplier"
)

type systemAccount struct {
	Key  string
	Code string
	Type string
}

var systemChart = []systemAccount{
	{KeyCash, "1000", TypeAsset},
	{KeyMobileMoney, "1010", TypeAsset},
	{KeyBank, "1020", TypeAsset},
	{KeyCardClearing, "1030", TypeAsset},
	{KeyReceivable, "1100", TypeAsset},
	{KeyInventory, "1200", TypeAsset},
	{KeyVatInput, "1300", TypeAsset},
	{KeyPayable, "2000", TypeLiability},
	{KeyVatOutput, "2100", TypeLiability},
	{KeyCustomerDeposits, "2200", TypeLiability},
	{KeyOwnerCapital, "3000", TypeEquity},
	{KeyOwnerDrawings, "3100", TypeEquity},
	{KeyRetainedEarnings, "3200", TypeEquity},
	{KeySales, "4000", TypeIncome},
	{KeyOtherIncome, "4100", TypeIncome},
	{KeyStockGains, "4200", TypeIncome},
	{KeyCogs, "5000", TypeExpense},
	{KeyStockLosses, "5100", TypeExpense},
	{KeyRent, "6000", TypeExpense},
	{KeySalaries, "6010", TypeExpense},
	{KeyUtilities, "6020", TypeExpense},
	{KeyTransport, "6030", TypeExpense},
	{KeyPhoneInternet, "6040", TypeExpense},
	{KeyMoneyCharges, "6050", TypeExpense},
	{KeyRepairs, "6060", TypeExpense},
	{KeyLicencesLevies, "6070", TypeExpense},
	{KeySupplies, "6080", TypeExpense},
	{KeyMarketing, "6090", TypeExpense},
	{KeyOtherExpenses, "6990", TypeExpense},
}

var moneyKeys = []string{KeyCash, KeyMobileMoney, KeyBank, KeyCardClearing}

var accountKeyByPaymentMethod = map[string]string{
	"cash":          KeyCash,
	"mobile":        KeyMobileMoney,
	"mobile_money":  KeyMobileMoney,
	"bank":          KeyBank,
	"card":          KeyCardClearing,
	"card_clearing": KeyCardClearing,
	"credit":        KeyReceivable,
	"deposit":       KeyCustomerDeposits,
}

var paymentMethodOrder = []string{"cash", "mobile", "mobile_money", "card", "card_clearing", "bank", "deposit", "credit"}

var automaticExpenseKeys = map[string]bool{KeyCogs: true, KeyStockLosses: true}

var reversibleSources = map[string]bool{
	SourceExpense:     true,
	SourceOwnerIn:     true,
	SourceOwnerOut:    true,
	SourceMoneyMove:   true,
	SourceOtherIncome: true,
	SourceManual:      true,
}

var (
	ErrFeatureOff        = errors.New("this feature is turned off in Settings → Features")
	ErrNotStarted        = errors.New("start the books first")
	ErrAlreadyStarted    = errors.New("the books have already been started")
	ErrBeforeStart       = errors.New("the date is before the books started")
	ErrPeriodClosed      = errors.New("that month is closed; choose a date after the closed period")
	ErrFutureDate        = errors.New("the date cannot be in the future")
	ErrInvalidDate       = errors.New("use dates like 2026-09-29")
	ErrUnbalanced        = errors.New("the money in and money out of the entry must be equal")
	ErrTooFewLines       = errors.New("an entry needs at least two lines")
	ErrInvalidLine       = errors.New("each line needs either a debit or a credit above zero, not both")
	ErrAccountNotUsable  = errors.New("one of the accounts is not active or does not exist")
	ErrUnknownMethod     = errors.New("unknown payment method")
	ErrEntryNotFound     = errors.New("entry not found")
	ErrNotReversible     = errors.New("only entries made on the Money page can be reversed here; automatic entries follow their sale or stock record")
	ErrAlreadyReversed   = errors.New("this entry has already been reversed")
	ErrSameMoneyAccount  = errors.New("choose two different places to move the money between")
	ErrExpenseAccount    = errors.New("choose what the money was spent on")
	ErrVatTooLarge       = errors.New("the VAT cannot be more than the amount")
	ErrShopNotFound      = errors.New("shop not found")
	ErrAccountNotFound   = errors.New("account not found")
	ErrCodeTaken         = errors.New("another account already uses that code")
	ErrSystemAccount     = errors.New("built-in accounts cannot be renamed or turned off")
	ErrCloseBackwards    = errors.New("the closed period can only move forward")
	ErrCloseTooRecent    = errors.New("you can close up to yesterday at the latest")
	ErrClientRefReused   = errors.New("this reference was already used for a different entry")
	ErrInvalidAttachment = errors.New("the receipt photo was not found; upload it again")
	ErrInvalidFilter     = errors.New("the filter is not valid")
	ErrRangeTooLong      = errors.New("choose a range of at most 366 days")
)

type Books struct {
	CompanyId     uuid.UUID
	Mode          string
	VatRegistered bool
	IsStarted     bool
	StartedOn     string
	StartedAt     time.Time
	StartMode     string
	ClosedUntil   *string
	Location      *time.Location
}

func (books Books) IsPosting() bool {
	return books.Mode != ModeOff && books.IsStarted
}

func (books Books) LocalDate(moment time.Time) string {
	return moment.In(books.Location).Format(dateLayout)
}

func (books Books) Today() string {
	return books.LocalDate(time.Now())
}

type Line struct {
	AccountId uuid.UUID
	Debit     int64
	Credit    int64
	ShopId    *uuid.UUID
}

type Entry struct {
	EntryDate       string
	SourceType      string
	SourceId        *uuid.UUID
	ClientRef       *string
	Memo            *string
	ShopId          *uuid.UUID
	AttachmentKey   *string
	ReceiptNumber   *string
	SupplierTin     *string
	PartyType       *string
	PartyId         *uuid.UUID
	ReversesEntryId *uuid.UUID
	CreatedBy       *uuid.UUID
	Lines           []Line
}

type PostedEntry struct {
	Id          uuid.UUID
	EntryNumber int64
}

type keyedLine struct {
	Key    string
	Debit  int64
	Credit int64
	ShopId *uuid.UUID
}

type Account struct {
	Id        uuid.UUID
	Code      string
	SystemKey *string
	Name      *string
	Type      string
	IsSystem  bool
	IsActive  bool
}
