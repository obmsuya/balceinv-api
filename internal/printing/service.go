package printing

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"image"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/storage"
	"github.com/chrisostomemataba/balceinv-api/internal/sales"
	"github.com/chrisostomemataba/balceinv-api/internal/settings"
	"github.com/google/uuid"
	"go.bug.st/serial/enumerator"
)

var (
	ErrPrinterOff      = errors.New("the receipt printer is turned off in settings")
	ErrPrinterNotSet   = errors.New("choose the printer port in settings first")
	ErrPrinterNotFound = errors.New("the printer did not answer; check that it is plugged in and switched on")
	ErrMissingSettings = errors.New("company settings are missing")
)

type StatusView struct {
	Enabled    bool   `json:"enabled"`
	Port       string `json:"port"`
	PaperWidth int    `json:"paper_width"`
	OpenDrawer bool   `json:"open_drawer"`
	AutoPrint  bool   `json:"auto_print"`
}

type DetectedPrinter struct {
	Port         string `json:"port"`
	IsUsb        bool   `json:"is_usb"`
	VendorId     string `json:"vendor_id"`
	ProductId    string `json:"product_id"`
	Manufacturer string `json:"manufacturer"`
	Product      string `json:"product"`
}

type Service struct {
	openDatabase       *database.Database
	salesService       *sales.Service
	settingsRepository *settings.Repository
	objectStore        storage.Store
}

func NewService(openDatabase *database.Database, salesService *sales.Service, settingsRepository *settings.Repository, objectStore storage.Store) *Service {
	return &Service{
		openDatabase:       openDatabase,
		salesService:       salesService,
		settingsRepository: settingsRepository,
		objectStore:        objectStore,
	}
}

type printJob struct {
	settings        settings.Settings
	receiptView     *sales.ReceiptView
	logoKey         *string
	companyTimezone string
}

func (service *Service) loadJob(ctx context.Context, companyId uuid.UUID, saleId *uuid.UUID) (printJob, error) {
	readTransaction, beginError := service.openDatabase.Reader.BeginTx(ctx, nil)
	if beginError != nil {
		return printJob{}, fmt.Errorf("failed to begin reading the receipt: %w", beginError)
	}
	defer readTransaction.Rollback()

	setTenantError := database.SetTenant(ctx, readTransaction, service.openDatabase.IsPostgres(), companyId)
	if setTenantError != nil {
		return printJob{}, setTenantError
	}

	companySettings, settingsError := service.settingsRepository.FindSettings(ctx, readTransaction, companyId)
	if settingsError != nil {
		return printJob{}, settingsError
	}
	companyProfile, profileError := service.settingsRepository.FindCompanyProfile(ctx, readTransaction, companyId)
	if profileError != nil {
		return printJob{}, profileError
	}
	if companySettings == nil || companyProfile == nil {
		return printJob{}, ErrMissingSettings
	}

	job := printJob{
		settings:        *companySettings,
		logoKey:         companyProfile.LogoKey,
		companyTimezone: companyProfile.Timezone,
	}
	if saleId != nil {
		receiptView, receiptError := service.salesService.Receipt(ctx, readTransaction, companyId, *saleId)
		if receiptError != nil {
			return printJob{}, receiptError
		}
		job.receiptView = &receiptView
	}
	return job, nil
}

func (service *Service) Status(ctx context.Context, companyId uuid.UUID) (StatusView, error) {
	job, loadError := service.loadJob(ctx, companyId, nil)
	if loadError != nil {
		return StatusView{}, loadError
	}
	statusView := StatusView{
		Enabled:    job.settings.PrinterEnabled,
		Port:       job.settings.PrinterPort,
		PaperWidth: job.settings.PrinterPaperWidth,
		OpenDrawer: job.settings.OpenCashDrawer,
		AutoPrint:  job.settings.PrintReceiptAutomatically,
	}
	return statusView, nil
}

