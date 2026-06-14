package services

import (
	"sort"
	"time"

	"github.com/chrisostomemataba/balceinv-api/repository"
)

type ReportService struct {
	repo *repository.ReportRepository
}

func NewReportService(repo *repository.ReportRepository) *ReportService {
	return &ReportService{repo: repo}
}

type SalesSummary struct {
	TotalSales         int     `json:"totalSales"`
	TotalRevenue       float64 `json:"totalRevenue"`
	TotalTax           float64 `json:"totalTax"`
	AverageTransaction float64 `json:"averageTransaction"`
	CashSales          float64 `json:"cashSales"`
	CardSales          float64 `json:"cardSales"`
	MobileSales        float64 `json:"mobileSales"`
}

type TopProduct struct {
	ProductID    uint    `json:"productId"`
	ProductName  string  `json:"productName"`
	SKU          string  `json:"sku"`
	TotalQty     int     `json:"totalQuantity"`
	TotalRevenue float64 `json:"totalRevenue"`
	SalesCount   int     `json:"salesCount"`
}

type SalesByUser struct {
	UserID       uint    `json:"userId"`
	UserName     string  `json:"userName"`
	TotalSales   int     `json:"totalSales"`
	TotalRevenue float64 `json:"totalRevenue"`
}

type LowStockItem struct {
	ID       uint   `json:"id"`
	Name     string `json:"name"`
	SKU      string `json:"sku"`
	Quantity int    `json:"quantity"`
	MinStock int    `json:"minStock"`
}

type OutOfStockItem struct {
	ID   uint   `json:"id"`
	Name string `json:"name"`
	SKU  string `json:"sku"`
}

type DeadStockItem struct {
	ID                uint   `json:"id"`
	Name              string `json:"name"`
	SKU               string `json:"sku"`
	Quantity          int    `json:"quantity"`
	DaysSinceLastSale int    `json:"daysSinceLastSale"`
}

type InventoryReport struct {
	TotalProducts  int              `json:"totalProducts"`
	TotalStockValue float64         `json:"totalStockValue"`
	LowStockCount  int              `json:"lowStockCount"`
	OutOfStockCount int             `json:"outOfStockCount"`
	DeadStockCount int              `json:"deadStockCount"`
	LowStockItems  []LowStockItem   `json:"lowStockItems"`
	OutOfStockItems []OutOfStockItem `json:"outOfStockItems"`
	DeadStockItems []DeadStockItem  `json:"deadStockItems"`
}

type FinancialReport struct {
	TotalRevenue float64 `json:"totalRevenue"`
	TotalCost    float64 `json:"totalCost"`
	GrossProfit  float64 `json:"grossProfit"`
	ProfitMargin float64 `json:"profitMargin"`
	TotalTax     float64 `json:"totalTax"`
	NetProfit    float64 `json:"netProfit"`
}

type DailyTrend struct {
	Date    string  `json:"date"`
	Sales   int     `json:"sales"`
	Revenue float64 `json:"revenue"`
}

func (s *ReportService) GetSalesSummary(dr repository.ReportDateRange) (*SalesSummary, error) {
	sales, err := s.repo.FindSalesInRange(dr)
	if err != nil {
		return nil, err
	}

	summary := &SalesSummary{TotalSales: len(sales)}

	for _, sale := range sales {
		summary.TotalRevenue += sale.TotalAmount
		summary.TotalTax += sale.TaxAmount

		switch sale.PaymentType {
		case "cash":
			summary.CashSales += sale.TotalAmount
		case "card":
			summary.CardSales += sale.TotalAmount
		case "mobile":
			summary.MobileSales += sale.TotalAmount
		}
	}

	if len(sales) > 0 {
		summary.AverageTransaction = summary.TotalRevenue / float64(len(sales))
	}

	return summary, nil
}

func (s *ReportService) GetTopProducts(dr repository.ReportDateRange, limit int) ([]TopProduct, error) {
	sales, err := s.repo.FindSalesInRange(dr)
	if err != nil {
		return nil, err
	}

	// Aggregate by product ID — same Map pattern your TypeScript used
	productMap := map[uint]*TopProduct{}

	for _, sale := range sales {
		for _, item := range sale.Items {
			existing, ok := productMap[item.ProductID]
			if ok {
				existing.TotalQty += item.Quantity
				existing.TotalRevenue += item.TotalPrice
				existing.SalesCount++
			} else {
				productMap[item.ProductID] = &TopProduct{
					ProductID:   item.ProductID,
					ProductName: item.Product.Name,
					SKU:         item.Product.SKU,
					TotalQty:    item.Quantity,
					TotalRevenue: item.TotalPrice,
					SalesCount:  1,
				}
			}
		}
	}

	result := make([]TopProduct, 0, len(productMap))
	for _, p := range productMap {
		result = append(result, *p)
	}

	// Sort by revenue descending — highest earning products first
	sort.Slice(result, func(i, j int) bool {
		return result[i].TotalRevenue > result[j].TotalRevenue
	})

	if limit > 0 && len(result) > limit {
		result = result[:limit]
	}

	return result, nil
}

