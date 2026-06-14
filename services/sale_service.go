package services

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/models"
	"github.com/chrisostomemataba/balceinv-api/repository"
	"gorm.io/gorm"
)

type resolvedItem struct {
	input   SaleItemInput
	product models.Product
}

type SaleService struct {
	repo                *repository.SaleRepository
	productRepo         *repository.ProductRepository
	settingsRepo        *repository.SettingsRepository
	notificationService *NotificationService
}

func NewSaleService(
	repo *repository.SaleRepository,
	productRepo *repository.ProductRepository,
	settingsRepo *repository.SettingsRepository,
	notificationService *NotificationService,
) *SaleService {
	return &SaleService{
		repo:                repo,
		productRepo:         productRepo,
		settingsRepo:        settingsRepo,
		notificationService: notificationService,
	}
}

type SaleItemInput struct {
	ProductID   uint     `json:"productId"`
	Quantity    int      `json:"quantity"`
	IsWholesale bool     `json:"isWholesale"`
	UnitPrice   *float64 `json:"unitPrice"`
}

type CreateSaleInput struct {
	Items       []SaleItemInput `json:"items"`
	PaymentType string          `json:"paymentType"`
	SaleType    string          `json:"saleType"`
	AmountPaid  float64         `json:"amountPaid"`
	UseEFD      bool            `json:"useEFD"`
	UserID      uint
}

type SaleResult struct {
	ID            uint        `json:"id"`
	ReceiptNumber string      `json:"receipt_number"`
	Total         float64     `json:"total"`
	TaxAmount     float64     `json:"tax_amount"`
	PaymentType   string      `json:"payment_type"`
	AmountPaid    float64     `json:"amount_paid"`
	Change        float64     `json:"change"`
	ReceiptData   interface{} `json:"receipt_data"`
}

func (s *SaleService) GetAll(filters repository.SaleFilters) ([]models.Sale, error) {
	return s.repo.FindAll(filters)
}

func (s *SaleService) GetByID(id uint) (*models.Sale, error) {
	sale, err := s.repo.FindByID(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("sale not found")
	}
	return sale, err
}

func (s *SaleService) GetByDateRange(start, end time.Time) ([]models.Sale, error) {
	return s.repo.FindByDateRange(start, end)
}

func (s *SaleService) GetDailySummary(date time.Time) (map[string]interface{}, error) {
	startOfDay := time.Date(date.Year(), date.Month(), date.Day(), 0, 0, 0, 0, date.Location())
	endOfDay := time.Date(date.Year(), date.Month(), date.Day(), 23, 59, 59, 999, date.Location())

	sales, err := s.repo.FindByDateRange(startOfDay, endOfDay)
	if err != nil {
		return nil, err
	}

	totalRevenue, totalTax := sumSaleTotals(sales)

	return map[string]interface{}{
		"sales":              sales,
		"total_revenue":      totalRevenue,
		"total_transactions": len(sales),
		"total_tax":          totalTax,
	}, nil
}

func (s *SaleService) GetMonthlySummary(year, month int) (map[string]interface{}, error) {
	start := time.Date(year, time.Month(month), 1, 0, 0, 0, 0, time.UTC)
	end := time.Date(year, time.Month(month+1), 0, 23, 59, 59, 999, time.UTC)

	sales, err := s.repo.FindByDateRange(start, end)
	if err != nil {
		return nil, err
	}

	totalRevenue, totalTax := sumSaleTotals(sales)

	avg := 0.0
	if len(sales) > 0 {
		avg = totalRevenue / float64(len(sales))
	}

	return map[string]interface{}{
		"sales":               sales,
		"total_revenue":       totalRevenue,
		"total_transactions":  len(sales),
		"total_tax":           totalTax,
		"average_transaction": avg,
	}, nil
}

