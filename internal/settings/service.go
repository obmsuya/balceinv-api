package settings

import (
	"context"
	"errors"
	"math"
	"net/mail"
	"net/url"
	"strings"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/identity"
	"github.com/chrisostomemataba/balceinv-api/internal/common/storage"
	"github.com/chrisostomemataba/balceinv-api/internal/media"
	"github.com/google/uuid"
)

const MaximumLogoBytes = 1 << 20

var (
	ErrSettingsNotFound   = errors.New("settings not found")
	ErrInvalidEfdEndpoint = errors.New("EFD endpoint must be an https:// address")
	ErrInvalidEmail       = errors.New("notification email is not a valid address")
	ErrCurrencyLocked     = errors.New("the currency can't change after the first sale; totals already recorded would stop making sense")
	ErrReceiptFormat      = errors.New("the receipt number format must contain {COUNTER} so every receipt number is different")
)

type Service struct {
	repository  *Repository
	objectStore storage.Store
}

func NewService(repository *Repository, objectStore storage.Store) *Service {
	return &Service{
		repository:  repository,
		objectStore: objectStore,
	}
}

func (service *Service) Get(ctx context.Context, querier database.Querier, companyId uuid.UUID) (SettingsView, error) {
	companySettings, findSettingsError := service.repository.FindSettings(ctx, querier, companyId)
	if findSettingsError != nil {
		return SettingsView{}, findSettingsError
	}
	companyProfile, findProfileError := service.repository.FindCompanyProfile(ctx, querier, companyId)
	if findProfileError != nil {
		return SettingsView{}, findProfileError
	}

	isMissing := companySettings == nil || companyProfile == nil
	if isMissing {
		return SettingsView{}, ErrSettingsNotFound
	}

	return toSettingsView(*companySettings, *companyProfile), nil
}

func (service *Service) Update(ctx context.Context, querier database.Querier, principal *identity.Principal, request UpdateSettingsRequest) (SettingsView, error) {
	companySettings, findSettingsError := service.repository.FindSettings(ctx, querier, principal.CompanyId)
	if findSettingsError != nil {
		return SettingsView{}, findSettingsError
	}
	companyProfile, findProfileError := service.repository.FindCompanyProfile(ctx, querier, principal.CompanyId)
	if findProfileError != nil {
		return SettingsView{}, findProfileError
	}
	isMissing := companySettings == nil || companyProfile == nil
	if isMissing {
		return SettingsView{}, ErrSettingsNotFound
	}

	changedProfile := applyProfileChanges(*companyProfile, request)
	changedSettings, applyError := applySettingsChanges(*companySettings, request)
	if applyError != nil {
		return SettingsView{}, applyError
	}

	isCurrencyChanging := changedProfile.CurrencyCode != companyProfile.CurrencyCode || changedProfile.CurrencyDecimals != companyProfile.CurrencyDecimals
	if isCurrencyChanging {
		hasSales, salesError := service.repository.HasSales(ctx, querier, principal.CompanyId)
		if salesError != nil {
			return SettingsView{}, salesError
		}
		if hasSales {
			return SettingsView{}, ErrCurrencyLocked
		}
	}
	changedSettings.UpdatedBy = &principal.UserId

	saveProfileError := service.repository.SaveCompanyProfile(ctx, querier, changedProfile)
	if saveProfileError != nil {
		return SettingsView{}, saveProfileError
	}
	saveSettingsError := service.repository.SaveSettings(ctx, querier, changedSettings)
	if saveSettingsError != nil {
		return SettingsView{}, saveSettingsError
	}

	return service.Get(ctx, querier, principal.CompanyId)
}

func (service *Service) UploadLogo(ctx context.Context, querier database.Querier, companyId uuid.UUID, logoBytes []byte) (SettingsView, error) {
	logoKey, storeError := media.StoreImage(ctx, service.objectStore, media.FolderLogos, companyId, logoBytes, MaximumLogoBytes)
	if storeError != nil {
		return SettingsView{}, storeError
	}

	updateError := service.repository.UpdateLogoKey(ctx, querier, companyId, logoKey)
	if updateError != nil {
		return SettingsView{}, updateError
	}

	return service.Get(ctx, querier, companyId)
}

