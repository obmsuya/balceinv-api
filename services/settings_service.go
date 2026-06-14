package services

import (
	"encoding/base64"
	"errors"
	"fmt"
	"io"
	"mime/multipart"

	"github.com/chrisostomemataba/balceinv-api/models"
	"github.com/chrisostomemataba/balceinv-api/repository"
)

type SettingsService struct {
	repo *repository.SettingsRepository
}

func NewSettingsService(repo *repository.SettingsRepository) *SettingsService {
	return &SettingsService{repo: repo}
}

type UpdateSettingsInput struct {
	// → companies table
	BusinessName    *string `json:"business_name"`
	BusinessAddress *string `json:"business_address"`
	BusinessPhone   *string `json:"business_phone"`
	BusinessTIN     *string `json:"business_tin"`
	ReceiptHeader   *string `json:"receipt_header"`
	ReceiptFooter   *string `json:"receipt_footer"`
	PrimaryColor    *string `json:"primary_color"`

	// → settings table: currency & tax
	TaxRate        *float64 `json:"tax_rate"`
	Currency       *string  `json:"currency"`
	CurrencySymbol *string  `json:"currency_symbol"`

	// → settings table: format
	DateFormat          *string `json:"date_format"`
	ReceiptNumberFormat *string `json:"receipt_number_format"`

	// → settings table: EFD
	EFDEnabled  *bool   `json:"efd_enabled"`
	EFDEndpoint *string `json:"efd_endpoint"`
	EFDApiKey   *string `json:"efd_api_key"`

	// → settings table: notifications
	LowStockThreshold         *int    `json:"low_stock_threshold"`
	EmailNotificationsEnabled *bool   `json:"email_notifications_enabled"`
	NotificationEmail         *string `json:"notification_email"`
	AlertSoundEnabled         *bool   `json:"alert_sound_enabled"`
	AlertOnLowStock           *bool   `json:"alert_on_low_stock"`
	AlertOnOutOfStock         *bool   `json:"alert_on_out_of_stock"`
	AlertOnDeadStock          *bool   `json:"alert_on_dead_stock"`
	DeadStockDays             *int    `json:"dead_stock_days"`

	// → settings table: receipt / hardware
	PrintReceiptAutomatically *bool `json:"print_receipt_automatically"`
	ShowTaxOnReceipt          *bool `json:"show_tax_on_receipt"`
	ShowBarcodesOnReceipt     *bool `json:"show_barcodes_on_receipt"`

	// → settings table: printer hardware
	// PrinterEnabled turns printing on/off without clearing the port config.
	// PrinterPort is the OS device path e.g. "COM3" or "/dev/usb/lp0".
	// PrinterBaudRate matters only for serial RS-232 printers (typically 9600 or 19200).
	// PrinterPaperWidth is 58 or 80 (mm).
	// OpenCashDrawer sends the ESC/POS DK pulse to open the drawer after printing.
	PrinterEnabled    *bool   `json:"printer_enabled"`
	PrinterPort       *string `json:"printer_port"`
	PrinterBaudRate   *int    `json:"printer_baud_rate"`
	PrinterPaperWidth *int    `json:"printer_paper_width"`
	OpenCashDrawer    *bool   `json:"open_cash_drawer"`
}

func (s *SettingsService) GetOrCreate() (*models.Settings, error) {
	settings, err := s.repo.GetSettings()
	if err == nil {
		return settings, nil
	}
	return s.repo.CreateDefaults(1)
}

