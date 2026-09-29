package accounting

import (
	"time"

	"github.com/google/uuid"
)

type StartRequest struct {
	Mode         *string `json:"mode" validate:"omitnil,oneof=today history"`
	CashInDrawer int64   `json:"cash_in_drawer" validate:"gte=0,lte=100000000000000"`
	MobileMoney  int64   `json:"mobile_money" validate:"gte=0,lte=100000000000000"`
	Bank         int64   `json:"bank" validate:"gte=0,lte=100000000000000"`
}

type MoneyRequest struct {
	ClientRef        string  `json:"client_ref" validate:"required,min=8,max=64"`
	Kind             string  `json:"kind" validate:"required,oneof=expense owner_in owner_out money_move other_income"`
	EntryDate        string  `json:"entry_date" validate:"omitempty,datetime=2006-01-02"`
	Amount           int64   `json:"amount" validate:"required,gt=0,lte=100000000000000"`
	MoneyAccount     string  `json:"money_account" validate:"required,oneof=cash mobile_money bank card_clearing"`
	ToMoneyAccount   *string `json:"to_money_account" validate:"omitnil,oneof=cash mobile_money bank card_clearing"`
	ExpenseAccountId *string `json:"expense_account_id" validate:"omitnil,uuid"`
	Fee              int64   `json:"fee" validate:"gte=0,lte=100000000000000"`
	ShopId           *string `json:"shop_id" validate:"omitnil,uuid"`
	Note             *string `json:"note" validate:"omitnil,max=300"`
	AttachmentKey    *string `json:"attachment_key" validate:"omitnil,max=200"`
	IncludesVat      bool    `json:"includes_vat"`
	VatAmount        *int64  `json:"vat_amount" validate:"omitnil,gte=0,lte=100000000000000"`
	SupplierTin      *string `json:"supplier_tin" validate:"omitnil,max=40"`
	ReceiptNumber    *string `json:"receipt_number" validate:"omitnil,max=60"`
}

type ReverseRequest struct {
	Reason string `json:"reason" validate:"required,min=3,max=300"`
}

type ManualLineRequest struct {
	AccountId string  `json:"account_id" validate:"required,uuid"`
	Debit     int64   `json:"debit" validate:"gte=0,lte=100000000000000"`
	Credit    int64   `json:"credit" validate:"gte=0,lte=100000000000000"`
	ShopId    *string `json:"shop_id" validate:"omitnil,uuid"`
}

type ManualRequest struct {
	ClientRef string              `json:"client_ref" validate:"required,min=8,max=64"`
	EntryDate string              `json:"entry_date" validate:"omitempty,datetime=2006-01-02"`
	Reason    string              `json:"reason" validate:"required,min=3,max=500"`
	Lines     []ManualLineRequest `json:"lines" validate:"required,min=2,max=50,dive"`
}

type AccountRequest struct {
	Code string `json:"code" validate:"required,min=1,max=10,numeric"`
	Name string `json:"name" validate:"required,min=1,max=80"`
	Type string `json:"type" validate:"required,oneof=asset liability equity income expense"`
}

type AccountUpdateRequest struct {
	Name     *string `json:"name" validate:"omitnil,min=1,max=80"`
	IsActive *bool   `json:"is_active"`
}

type CloseRequest struct {
	ClosedUntil string `json:"closed_until" validate:"required,datetime=2006-01-02"`
}

type StatusView struct {
	Mode               string  `json:"mode"`
	VatRegistered      bool    `json:"vat_registered"`
	VatRateBasisPoints int     `json:"vat_rate_basis_points"`
	Started            bool    `json:"started"`
	StartedOn          *string `json:"started_on"`
	StartMode          *string `json:"start_mode"`
	ClosedUntil        *string `json:"closed_until"`
	Today              string  `json:"today"`
	FirstRecordDate    *string `json:"first_record_date"`
	SuggestedStartMode string  `json:"suggested_start_mode"`
	UnpostedCount      int     `json:"unposted_count"`
}