func applyProfileChanges(companyProfile CompanyProfile, request UpdateSettingsRequest) CompanyProfile {
	if request.BusinessName != nil {
		companyProfile.Name = strings.TrimSpace(*request.BusinessName)
	}
	if request.BusinessType != nil {
		companyProfile.BusinessType = strings.TrimSpace(*request.BusinessType)
	}
	if request.BusinessPhone != nil {
		companyProfile.Phone = optionalText(*request.BusinessPhone)
	}
	if request.BusinessAddress != nil {
		companyProfile.Address = optionalText(*request.BusinessAddress)
	}
	if request.BusinessTin != nil {
		companyProfile.Tin = optionalText(*request.BusinessTin)
	}
	if request.ReceiptHeader != nil {
		companyProfile.ReceiptHeader = optionalText(*request.ReceiptHeader)
	}
	if request.ReceiptFooter != nil {
		companyProfile.ReceiptFooter = optionalText(*request.ReceiptFooter)
	}
	if request.PrimaryColor != nil {
		companyProfile.PrimaryColor = strings.ToLower(*request.PrimaryColor)
	}
	if request.CurrencyCode != nil {
		companyProfile.CurrencyCode = *request.CurrencyCode
	}
	if request.CurrencyDecimals != nil {
		companyProfile.CurrencyDecimals = *request.CurrencyDecimals
	}
	if request.Timezone != nil {
		companyProfile.Timezone = *request.Timezone
	}
	if request.DefaultLocale != nil {
		companyProfile.DefaultLocale = *request.DefaultLocale
	}
	return companyProfile
}

func applySettingsChanges(companySettings Settings, request UpdateSettingsRequest) (Settings, error) {
	if request.TaxRate != nil {
		companySettings.TaxRateBasisPoints = int(math.Round(*request.TaxRate * 100))
	}
	if request.DateFormat != nil {
		companySettings.DateFormat = *request.DateFormat
	}
	if request.ReceiptNumberFormat != nil {
		receiptFormat := strings.TrimSpace(*request.ReceiptNumberFormat)
		if !strings.Contains(receiptFormat, "{COUNTER}") {
			return companySettings, ErrReceiptFormat
		}
		companySettings.ReceiptNumberFormat = receiptFormat
	}
	if request.ReceiptLanguage != nil {
		companySettings.ReceiptLanguage = *request.ReceiptLanguage
	}
	if request.EfdEnabled != nil {
		companySettings.EfdEnabled = *request.EfdEnabled
	}
	if request.EfdEndpoint != nil {
		efdEndpoint := optionalText(*request.EfdEndpoint)
		endpointError := checkEfdEndpoint(efdEndpoint)
		if endpointError != nil {
			return companySettings, endpointError
		}
		companySettings.EfdEndpoint = efdEndpoint
	}
	if request.EfdApiKey != nil {
		companySettings.EfdApiKey = optionalText(*request.EfdApiKey)
	}
	if request.LowStockThreshold != nil {
		companySettings.LowStockThreshold = *request.LowStockThreshold
	}
	if request.EmailNotificationsEnabled != nil {
		companySettings.EmailNotificationsEnabled = *request.EmailNotificationsEnabled
	}
	if request.NotificationEmail != nil {
		notificationEmail := optionalText(*request.NotificationEmail)
		if notificationEmail != nil {
			_, parseError := mail.ParseAddress(*notificationEmail)
			if parseError != nil {
				return companySettings, ErrInvalidEmail
			}
		}
		companySettings.NotificationEmail = notificationEmail
	}
	if request.AlertSoundEnabled != nil {
		companySettings.AlertSoundEnabled = *request.AlertSoundEnabled
	}
	if request.AlertOnLowStock != nil {
		companySettings.AlertOnLowStock = *request.AlertOnLowStock
	}
	if request.AlertOnOutOfStock != nil {
		companySettings.AlertOnOutOfStock = *request.AlertOnOutOfStock
	}
	if request.AlertOnDeadStock != nil {
		companySettings.AlertOnDeadStock = *request.AlertOnDeadStock
	}
	if request.DeadStockDays != nil {
		companySettings.DeadStockDays = *request.DeadStockDays
	}
	if request.PrintReceiptAutomatically != nil {
		companySettings.PrintReceiptAutomatically = *request.PrintReceiptAutomatically
	}
	if request.ShowTaxOnReceipt != nil {
		companySettings.ShowTaxOnReceipt = *request.ShowTaxOnReceipt
	}
	if request.ShowBarcodesOnReceipt != nil {
		companySettings.ShowBarcodesOnReceipt = *request.ShowBarcodesOnReceipt
	}
	if request.PrinterEnabled != nil {
		companySettings.PrinterEnabled = *request.PrinterEnabled
	}
	if request.PrinterPort != nil {
		companySettings.PrinterPort = strings.TrimSpace(*request.PrinterPort)
	}
	if request.PrinterModel != nil {
		companySettings.PrinterModel = strings.TrimSpace(*request.PrinterModel)
	}
	if request.PrinterBaudRate != nil {
		companySettings.PrinterBaudRate = *request.PrinterBaudRate
	}
	if request.PrinterPaperWidth != nil {
		companySettings.PrinterPaperWidth = *request.PrinterPaperWidth
	}
	if request.OpenCashDrawer != nil {
		companySettings.OpenCashDrawer = *request.OpenCashDrawer
	}
	return companySettings, nil
}

