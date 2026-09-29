package settings

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/google/uuid"
)

type Repository struct{}

func NewRepository() *Repository {
	return &Repository{}
}

func (repository *Repository) InsertDefaults(ctx context.Context, querier database.Querier, companyId uuid.UUID, createdAt time.Time) error {
	query := `
		INSERT INTO settings (company_id, receipt_number_format, created_at, updated_at)
		VALUES ($1, $2, $3, $4)
	`

	_, insertError := querier.ExecContext(ctx, query, companyId, DefaultReceiptNumberFormat, createdAt, createdAt)
	if insertError != nil {
		return fmt.Errorf("failed to insert default settings: %w", insertError)
	}

	return nil
}

func (repository *Repository) FindSettings(ctx context.Context, querier database.Querier, companyId uuid.UUID) (*Settings, error) {
	query := `
		SELECT company_id, tax_rate_basis_points, date_format, receipt_number_format, receipt_language,
		       efd_enabled, efd_endpoint, efd_api_key, low_stock_threshold, email_notifications_enabled,
		       notification_email, alert_sound_enabled, alert_on_low_stock, alert_on_out_of_stock,
		       alert_on_dead_stock, dead_stock_days, print_receipt_automatically, show_tax_on_receipt,
		       show_barcodes_on_receipt, printer_enabled, printer_port, printer_model, printer_baud_rate,
		       printer_paper_width, open_cash_drawer, till_numpad_enabled, customer_display_enabled,
		       updated_by, created_at, updated_at
		FROM settings
		WHERE company_id = $1
	`

	foundSettings := Settings{}
	scanError := querier.QueryRowContext(ctx, query, companyId).Scan(
		&foundSettings.CompanyId,
		&foundSettings.TaxRateBasisPoints,
		&foundSettings.DateFormat,
		&foundSettings.ReceiptNumberFormat,
		&foundSettings.ReceiptLanguage,
		&foundSettings.EfdEnabled,
		&foundSettings.EfdEndpoint,
		&foundSettings.EfdApiKey,
		&foundSettings.LowStockThreshold,
		&foundSettings.EmailNotificationsEnabled,
		&foundSettings.NotificationEmail,
		&foundSettings.AlertSoundEnabled,
		&foundSettings.AlertOnLowStock,
		&foundSettings.AlertOnOutOfStock,
		&foundSettings.AlertOnDeadStock,
		&foundSettings.DeadStockDays,
		&foundSettings.PrintReceiptAutomatically,
		&foundSettings.ShowTaxOnReceipt,
		&foundSettings.ShowBarcodesOnReceipt,
		&foundSettings.PrinterEnabled,
		&foundSettings.PrinterPort,
		&foundSettings.PrinterModel,
		&foundSettings.PrinterBaudRate,
		&foundSettings.PrinterPaperWidth,
		&foundSettings.OpenCashDrawer,
		&foundSettings.TillNumpadEnabled,
		&foundSettings.CustomerDisplayEnabled,
		&foundSettings.UpdatedBy,
		&foundSettings.CreatedAt,
		&foundSettings.UpdatedAt,
	)
	if scanError != nil {
		if errors.Is(scanError, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to find settings: %w", scanError)
	}

	return &foundSettings, nil
}

func (repository *Repository) SaveSettings(ctx context.Context, querier database.Querier, changedSettings Settings) error {
	query := `
		UPDATE settings
		SET tax_rate_basis_points = $2, date_format = $3, receipt_number_format = $4, receipt_language = $5,
		    efd_enabled = $6, efd_endpoint = $7, efd_api_key = $8, low_stock_threshold = $9,
		    email_notifications_enabled = $10, notification_email = $11, alert_sound_enabled = $12,
		    alert_on_low_stock = $13, alert_on_out_of_stock = $14, alert_on_dead_stock = $15,
		    dead_stock_days = $16, print_receipt_automatically = $17, show_tax_on_receipt = $18,
		    show_barcodes_on_receipt = $19, printer_enabled = $20, printer_port = $21, printer_model = $22,
		    printer_baud_rate = $23, printer_paper_width = $24, open_cash_drawer = $25, till_numpad_enabled = $26,
		    customer_display_enabled = $27, updated_by = $28, updated_at = $29
		WHERE company_id = $1
	`

	_, updateError := querier.ExecContext(ctx, query,
		changedSettings.CompanyId,
		changedSettings.TaxRateBasisPoints,
		changedSettings.DateFormat,
		changedSettings.ReceiptNumberFormat,
		changedSettings.ReceiptLanguage,
		changedSettings.EfdEnabled,
		changedSettings.EfdEndpoint,
		changedSettings.EfdApiKey,
		changedSettings.LowStockThreshold,
		changedSettings.EmailNotificationsEnabled,
		changedSettings.NotificationEmail,
		changedSettings.AlertSoundEnabled,
		changedSettings.AlertOnLowStock,
		changedSettings.AlertOnOutOfStock,
		changedSettings.AlertOnDeadStock,
		changedSettings.DeadStockDays,
		changedSettings.PrintReceiptAutomatically,
		changedSettings.ShowTaxOnReceipt,
		changedSettings.ShowBarcodesOnReceipt,
		changedSettings.PrinterEnabled,
		changedSettings.PrinterPort,
		changedSettings.PrinterModel,
		changedSettings.PrinterBaudRate,
		changedSettings.PrinterPaperWidth,
		changedSettings.OpenCashDrawer,
		changedSettings.TillNumpadEnabled,
		changedSettings.CustomerDisplayEnabled,
		changedSettings.UpdatedBy,
		time.Now().UTC(),
	)
	if updateError != nil {
		return fmt.Errorf("failed to save settings: %w", updateError)
	}

	return nil
}

func (repository *Repository) FindCompanyProfile(ctx context.Context, querier database.Querier, companyId uuid.UUID) (*CompanyProfile, error) {
	query := `
		SELECT id, name, business_type, phone, address, tin, logo_key, primary_color, currency_code,
		       currency_decimals, timezone, default_locale, receipt_header, receipt_footer, updated_at
		FROM companies
		WHERE id = $1
	`

	foundProfile := CompanyProfile{}
	scanError := querier.QueryRowContext(ctx, query, companyId).Scan(
		&foundProfile.Id,
		&foundProfile.Name,
		&foundProfile.BusinessType,
		&foundProfile.Phone,
		&foundProfile.Address,
		&foundProfile.Tin,
		&foundProfile.LogoKey,
		&foundProfile.PrimaryColor,
		&foundProfile.CurrencyCode,
		&foundProfile.CurrencyDecimals,
		&foundProfile.Timezone,
		&foundProfile.DefaultLocale,
		&foundProfile.ReceiptHeader,
		&foundProfile.ReceiptFooter,
		&foundProfile.UpdatedAt,
	)
	if scanError != nil {
		if errors.Is(scanError, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to find company profile: %w", scanError)
	}

	return &foundProfile, nil
}

func (repository *Repository) SaveCompanyProfile(ctx context.Context, querier database.Querier, changedProfile CompanyProfile) error {
	query := `
		UPDATE companies
		SET name = $2, business_type = $3, phone = $4, address = $5, tin = $6, primary_color = $7,
		    currency_code = $8, currency_decimals = $9, timezone = $10, default_locale = $11,
		    receipt_header = $12, receipt_footer = $13, updated_at = $14
		WHERE id = $1
	`

	_, updateError := querier.ExecContext(ctx, query,
		changedProfile.Id,
		changedProfile.Name,
		changedProfile.BusinessType,
		changedProfile.Phone,
		changedProfile.Address,
		changedProfile.Tin,
		changedProfile.PrimaryColor,
		changedProfile.CurrencyCode,
		changedProfile.CurrencyDecimals,
		changedProfile.Timezone,
		changedProfile.DefaultLocale,
		changedProfile.ReceiptHeader,
		changedProfile.ReceiptFooter,
		time.Now().UTC(),
	)
	if updateError != nil {
		return fmt.Errorf("failed to save company profile: %w", updateError)
	}

	return nil
}

func (repository *Repository) UpdateLogoKey(ctx context.Context, querier database.Querier, companyId uuid.UUID, logoKey string) error {
	query := `UPDATE companies SET logo_key = $2, updated_at = $3 WHERE id = $1`

	_, updateError := querier.ExecContext(ctx, query, companyId, logoKey, time.Now().UTC())
	if updateError != nil {
		return fmt.Errorf("failed to update logo: %w", updateError)
	}

	return nil
}

func (repository *Repository) HasSales(ctx context.Context, querier database.Querier, companyId uuid.UUID) (bool, error) {
	query := `SELECT COUNT(*) FROM (SELECT 1 FROM sales WHERE company_id = $1 LIMIT 1) first_sale`

	saleCount := 0
	scanError := querier.QueryRowContext(ctx, query, companyId).Scan(&saleCount)
	if scanError != nil {
		return false, fmt.Errorf("failed to check for sales: %w", scanError)
	}

	return saleCount > 0, nil
}
