package cleardata

import "errors"

var (
	ErrOwnerOnly     = errors.New("only the owner can clear the business data")
	ErrNameMismatch  = errors.New("the business name you typed does not match")
	ErrWrongPassword = errors.New("the password is not right")
	ErrNoShopToKeep  = errors.New("there is no open shop to keep")
)

var ClearedTables = []string{
	"invoice_payments",
	"invoice_lines",
	"invoices",
	"invoice_counters",
	"supplier_receipts",
	"fiscal_refund_notes",
	"sale_refund_lines",
	"sale_refunds",
	"customer_order_payments",
	"customer_order_lines",
	"customer_orders",
	"customer_payments",
	"fiscal_credit_notes",
	"fiscal_receipts",
	"sale_item_addons",
	"sale_payments",
	"sale_items",
	"sales",
	"journal_lines",
	"journal_entries",
	"accounts",
	"accounting_settings",
	"supplier_return_lines",
	"supplier_returns",
	"supplier_payments",
	"purchase_lines",
	"purchases",
	"purchase_order_lines",
	"purchase_orders",
	"supplier_document_counters",
	"stock_transfer_items",
	"stock_transfers",
	"stock_movements",
	"notifications",
	"shop_stock",
	"customers",
	"discounts",
	"product_addons",
	"price_history",
	"barcodes",
	"products",
	"suppliers",
}

var KeptTables = []string{
	"companies",
	"company_features",
	"company_subscriptions",
	"settings",
	"roles",
	"role_permissions",
	"shops",
	"users",
	"user_permissions",
	"user_shops",
	"sessions",
	"support_messages",
	"platform_audit",
}

type ClearSummary struct {
	Products     int64
	Sales        int64
	Customers    int64
	Suppliers    int64
	ShopsRemoved int64
	StaffRemoved int64
	FilesRemoved int
	KeptShopId   string
}
