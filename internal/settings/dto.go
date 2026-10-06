package settings

import (
	"time"

	"github.com/google/uuid"
)

type CompanyView struct {
	Id               uuid.UUID `json:"id"`
	Name             string    `json:"name"`
	BusinessType     string    `json:"business_type"`
	Phone            *string   `json:"phone"`
	Address          *string   `json:"address"`
	Tin              *string   `json:"tin"`
	LogoUrl          *string   `json:"logo_url"`
	PrimaryColor     string    `json:"primary_color"`
	CurrencyCode     string    `json:"currency_code"`
	CurrencyDecimals int       `json:"currency_decimals"`
	Timezone         string    `json:"timezone"`
	DefaultLocale    string    `json:"default_locale"`
	ReceiptHeader    *string   `json:"receipt_header"`
	ReceiptFooter    *string   `json:"receipt_footer"`
}

type SettingsView struct {
	Company                      CompanyView `json:"company"`
	TaxRate                      float64     `json:"tax_rate"`
	DateFormat                   string      `json:"date_format"`
	ReceiptNumberFormat          string      `json:"receipt_number_format"`
	ReceiptLanguage              string      `json:"receipt_language"`
	EfdEnabled                   bool        `json:"efd_enabled"`
	EfdEndpoint                  *string     `json:"efd_endpoint"`
	EfdApiKeySet                 bool        `json:"efd_api_key_set"`
	LowStockThreshold            int         `json:"low_stock_threshold"`
	EmailNotificationsEnabled    bool        `json:"email_notifications_enabled"`
	NotificationEmail            *string     `json:"notification_email"`
	AlertSoundEnabled            bool        `json:"alert_sound_enabled"`
	AlertOnLowStock              bool        `json:"alert_on_low_stock"`
	AlertOnOutOfStock            bool        `json:"alert_on_out_of_stock"`
	AlertOnDeadStock             bool        `json:"alert_on_dead_stock"`
	DeadStockDays                int         `json:"dead_stock_days"`
	PrintReceiptAutomatically    bool        `json:"print_receipt_automatically"`
	ShowTaxOnReceipt             bool        `json:"show_tax_on_receipt"`
	ShowBarcodesOnReceipt        bool        `json:"show_barcodes_on_receipt"`
	PrinterEnabled               bool        `json:"printer_enabled"`
	PrinterPort                  string      `json:"printer_port"`
	PrinterModel                 string      `json:"printer_model"`
	PrinterBaudRate              int         `json:"printer_baud_rate"`
	PrinterPaperWidth            int         `json:"printer_paper_width"`
	OpenCashDrawer               bool        `json:"open_cash_drawer"`
	TillNumpadEnabled            bool        `json:"till_numpad_enabled"`
	CustomerDisplayEnabled       bool        `json:"customer_display_enabled"`
	TillDiscountLimitBasisPoints int         `json:"till_discount_limit_basis_points"`
	UpdatedAt                    time.Time   `json:"updated_at"`
}

type UpdateSettingsRequest struct {
	BusinessName                 *string  `json:"business_name" validate:"omitnil,min=1,max=120"`
	BusinessType                 *string  `json:"business_type" validate:"omitnil,min=1,max=40"`
	BusinessPhone                *string  `json:"business_phone" validate:"omitnil,max=40"`
	BusinessAddress              *string  `json:"business_address" validate:"omitnil,max=200"`
	BusinessTin                  *string  `json:"business_tin" validate:"omitnil,max=40"`
	ReceiptHeader                *string  `json:"receipt_header" validate:"omitnil,max=300"`
	ReceiptFooter                *string  `json:"receipt_footer" validate:"omitnil,max=300"`
	PrimaryColor                 *string  `json:"primary_color" validate:"omitnil,len=7,hexcolor"`
	CurrencyCode                 *string  `json:"currency_code" validate:"omitnil,len=3,uppercase,alpha"`
	CurrencyDecimals             *int     `json:"currency_decimals" validate:"omitnil,oneof=0 2"`
	Timezone                     *string  `json:"timezone" validate:"omitnil,timezone"`
	DefaultLocale                *string  `json:"default_locale" validate:"omitnil,oneof=en sw"`
	TaxRate                      *float64 `json:"tax_rate" validate:"omitnil,gte=0,lte=100"`
	DateFormat                   *string  `json:"date_format" validate:"omitnil,oneof=DD/MM/YYYY MM/DD/YYYY YYYY-MM-DD"`
	ReceiptNumberFormat          *string  `json:"receipt_number_format" validate:"omitnil,min=1,max=60"`
	ReceiptLanguage              *string  `json:"receipt_language" validate:"omitnil,oneof=en sw"`
	EfdEnabled                   *bool    `json:"efd_enabled"`
	EfdEndpoint                  *string  `json:"efd_endpoint" validate:"omitnil,max=300"`
	EfdApiKey                    *string  `json:"efd_api_key" validate:"omitnil,max=500"`
	LowStockThreshold            *int     `json:"low_stock_threshold" validate:"omitnil,gte=0,lte=100000"`
	EmailNotificationsEnabled    *bool    `json:"email_notifications_enabled"`
	NotificationEmail            *string  `json:"notification_email" validate:"omitnil,max=254"`
	AlertSoundEnabled            *bool    `json:"alert_sound_enabled"`
	AlertOnLowStock              *bool    `json:"alert_on_low_stock"`
	AlertOnOutOfStock            *bool    `json:"alert_on_out_of_stock"`
	AlertOnDeadStock             *bool    `json:"alert_on_dead_stock"`
	DeadStockDays                *int     `json:"dead_stock_days" validate:"omitnil,gte=1,lte=3650"`
	PrintReceiptAutomatically    *bool    `json:"print_receipt_automatically"`
	ShowTaxOnReceipt             *bool    `json:"show_tax_on_receipt"`
	ShowBarcodesOnReceipt        *bool    `json:"show_barcodes_on_receipt"`
	PrinterEnabled               *bool    `json:"printer_enabled"`
	PrinterPort                  *string  `json:"printer_port" validate:"omitnil,max=200"`
	PrinterModel                 *string  `json:"printer_model" validate:"omitnil,max=100"`
	PrinterBaudRate              *int     `json:"printer_baud_rate" validate:"omitnil,oneof=9600 19200 38400 57600 115200"`
	PrinterPaperWidth            *int     `json:"printer_paper_width" validate:"omitnil,oneof=58 80"`
	OpenCashDrawer               *bool    `json:"open_cash_drawer"`
	TillNumpadEnabled            *bool    `json:"till_numpad_enabled"`
	CustomerDisplayEnabled       *bool    `json:"customer_display_enabled"`
	TillDiscountLimitBasisPoints *int     `json:"till_discount_limit_basis_points" validate:"omitnil,gte=0,lte=10000"`
}