func checkEfdEndpoint(efdEndpoint *string) error {
	if efdEndpoint == nil {
		return nil
	}
	parsedEndpoint, parseError := url.Parse(*efdEndpoint)
	isHttps := parseError == nil && parsedEndpoint.Scheme == "https" && parsedEndpoint.Host != ""
	if !isHttps {
		return ErrInvalidEfdEndpoint
	}
	return nil
}

func optionalText(rawText string) *string {
	trimmedText := strings.TrimSpace(rawText)
	if trimmedText == "" {
		return nil
	}
	return &trimmedText
}

func toSettingsView(companySettings Settings, companyProfile CompanyProfile) SettingsView {
	return SettingsView{
		Company: CompanyView{
			Id:               companyProfile.Id,
			Name:             companyProfile.Name,
			BusinessType:     companyProfile.BusinessType,
			Phone:            companyProfile.Phone,
			Address:          companyProfile.Address,
			Tin:              companyProfile.Tin,
			LogoUrl:          media.PublicUrl(companyProfile.LogoKey),
			PrimaryColor:     companyProfile.PrimaryColor,
			CurrencyCode:     companyProfile.CurrencyCode,
			CurrencyDecimals: companyProfile.CurrencyDecimals,
			Timezone:         companyProfile.Timezone,
			DefaultLocale:    companyProfile.DefaultLocale,
			ReceiptHeader:    companyProfile.ReceiptHeader,
			ReceiptFooter:    companyProfile.ReceiptFooter,
		},
		TaxRate:                   float64(companySettings.TaxRateBasisPoints) / 100,
		DateFormat:                companySettings.DateFormat,
		ReceiptNumberFormat:       companySettings.ReceiptNumberFormat,
		ReceiptLanguage:           companySettings.ReceiptLanguage,
		EfdEnabled:                companySettings.EfdEnabled,
		EfdEndpoint:               companySettings.EfdEndpoint,
		EfdApiKeySet:              companySettings.EfdApiKey != nil,
		LowStockThreshold:         companySettings.LowStockThreshold,
		EmailNotificationsEnabled: companySettings.EmailNotificationsEnabled,
		NotificationEmail:         companySettings.NotificationEmail,
		AlertSoundEnabled:         companySettings.AlertSoundEnabled,
		AlertOnLowStock:           companySettings.AlertOnLowStock,
		AlertOnOutOfStock:         companySettings.AlertOnOutOfStock,
		AlertOnDeadStock:          companySettings.AlertOnDeadStock,
		DeadStockDays:             companySettings.DeadStockDays,
		PrintReceiptAutomatically: companySettings.PrintReceiptAutomatically,
		ShowTaxOnReceipt:          companySettings.ShowTaxOnReceipt,
		ShowBarcodesOnReceipt:     companySettings.ShowBarcodesOnReceipt,
		PrinterEnabled:            companySettings.PrinterEnabled,
		PrinterPort:               companySettings.PrinterPort,
		PrinterModel:              companySettings.PrinterModel,
		PrinterBaudRate:           companySettings.PrinterBaudRate,
		PrinterPaperWidth:         companySettings.PrinterPaperWidth,
		OpenCashDrawer:            companySettings.OpenCashDrawer,
		UpdatedAt:                 companySettings.UpdatedAt,
	}
}
