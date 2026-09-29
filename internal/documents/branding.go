package documents

import (
	"bytes"
	"context"
	"database/sql"
	"errors"
	"fmt"
	"image"
	_ "image/jpeg"
	"image/png"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/storage"
	"github.com/chrisostomemataba/balceinv-api/internal/features"
	"github.com/chrisostomemataba/balceinv-api/internal/settings"
	"github.com/google/uuid"
	_ "golang.org/x/image/webp"
)

const defaultBrandColor = "#5EA500"

var (
	ErrMissingCompany = errors.New("company settings are missing")

	hexColorPattern = regexp.MustCompile(`^#[0-9A-Fa-f]{6}$`)
)

type Branding struct {
	CompanyName      string
	LogoPng          []byte
	LogoWidth        int
	LogoHeight       int
	PrimaryColor     string
	Tin              string
	Vrn              string
	Address          string
	Phone            string
	CurrencyCode     string
	CurrencyDecimals int
	Timezone         string
	DefaultLanguage  string
	ReceiptLanguage  string
	ReceiptHeader    string
	ReceiptFooter    string
	ShowTaxOnReceipt bool
	VatRegistered    bool
}

type Viewer struct {
	Name     string
	Language string
}

func LoadBranding(ctx context.Context, querier database.Querier, objectStore storage.Store, companyId uuid.UUID) (Branding, error) {
	settingsRepository := settings.NewRepository()
	companyProfile, profileError := settingsRepository.FindCompanyProfile(ctx, querier, companyId)
	if profileError != nil {
		return Branding{}, fmt.Errorf("failed to load the company for a document: %w", profileError)
	}
	companySettings, settingsError := settingsRepository.FindSettings(ctx, querier, companyId)
	if settingsError != nil {
		return Branding{}, fmt.Errorf("failed to load the settings for a document: %w", settingsError)
	}
	if companyProfile == nil || companySettings == nil {
		return Branding{}, ErrMissingCompany
	}
	companyFeatures, featuresError := features.NewRepository().Find(ctx, querier, companyId)
	if featuresError != nil {
		return Branding{}, fmt.Errorf("failed to load the features for a document: %w", featuresError)
	}

	branding := Branding{
		CompanyName:      companyProfile.Name,
		PrimaryColor:     strings.ToUpper(companyProfile.PrimaryColor),
		Tin:              textOf(companyProfile.Tin),
		Address:          textOf(companyProfile.Address),
		Phone:            textOf(companyProfile.Phone),
		CurrencyCode:     companyProfile.CurrencyCode,
		CurrencyDecimals: companyProfile.CurrencyDecimals,
		Timezone:         companyProfile.Timezone,
		DefaultLanguage:  companyProfile.DefaultLocale,
		ReceiptLanguage:  companySettings.ReceiptLanguage,
		ReceiptHeader:    textOf(companyProfile.ReceiptHeader),
		ReceiptFooter:    textOf(companyProfile.ReceiptFooter),
		ShowTaxOnReceipt: companySettings.ShowTaxOnReceipt,
		VatRegistered:    companyFeatures.VatRegistered,
	}
	if companyFeatures.VatRegistered {
		branding.Vrn = textOf(companyFeatures.VatNumber)
	}
	if !hexColorPattern.MatchString(branding.PrimaryColor) {
		branding.PrimaryColor = defaultBrandColor
	}
	branding.LogoPng, branding.LogoWidth, branding.LogoHeight = loadLogo(ctx, objectStore, companyProfile.LogoKey)
	return branding, nil
}

func loadLogo(ctx context.Context, objectStore storage.Store, logoKey *string) ([]byte, int, int) {
	if objectStore == nil || logoKey == nil || *logoKey == "" {
		return nil, 0, 0
	}
	logoObject, getError := objectStore.Get(ctx, *logoKey)
	if getError != nil {
		return nil, 0, 0
	}
	return LogoAsPng(logoObject.Body)
}

func LogoAsPng(imageBytes []byte) ([]byte, int, int) {
	logoImage, _, decodeError := image.Decode(bytes.NewReader(imageBytes))
	if decodeError != nil {
		return nil, 0, 0
	}
	pngBuffer := &bytes.Buffer{}
	encodeError := png.Encode(pngBuffer, logoImage)
	if encodeError != nil {
		return nil, 0, 0
	}
	logoBounds := logoImage.Bounds()
	return pngBuffer.Bytes(), logoBounds.Dx(), logoBounds.Dy()
}

func LoadViewer(ctx context.Context, querier database.Querier, companyId uuid.UUID, userId uuid.UUID) (Viewer, error) {
	query := `SELECT name, locale FROM users WHERE company_id = $1 AND id = $2`

	viewerName := ""
	var viewerLocale *string
	scanError := querier.QueryRowContext(ctx, query, companyId, userId).Scan(&viewerName, &viewerLocale)
	if errors.Is(scanError, sql.ErrNoRows) {
		return Viewer{}, nil
	}
	if scanError != nil {
		return Viewer{}, fmt.Errorf("failed to load who asked for a document: %w", scanError)
	}
	return Viewer{Name: viewerName, Language: textOf(viewerLocale)}, nil
}

func (branding Branding) Location() *time.Location {
	companyLocation, locationError := time.LoadLocation(branding.Timezone)
	if locationError != nil {
		return time.UTC
	}
	return companyLocation
}

func (branding Branding) taxLine(language string) string {
	identityParts := []string{}
	if branding.Tin != "" {
		identityParts = append(identityParts, Label(language, "tin")+": "+branding.Tin)
	}
	if branding.Vrn != "" {
		identityParts = append(identityParts, Label(language, "vrn")+": "+branding.Vrn)
	}
	return strings.Join(identityParts, "  ·  ")
}

func (branding Branding) contactLine() string {
	contactParts := []string{}
	for _, contactPart := range []string{branding.Address, branding.Phone} {
		if strings.TrimSpace(contactPart) != "" {
			contactParts = append(contactParts, strings.Join(strings.Fields(contactPart), " "))
		}
	}
	return strings.Join(contactParts, "  ·  ")
}

func (branding Branding) colorRgb() (int, int, int) {
	colorValue, parseError := strconv.ParseUint(strings.TrimPrefix(branding.PrimaryColor, "#"), 16, 32)
	if parseError != nil {
		return 94, 165, 0
	}
	return int(colorValue >> 16 & 0xFF), int(colorValue >> 8 & 0xFF), int(colorValue & 0xFF)
}

func textOf(value *string) string {
	if value == nil {
		return ""
	}
	return strings.TrimSpace(*value)
}