func (s *SettingsService) Update(input UpdateSettingsInput, updatedBy uint) (*models.Settings, error) {
	settings, err := s.GetOrCreate()
	if err != nil {
		return nil, err
	}

	// ── Company table updates ─────────────────────────────────────────────
	companyUpdates := map[string]interface{}{}
	if input.BusinessName != nil {
		companyUpdates["name"] = *input.BusinessName
	}
	if input.BusinessAddress != nil {
		companyUpdates["address"] = *input.BusinessAddress
	}
	if input.BusinessPhone != nil {
		companyUpdates["phone"] = *input.BusinessPhone
	}
	if input.BusinessTIN != nil {
		companyUpdates["tin"] = *input.BusinessTIN
	}
	if input.ReceiptHeader != nil {
		companyUpdates["receipt_header"] = *input.ReceiptHeader
	}
	if input.ReceiptFooter != nil {
		companyUpdates["receipt_footer"] = *input.ReceiptFooter
	}
	if input.PrimaryColor != nil {
		companyUpdates["primary_color"] = *input.PrimaryColor
	}
	if len(companyUpdates) > 0 {
		if err := s.repo.UpdateCompany(settings.CompanyID, companyUpdates); err != nil {
			return nil, err
		}
	}

	// ── Settings table updates ────────────────────────────────────────────
	settingsUpdates := map[string]interface{}{"updated_by": updatedBy}

	if input.TaxRate != nil {
		settingsUpdates["tax_rate"] = *input.TaxRate
	}
	if input.Currency != nil {
		settingsUpdates["currency"] = *input.Currency
	}
	if input.CurrencySymbol != nil {
		settingsUpdates["currency_symbol"] = *input.CurrencySymbol
	}
	if input.DateFormat != nil {
		settingsUpdates["date_format"] = *input.DateFormat
	}
	if input.ReceiptNumberFormat != nil {
		settingsUpdates["receipt_number_format"] = *input.ReceiptNumberFormat
	}
	if input.EFDEnabled != nil {
		settingsUpdates["efd_enabled"] = *input.EFDEnabled
	}
	if input.EFDEndpoint != nil {
		settingsUpdates["efd_endpoint"] = *input.EFDEndpoint
	}
	if input.EFDApiKey != nil {
		settingsUpdates["efd_api_key"] = *input.EFDApiKey
	}
	if input.LowStockThreshold != nil {
		settingsUpdates["low_stock_threshold"] = *input.LowStockThreshold
	}
	if input.EmailNotificationsEnabled != nil {
		settingsUpdates["email_notifications_enabled"] = *input.EmailNotificationsEnabled
	}
	if input.NotificationEmail != nil {
		settingsUpdates["notification_email"] = *input.NotificationEmail
	}
	if input.AlertSoundEnabled != nil {
		settingsUpdates["alert_sound_enabled"] = *input.AlertSoundEnabled
	}
	if input.AlertOnLowStock != nil {
		settingsUpdates["alert_on_low_stock"] = *input.AlertOnLowStock
	}
	if input.AlertOnOutOfStock != nil {
		settingsUpdates["alert_on_out_of_stock"] = *input.AlertOnOutOfStock
	}
	if input.AlertOnDeadStock != nil {
		settingsUpdates["alert_on_dead_stock"] = *input.AlertOnDeadStock
	}
	if input.DeadStockDays != nil {
		settingsUpdates["dead_stock_days"] = *input.DeadStockDays
	}
	if input.PrintReceiptAutomatically != nil {
		settingsUpdates["print_receipt_automatically"] = *input.PrintReceiptAutomatically
	}
	if input.ShowTaxOnReceipt != nil {
		settingsUpdates["show_tax_on_receipt"] = *input.ShowTaxOnReceipt
	}
	if input.ShowBarcodesOnReceipt != nil {
		settingsUpdates["show_barcodes_on_receipt"] = *input.ShowBarcodesOnReceipt
	}

	// Printer hardware fields
	if input.PrinterEnabled != nil {
		settingsUpdates["printer_enabled"] = *input.PrinterEnabled
	}
	if input.PrinterPort != nil {
		settingsUpdates["printer_port"] = *input.PrinterPort
	}
	if input.PrinterBaudRate != nil {
		settingsUpdates["printer_baud_rate"] = *input.PrinterBaudRate
	}
	if input.PrinterPaperWidth != nil {
		settingsUpdates["printer_paper_width"] = *input.PrinterPaperWidth
	}
	if input.OpenCashDrawer != nil {
		settingsUpdates["open_cash_drawer"] = *input.OpenCashDrawer
	}

	return s.repo.UpdateSettings(settings.ID, settingsUpdates)
}

func (s *SettingsService) TestEFD(endpoint, apiKey string) (map[string]interface{}, error) {
	settings, err := s.GetOrCreate()
	if err != nil {
		return nil, err
	}

	result := map[string]interface{}{
		"endpoint": endpoint,
		"status":   "unreachable",
		"message":  "EFD endpoint did not respond",
	}

	s.repo.UpdateSettings(settings.ID, map[string]interface{}{
		"efd_test_status": "failed",
	})

	return result, nil
}

func (s *SettingsService) UploadLogo(fileHeader *multipart.FileHeader) (string, error) {
	settings, err := s.GetOrCreate()
	if err != nil {
		return "", err
	}

	file, err := fileHeader.Open()
	if err != nil {
		return "", errors.New("could not open uploaded file")
	}
	defer file.Close()

	data, err := io.ReadAll(file)
	if err != nil {
		return "", errors.New("could not read file")
	}

	mimeType := fileHeader.Header.Get("Content-Type")
	if mimeType == "" {
		mimeType = "image/png"
	}

	logoURL := fmt.Sprintf("data:%s;base64,%s", mimeType, base64.StdEncoding.EncodeToString(data))

	if err := s.repo.UpdateCompany(settings.CompanyID, map[string]interface{}{"logo": logoURL}); err != nil {
		return "", err
	}

	return logoURL, nil
}