func (s *ReportService) GetSalesByUser(dr repository.ReportDateRange) ([]SalesByUser, error) {
	sales, err := s.repo.FindSalesInRange(dr)
	if err != nil {
		return nil, err
	}

	userMap := map[uint]*SalesByUser{}

	for _, sale := range sales {
		existing, ok := userMap[sale.UserID]
		if ok {
			existing.TotalSales++
			existing.TotalRevenue += sale.TotalAmount
		} else {
			userMap[sale.UserID] = &SalesByUser{
				UserID:       sale.UserID,
				UserName:     sale.User.Name,
				TotalSales:   1,
				TotalRevenue: sale.TotalAmount,
			}
		}
	}

	result := make([]SalesByUser, 0, len(userMap))
	for _, u := range userMap {
		result = append(result, *u)
	}

	sort.Slice(result, func(i, j int) bool {
		return result[i].TotalRevenue > result[j].TotalRevenue
	})

	return result, nil
}

func (s *ReportService) GetInventoryReport() (*InventoryReport, error) {
	products, err := s.repo.FindAllProducts()
	if err != nil {
		return nil, err
	}

	// Dead stock = products with quantity > 0 that have not sold in 30 days
	thirtyDaysAgo := time.Now().AddDate(0, 0, -30)
	recentSales, err := s.repo.FindSalesSince(thirtyDaysAgo)
	if err != nil {
		return nil, err
	}

	soldRecently := map[uint]bool{}
	for _, sale := range recentSales {
		for _, item := range sale.Items {
			soldRecently[item.ProductID] = true
		}
	}

	report := &InventoryReport{
		TotalProducts:   len(products),
		LowStockItems:   []LowStockItem{},
		OutOfStockItems: []OutOfStockItem{},
		DeadStockItems:  []DeadStockItem{},
	}

	for _, p := range products {
		report.TotalStockValue += float64(p.Quantity) * p.CostPrice

		if p.Quantity == 0 {
			report.OutOfStockCount++
			report.OutOfStockItems = append(report.OutOfStockItems, OutOfStockItem{
				ID: p.ID, Name: p.Name, SKU: p.SKU,
			})
		} else if p.Quantity <= p.MinStock {
			report.LowStockCount++
			report.LowStockItems = append(report.LowStockItems, LowStockItem{
				ID: p.ID, Name: p.Name, SKU: p.SKU,
				Quantity: p.Quantity, MinStock: p.MinStock,
			})
		}

		if p.Quantity > 0 && !soldRecently[p.ID] {
			report.DeadStockCount++
			report.DeadStockItems = append(report.DeadStockItems, DeadStockItem{
				ID: p.ID, Name: p.Name, SKU: p.SKU,
				Quantity: p.Quantity, DaysSinceLastSale: 30,
			})
		}
	}

	return report, nil
}

func (s *ReportService) GetFinancialReport(dr repository.ReportDateRange) (*FinancialReport, error) {
	sales, err := s.repo.FindSalesInRange(dr)
	if err != nil {
		return nil, err
	}

	report := &FinancialReport{}

	for _, sale := range sales {
		report.TotalRevenue += sale.TotalAmount
		report.TotalTax += sale.TaxAmount

		for _, item := range sale.Items {
			report.TotalCost += float64(item.Quantity) * item.Product.CostPrice
		}
	}

	report.GrossProfit = report.TotalRevenue - report.TotalCost

	if report.TotalRevenue > 0 {
		report.ProfitMargin = (report.GrossProfit / report.TotalRevenue) * 100
	}

	report.NetProfit = report.GrossProfit - report.TotalTax

	return report, nil
}

func (s *ReportService) GetDailyTrend(dr repository.ReportDateRange) ([]DailyTrend, error) {
	sales, err := s.repo.FindSalesInRange(dr)
	if err != nil {
		return nil, err
	}

	// Group sales by date string — same Map pattern your TypeScript used
	dailyMap := map[string]*DailyTrend{}

	for _, sale := range sales {
		date := sale.CreatedAt.Format("2006-01-02")
		existing, ok := dailyMap[date]
		if ok {
			existing.Sales++
			existing.Revenue += sale.TotalAmount
		} else {
			dailyMap[date] = &DailyTrend{
				Date:    date,
				Sales:   1,
				Revenue: sale.TotalAmount,
			}
		}
	}

	result := make([]DailyTrend, 0, len(dailyMap))
	for _, d := range dailyMap {
		result = append(result, *d)
	}

	// Sort ascending by date so charts render left-to-right chronologically
	sort.Slice(result, func(i, j int) bool {
		return result[i].Date < result[j].Date
	})

	return result, nil
}

// Dashboard summary — used by the single /api/dashboard endpoint.
// This combines the core numbers your useDashboard composable needs.
type DashboardSummary struct {
	UserCount     int64   `json:"userCount"`
	ProductCount  int64   `json:"productCount"`
	SaleCount     int64   `json:"saleCount"`
	TotalRevenue  float64 `json:"totalRevenue"`
}