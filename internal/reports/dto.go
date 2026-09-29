package reports

import (
	"time"

	"github.com/google/uuid"
)

type Scope struct {
	CompanyId uuid.UUID
	ShopIds   []uuid.UUID
	AllShops  bool
	FirstDay  time.Time
	LastDay   time.Time
	From      time.Time
	To        time.Time
	FromDate  string
	ToDate    string
	Location  *time.Location
}

type PaymentTotalsView struct {
	Cash   int64 `json:"cash"`
	Card   int64 `json:"card"`
	Mobile int64 `json:"mobile"`
}

type SummaryView struct {
	FromDate          string            `json:"from"`
	ToDate            string            `json:"to"`
	SaleCount         int64             `json:"sale_count"`
	UnitsSold         int64             `json:"units_sold"`
	Total             int64             `json:"total"`
	TaxTotal          int64             `json:"tax_total"`
	DiscountTotal     int64             `json:"discount_total"`
	NetSales          int64             `json:"net_sales"`
	CostTotal         int64             `json:"cost_total"`
	GrossProfit       int64             `json:"gross_profit"`
	MarginBasisPoints int64             `json:"margin_basis_points"`
	AverageSale       int64             `json:"average_sale"`
	Payments          PaymentTotalsView `json:"payments"`
}

type DayView struct {
	Date        string `json:"date"`
	SaleCount   int64  `json:"sale_count"`
	Total       int64  `json:"total"`
	TaxTotal    int64  `json:"tax_total"`
	CostTotal   int64  `json:"cost_total"`
	GrossProfit int64  `json:"gross_profit"`
}

type ProductRowView struct {
	ProductId    uuid.UUID `json:"product_id"`
	Name         string    `json:"name"`
	VariantLabel string    `json:"variant_label"`
	Sku          string    `json:"sku"`
	Quantity     int64     `json:"quantity"`
	Revenue      int64     `json:"revenue"`
	NetRevenue   int64     `json:"net_revenue"`
	CostTotal    int64     `json:"cost_total"`
	GrossProfit  int64     `json:"gross_profit"`
	SaleCount    int64     `json:"sale_count"`
}

type CashierRowView struct {
	UserId      uuid.UUID `json:"user_id"`
	Name        string    `json:"name"`
	SaleCount   int64     `json:"sale_count"`
	Total       int64     `json:"total"`
	AverageSale int64     `json:"average_sale"`
}

type ShopRowView struct {
	ShopId      uuid.UUID `json:"shop_id"`
	Name        string    `json:"name"`
	SaleCount   int64     `json:"sale_count"`
	Total       int64     `json:"total"`
	TaxTotal    int64     `json:"tax_total"`
	CostTotal   int64     `json:"cost_total"`
	GrossProfit int64     `json:"gross_profit"`
}

type StockTotalsView struct {
	ProductCount int64 `json:"product_count"`
	Units        int64 `json:"units"`
	ValueAtCost  int64 `json:"value_at_cost"`
	ValueAtPrice int64 `json:"value_at_price"`
	LowCount     int64 `json:"low_count"`
	OutCount     int64 `json:"out_count"`
}

type DeadStockView struct {
	ProductId    uuid.UUID  `json:"product_id"`
	Name         string     `json:"name"`
	VariantLabel string     `json:"variant_label"`
	Sku          string     `json:"sku"`
	Quantity     int64      `json:"quantity"`
	ValueAtCost  int64      `json:"value_at_cost"`
	LastSoldAt   *time.Time `json:"last_sold_at"`
}

type InventoryView struct {
	Stock         StockTotalsView `json:"stock"`
	DeadStockDays int             `json:"dead_stock_days"`
	DeadStock     []DeadStockView `json:"dead_stock"`
}

type RecentSaleView struct {
	Id            uuid.UUID `json:"id"`
	ReceiptNumber string    `json:"receipt_number"`
	ShopName      string    `json:"shop_name"`
	CashierName   string    `json:"cashier_name"`
	Total         int64     `json:"total"`
	CreatedAt     time.Time `json:"created_at"`
}

type DashboardView struct {
	Today        SummaryView      `json:"today"`
	Yesterday    SummaryView      `json:"yesterday"`
	MonthToDate  SummaryView      `json:"month_to_date"`
	LastTwoWeeks []DayView        `json:"last_two_weeks"`
	TopProducts  []ProductRowView `json:"top_products"`
	Stock        StockTotalsView  `json:"stock"`
	RecentSales  []RecentSaleView `json:"recent_sales"`
}