// CreateSale is the entry point for the POS checkout flow.
// It validates stock, calculates totals, and commits the sale atomically.
func (s *SaleService) CreateSale(input CreateSaleInput) (*SaleResult, error) {
	settings, _ := s.settingsRepo.GetSettings()

	resolved, err := s.resolveItems(input.Items)
	if err != nil {
		return nil, err
	}

	taxRate := taxRateFromSettings(settings)
	total := calculateTotal(resolved)
	taxAmount := total * (taxRate / (100 + taxRate))

	change, err := validatePayment(input.PaymentType, input.AmountPaid, total)
	if err != nil {
		return nil, err
	}

	receiptNumber, err := s.generateReceiptNumber(settings)
	if err != nil {
		return nil, err
	}

	sale := s.buildSaleRecord(input, receiptNumber, total, taxAmount)
	saleItems, movements, stockUpdates := s.buildTransactionData(resolved, receiptNumber)

	if err := s.repo.CreateWithItems(sale, saleItems, movements, stockUpdates); err != nil {
		return nil, err
	}

	s.triggerStockCheck(resolved)

	return &SaleResult{
		ID:            sale.ID,
		ReceiptNumber: sale.ReceiptNumber,
		Total:         sale.TotalAmount,
		TaxAmount:     sale.TaxAmount,
		PaymentType:   sale.PaymentType,
		AmountPaid:    input.AmountPaid,
		Change:        change,
		ReceiptData:   s.buildReceiptData(sale, resolved, settings, change, taxRate),
	}, nil
}

// resolveItems loads each product from the database and validates stock.
func (s *SaleService) resolveItems(items []SaleItemInput) ([]resolvedItem, error) {
	resolved := make([]resolvedItem, 0, len(items))
	for _, item := range items {
		product, err := s.productRepo.FindByID(item.ProductID)
		if err != nil {
			return nil, fmt.Errorf("product with ID %d not found", item.ProductID)
		}
		if product.Quantity < item.Quantity {
			return nil, fmt.Errorf("insufficient stock for %s", product.Name)
		}
		resolved = append(resolved, resolvedItem{input: item, product: *product})
	}
	return resolved, nil
}

// buildSaleRecord constructs the Sale model without persisting it.
func (s *SaleService) buildSaleRecord(input CreateSaleInput, receiptNumber string, total, taxAmount float64) *models.Sale {
	saleType := input.SaleType
	if saleType == "" {
		saleType = "retail"
	}
	return &models.Sale{
		UserID:        input.UserID,
		ReceiptNumber: receiptNumber,
		TotalAmount:   total,
		TaxAmount:     taxAmount,
		PaymentType:   input.PaymentType,
		SaleType:      saleType,
	}
}

// buildTransactionData constructs the sale items, stock movements, and stock
// update map needed by the repository transaction. The effective unit price
// is resolved once here and used consistently for both the sale item record
// and the line total — so the receipt price always matches what the cashier charged.
func (s *SaleService) buildTransactionData(
	resolved []resolvedItem,
	receiptNumber string,
) ([]models.SaleItem, []models.StockMovement, map[uint]int) {
	saleItems := make([]models.SaleItem, 0, len(resolved))
	movements := make([]models.StockMovement, 0, len(resolved))
	stockUpdates := map[uint]int{}

	for _, r := range resolved {
		effectivePrice := resolveEffectivePrice(r)
		lineTotal := effectivePrice * float64(r.input.Quantity)
		newQuantity := r.product.Quantity - r.input.Quantity
		ref := receiptNumber

		saleItems = append(saleItems, models.SaleItem{
			ProductID:   r.product.ID,
			Quantity:    r.input.Quantity,
			UnitPrice:   effectivePrice,
			TotalPrice:  lineTotal,
			IsWholesale: r.input.IsWholesale,
		})

		stockUpdates[r.product.ID] = newQuantity

		movements = append(movements, models.StockMovement{
			ProductID:   r.product.ID,
			Change:      -r.input.Quantity,
			NewQuantity: newQuantity,
			Reason:      "sale",
			Reference:   &ref,
			UserID:      &r.input.ProductID,
		})
	}

	return saleItems, movements, stockUpdates
}

// triggerStockCheck runs the notification check after a successful sale.
// Errors are intentionally ignored — a failed stock check must never
// roll back a completed sale or show an error to the cashier.
func (s *SaleService) triggerStockCheck(resolved []resolvedItem) {
	productIDs := make([]uint, 0, len(resolved))
	for _, r := range resolved {
		productIDs = append(productIDs, r.product.ID)
	}
	_ = s.notificationService.CheckStockLevels(productIDs)
}