type AccountView struct {
	Id          uuid.UUID `json:"id"`
	Code        string    `json:"code"`
	SystemKey   *string   `json:"system_key"`
	Name        *string   `json:"name"`
	Type        string    `json:"type"`
	IsSystem    bool      `json:"is_system"`
	IsActive    bool      `json:"is_active"`
	IsMoney     bool      `json:"is_money"`
	IsSpendable bool      `json:"is_spendable"`
}

type LineView struct {
	LineNo      int        `json:"line_no"`
	AccountId   uuid.UUID  `json:"account_id"`
	AccountCode string     `json:"account_code"`
	AccountKey  *string    `json:"account_key"`
	AccountName *string    `json:"account_name"`
	AccountType string     `json:"account_type"`
	Debit       int64      `json:"debit"`
	Credit      int64      `json:"credit"`
	ShopId      *uuid.UUID `json:"shop_id"`
	ShopName    *string    `json:"shop_name"`
}

type EntryView struct {
	Id                uuid.UUID  `json:"id"`
	EntryNumber       int64      `json:"entry_number"`
	Number            string     `json:"number"`
	EntryDate         string     `json:"entry_date"`
	SourceType        string     `json:"source_type"`
	SourceId          *uuid.UUID `json:"source_id"`
	Memo              *string    `json:"memo"`
	ShopId            *uuid.UUID `json:"shop_id"`
	ShopName          *string    `json:"shop_name"`
	HasAttachment     bool       `json:"has_attachment"`
	ReceiptNumber     *string    `json:"receipt_number"`
	SupplierTin       *string    `json:"supplier_tin"`
	PartyType         *string    `json:"party_type"`
	PartyId           *uuid.UUID `json:"party_id"`
	ReversesEntryId   *uuid.UUID `json:"reverses_entry_id"`
	ReversedByEntryId *uuid.UUID `json:"reversed_by_entry_id"`
	IsReversible      bool       `json:"is_reversible"`
	CreatedByName     *string    `json:"created_by_name"`
	CreatedAt         time.Time  `json:"created_at"`
	Amount            int64      `json:"amount"`
	Lines             []LineView `json:"lines"`
}

type AccountAmountView struct {
	AccountId uuid.UUID `json:"account_id"`
	Code      string    `json:"code"`
	SystemKey *string   `json:"system_key"`
	Name      *string   `json:"name"`
	Type      string    `json:"type"`
	Amount    int64     `json:"amount"`
}

type MoneyBalancesView struct {
	Cash         int64 `json:"cash"`
	MobileMoney  int64 `json:"mobile_money"`
	Bank         int64 `json:"bank"`
	CardClearing int64 `json:"card_clearing"`
}

type VatSummaryView struct {
	Charged     int64  `json:"charged"`
	Reclaimable int64  `json:"reclaimable"`
	ToPay       int64  `json:"to_pay"`
	DueDate     string `json:"due_date"`
}

type OverviewView struct {
	FromDate string            `json:"from"`
	ToDate   string            `json:"to"`
	MoneyIn  int64             `json:"money_in"`
	MoneyOut int64             `json:"money_out"`
	Income   int64             `json:"income"`
	Costs    int64             `json:"costs"`
	Profit   int64             `json:"profit"`
	Balances MoneyBalancesView `json:"balances"`
	WhatIOwn int64             `json:"what_i_own"`
	WhatIOwe int64             `json:"what_i_owe"`
	Vat      *VatSummaryView   `json:"vat"`
}

type ProfitAndLossView struct {
	FromDate      string              `json:"from"`
	ToDate        string              `json:"to"`
	Income        []AccountAmountView `json:"income"`
	TotalIncome   int64               `json:"total_income"`
	CostOfGoods   int64               `json:"cost_of_goods"`
	GrossProfit   int64               `json:"gross_profit"`
	Expenses      []AccountAmountView `json:"expenses"`
	TotalExpenses int64               `json:"total_expenses"`
	NetProfit     int64               `json:"net_profit"`
}