func (service *Service) PrintReceipt(ctx context.Context, companyId uuid.UUID, saleId uuid.UUID, openDrawer bool) error {
	job, loadError := service.loadJob(ctx, companyId, &saleId)
	if loadError != nil {
		return loadError
	}
	if !job.settings.PrinterEnabled {
		return ErrPrinterOff
	}
	if strings.TrimSpace(job.settings.PrinterPort) == "" {
		return ErrPrinterNotSet
	}

	companyLocation, locationError := time.LoadLocation(job.companyTimezone)
	if locationError != nil {
		companyLocation = time.UTC
	}
	receiptBytes := BuildReceipt(*job.receiptView, service.loadLogo(ctx, job.logoKey), openDrawer && job.settings.OpenCashDrawer, time.Now(), companyLocation)
	return writeToPort(job.settings.PrinterPort, receiptBytes)
}

func (service *Service) TestPrint(ctx context.Context, companyId uuid.UUID, portOverride string) (string, error) {
	job, loadError := service.loadJob(ctx, companyId, nil)
	if loadError != nil {
		return "", loadError
	}
	portPath := strings.TrimSpace(portOverride)
	if portPath == "" {
		portPath = strings.TrimSpace(job.settings.PrinterPort)
	}
	if portPath == "" {
		return "", ErrPrinterNotSet
	}
	companyLocation, locationError := time.LoadLocation(job.companyTimezone)
	if locationError != nil {
		companyLocation = time.UTC
	}
	testBytes := BuildTestReceipt(job.settings.PrinterPaperWidth, job.settings.ReceiptLanguage, time.Now().In(companyLocation))
	return portPath, writeToPort(portPath, testBytes)
}

func (service *Service) loadLogo(ctx context.Context, logoKey *string) image.Image {
	if logoKey == nil || *logoKey == "" {
		return nil
	}
	logoObject, getError := service.objectStore.Get(ctx, *logoKey)
	if getError != nil {
		return nil
	}
	logoImage, _, decodeError := image.Decode(bytes.NewReader(logoObject.Body))
	if decodeError != nil {
		return nil
	}
	return logoImage
}

func ListDevices() ([]DetectedPrinter, error) {
	serialPorts, enumerateError := enumerator.GetDetailedPortsList()
	if enumerateError != nil {
		return nil, fmt.Errorf("could not list serial ports: %w", enumerateError)
	}

	detectedPrinters := []DetectedPrinter{}
	for _, serialPort := range serialPorts {
		detectedPrinter := DetectedPrinter{
			Port:         serialPort.Name,
			IsUsb:        serialPort.IsUSB,
			VendorId:     serialPort.VID,
			ProductId:    serialPort.PID,
			Manufacturer: serialPort.Manufacturer,
			Product:      serialPort.Product,
		}
		detectedPrinters = append(detectedPrinters, detectedPrinter)
	}

	rawUsbEntries, readError := os.ReadDir("/dev/usb")
	if readError == nil {
		for _, rawUsbEntry := range rawUsbEntries {
			if strings.HasPrefix(rawUsbEntry.Name(), "lp") {
				detectedPrinter := DetectedPrinter{
					Port:    filepath.Join("/dev/usb", rawUsbEntry.Name()),
					IsUsb:   true,
					Product: "USB printer (raw device)",
				}
				detectedPrinters = append(detectedPrinters, detectedPrinter)
			}
		}
	}
	return detectedPrinters, nil
}

func writeToPort(portPath string, printBytes []byte) error {
	portFile, openError := os.OpenFile(portPath, os.O_WRONLY, 0)
	if openError != nil {
		slog.Warn("printer port could not be opened", "port", portPath, "error", openError)
		return fmt.Errorf("%w (%s)", ErrPrinterNotFound, portPath)
	}
	defer portFile.Close()

	_, writeError := portFile.Write(printBytes)
	if writeError != nil {
		slog.Warn("printer write failed", "port", portPath, "error", writeError)
		return fmt.Errorf("%w (%s)", ErrPrinterNotFound, portPath)
	}
	slog.Info("printed", "port", portPath, "bytes", len(printBytes))
	return nil
}