func (s *SaleService) generateReceiptNumber(settings *models.Settings) (string, error) {
	format := "SALE-{TIMESTAMP}-{COUNTER}"
	if settings != nil && settings.ReceiptNumberFormat != "" {
		format = settings.ReceiptNumberFormat
	}

	count, err := s.repo.CountToday()
	if err != nil {
		return "", err
	}

	counter := count + 1
	timestamp := time.Now().UnixMilli()
	date := strings.ReplaceAll(time.Now().Format("2006-01-02"), "-", "")

	receipt := format
	receipt = strings.ReplaceAll(receipt, "{TIMESTAMP}", fmt.Sprintf("%d", timestamp))
	receipt = strings.ReplaceAll(receipt, "{COUNTER}", fmt.Sprintf("%04d", counter))
	receipt = strings.ReplaceAll(receipt, "{DATE}", date)

	return receipt, nil
}

func (s *SaleService) buildReceiptData(
	sale *models.Sale,
	resolved []resolvedItem,
	settings *models.Settings,
	change float64,
	taxRate float64,
) map[string]interface{} {
	businessName := "POS System"
	currency := "TZS"
	footer := "Thank you for your business!"

	if settings != nil {
		currency = settings.Currency
		if settings.Company.Name != "" {
			businessName = settings.Company.Name
		}
		if settings.Company.ReceiptFooter != nil {
			footer = *settings.Company.ReceiptFooter
		}
	}

	items := make([]map[string]interface{}, 0, len(resolved))
	for _, r := range resolved {
		effectivePrice := resolveEffectivePrice(r)
		items = append(items, map[string]interface{}{
			"name":         r.product.Name,
			"sku":          r.product.SKU,
			"quantity":     r.input.Quantity,
			"unit_price":   effectivePrice,
			"total":        effectivePrice * float64(r.input.Quantity),
			"is_wholesale": r.input.IsWholesale,
		})
	}

	return map[string]interface{}{
		"business_name":  businessName,
		"receipt_number": sale.ReceiptNumber,
		"date":           time.Now(),
		"payment_type":   sale.PaymentType,
		"items":          items,
		"subtotal":       sale.TotalAmount - sale.TaxAmount,
		"tax_amount":     sale.TaxAmount,
		"tax_rate":       taxRate,
		"total":          sale.TotalAmount,
		"change":         change,
		"currency":       currency,
		"receipt_footer": footer,
	}
}

// ── Package-level helpers ─────────────────────────────────────────────────

// resolveEffectivePrice returns the price to use for a sale item.
// Priority: frontend-provided unit price (includes discounts and addons) →
// wholesale price → standard product price.
func resolveEffectivePrice(r resolvedItem) float64 {
	if r.input.UnitPrice != nil && *r.input.UnitPrice > 0 {
		return *r.input.UnitPrice
	}
	if r.input.IsWholesale && r.product.WholesalePrice != nil {
		return *r.product.WholesalePrice
	}
	return r.product.Price
}

// calculateTotal sums the line totals for all resolved items using
// the effective price for each — same resolution order as resolveEffectivePrice.
func calculateTotal(resolved []resolvedItem) float64 {
	total := 0.0
	for _, r := range resolved {
		total += resolveEffectivePrice(r) * float64(r.input.Quantity)
	}
	return total
}

// validatePayment checks whether the cash payment covers the total.
// Returns the change due, or an error if the amount is insufficient.
func validatePayment(paymentType string, amountPaid, total float64) (float64, error) {
	if paymentType != "cash" || amountPaid <= 0 {
		return 0, nil
	}
	change := amountPaid - total
	if change < 0 {
		return 0, fmt.Errorf("insufficient payment. required: %.2f, paid: %.2f", total, amountPaid)
	}
	return change, nil
}

// taxRateFromSettings returns the configured tax rate or the default of 18%.
func taxRateFromSettings(settings *models.Settings) float64 {
	if settings != nil {
		return settings.TaxRate
	}
	return 18.0
}

// sumSaleTotals aggregates revenue and tax across a slice of sales.
func sumSaleTotals(sales []models.Sale) (totalRevenue, totalTax float64) {
	for _, sale := range sales {
		totalRevenue += sale.TotalAmount
		totalTax += sale.TaxAmount
	}
	return
}