type BalanceSheetView struct {
	AsOf             string              `json:"as_of"`
	Assets           []AccountAmountView `json:"assets"`
	TotalAssets      int64               `json:"total_assets"`
	Liabilities      []AccountAmountView `json:"liabilities"`
	TotalLiabilities int64               `json:"total_liabilities"`
	Equity           []AccountAmountView `json:"equity"`
	ProfitToDate     int64               `json:"profit_to_date"`
	TotalEquity      int64               `json:"total_equity"`
	IsBalanced       bool                `json:"is_balanced"`
}

type TrialBalanceRowView struct {
	AccountAmountView
	TotalDebit    int64 `json:"total_debit"`
	TotalCredit   int64 `json:"total_credit"`
	DebitBalance  int64 `json:"debit_balance"`
	CreditBalance int64 `json:"credit_balance"`
}

type TrialBalanceView struct {
	AsOf               string                `json:"as_of"`
	Rows               []TrialBalanceRowView `json:"rows"`
	TotalDebitBalance  int64                 `json:"total_debit_balance"`
	TotalCreditBalance int64                 `json:"total_credit_balance"`
	IsBalanced         bool                  `json:"is_balanced"`
}

type StatementLineView struct {
	EntryId     uuid.UUID  `json:"entry_id"`
	EntryNumber int64      `json:"entry_number"`
	Number      string     `json:"number"`
	EntryDate   string     `json:"entry_date"`
	SourceType  string     `json:"source_type"`
	Memo        *string    `json:"memo"`
	Debit       int64      `json:"debit"`
	Credit      int64      `json:"credit"`
	Balance     int64      `json:"balance"`
	ShopId      *uuid.UUID `json:"shop_id"`
	ShopName    *string    `json:"shop_name"`
}

type StatementView struct {
	Account        AccountView         `json:"account"`
	FromDate       string              `json:"from"`
	ToDate         string              `json:"to"`
	OpeningBalance int64               `json:"opening_balance"`
	TotalDebit     int64               `json:"total_debit"`
	TotalCredit    int64               `json:"total_credit"`
	ClosingBalance int64               `json:"closing_balance"`
	Lines          []StatementLineView `json:"lines"`
}

type VatMonthView struct {
	Month       string `json:"month"`
	Charged     int64  `json:"charged"`
	Reclaimable int64  `json:"reclaimable"`
	ToPay       int64  `json:"to_pay"`
	DueDate     string `json:"due_date"`
}

type VatReportView struct {
	FromDate         string         `json:"from"`
	ToDate           string         `json:"to"`
	Months           []VatMonthView `json:"months"`
	TotalCharged     int64          `json:"total_charged"`
	TotalReclaimable int64          `json:"total_reclaimable"`
	TotalToPay       int64          `json:"total_to_pay"`
}

type IntegrityView struct {
	FromDate            string `json:"from"`
	ToDate              string `json:"to"`
	TotalDebit          int64  `json:"total_debit"`
	TotalCredit         int64  `json:"total_credit"`
	IsBalanced          bool   `json:"is_balanced"`
	VatRegistered       bool   `json:"vat_registered"`
	LedgerSales         int64  `json:"ledger_sales"`
	SalesReportTotal    int64  `json:"sales_report_total"`
	SalesReportTax      int64  `json:"sales_report_tax"`
	ExpectedLedgerSales int64  `json:"expected_ledger_sales"`
	SalesDifference     int64  `json:"sales_difference"`
	UnpostedCount       int    `json:"unposted_count"`
	InventoryAccount    int64  `json:"inventory_account"`
	LiveStockValue      int64  `json:"live_stock_value"`
	InventoryDifference int64  `json:"inventory_difference"`
}

type CatchUpView struct {
	Posted int `json:"posted"`
}

type ReceiptUploadView struct {
	AttachmentKey string `json:"attachment_key"`
}
