package services

import (
	"bufio"
	"bytes"
	"encoding/base64"
	"fmt"
	"image"
	"image/color"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"os"
	"strings"
	"time"

	"github.com/kenshaw/escpos"

	"github.com/chrisostomemataba/balceinv-api/models"
	"github.com/chrisostomemataba/balceinv-api/repository"
)

// PrintService writes ESC/POS receipts to a configured printer port.
// Every call reads current settings so that port or paper-width changes
// take effect immediately without restarting the server.
type PrintService struct {
	saleRepository     *repository.SaleRepository
	settingsRepository *repository.SettingsRepository
}

func NewPrintService(
	saleRepository *repository.SaleRepository,
	settingsRepository *repository.SettingsRepository,
) *PrintService {
	return &PrintService{
		saleRepository:     saleRepository,
		settingsRepository: settingsRepository,
	}
}

// columnCount returns the number of character columns for a given paper width.
// At standard thermal font size: 58mm → 32 cols, 80mm → 48 cols.
func columnCount(paperWidthMM int) int {
	if paperWidthMM == 58 {
		return 32
	}
	return 48
}

// PrintReceipt is the single public entry point called after a completed sale.
func (service *PrintService) PrintReceipt(saleID uint, openDrawer bool) error {
	settings, err := service.settingsRepository.GetSettings()
	if err != nil {
		return fmt.Errorf("could not load settings: %w", err)
	}

	if !settings.PrinterEnabled {
		return nil
	}

	if settings.PrinterPort == "" {
		return fmt.Errorf("printer port is not configured in settings")
	}

	sale, err := service.saleRepository.FindByID(saleID)
	if err != nil {
		return fmt.Errorf("sale %d not found: %w", saleID, err)
	}

	receiptBytes, err := service.buildReceipt(sale, settings, openDrawer)
	if err != nil {
		return fmt.Errorf("could not build receipt: %w", err)
	}

	return service.writeToPort(settings.PrinterPort, receiptBytes)
}

// buildReceipt assembles the full ESC/POS byte sequence.
// Delegated into smaller sections to keep cognitive complexity low.
func (service *PrintService) buildReceipt(
	sale *models.Sale,
	settings *models.Settings,
	openDrawer bool,
) ([]byte, error) {
	columns := columnCount(settings.PrinterPaperWidth)
	buffer := &bytes.Buffer{}
	writer := bufio.NewWriter(buffer)
	// escpos.New expects an io.ReadWriter. bytes.Buffer implements
	// io.ReadWriter (Read and Write), while *bufio.Writer does not
	// implement Read, so pass the underlying buffer.
	printer := escpos.New(buffer)

	printer.Init()

	service.printLogo(printer, buffer, writer, settings.Company, columns)
	service.printHeader(printer, settings.Company, columns)
	service.printMeta(printer, sale, columns)
	service.printItems(printer, sale, settings.Currency, columns)
	service.printTotals(printer, sale, settings, columns)
	service.printFooter(printer, settings.Company, columns)

	if openDrawer && settings.OpenCashDrawer {
		printer.Cut()
		// ESC p — cash drawer kick pulse on pin 2
		writer.Write([]byte{0x1B, 0x70, 0x00, 0x19, 0xFA})
	} else {
		printer.Cut()
	}

	printer.End()
	writer.Flush()

	return buffer.Bytes(), nil
}

// printLogo renders the company logo as a raster image if one is set.
func (service *PrintService) printLogo(
	printer *escpos.Escpos,
	buffer *bytes.Buffer,
	writer *bufio.Writer,
	company models.Company,
	columns int,
) {
	if company.Logo == nil || *company.Logo == "" {
		return
	}

	logoBytes, err := renderLogoToRaster(*company.Logo, columns)
	if err != nil {
		return
	}

	writer.Flush()
	buffer.Write(logoBytes)
	printer.Formfeed()
}

// printHeader writes the business name, address, phone, TIN, and receipt header.
func (service *PrintService) printHeader(
	printer *escpos.Escpos,
	company models.Company,
	columns int,
) {
	printer.SetAlign("center")
	printer.SetEmphasize(1)
	printer.SetFontSize(2, 2)
	printer.Write(company.Name + "\n")
	printer.SetFontSize(1, 1)
	printer.SetEmphasize(0)

	if company.Address != nil && *company.Address != "" {
		printer.Write(centerText(*company.Address, columns) + "\n")
	}
	if company.Phone != nil && *company.Phone != "" {
		printer.Write(centerText("Tel: "+*company.Phone, columns) + "\n")
	}
	if company.TIN != nil && *company.TIN != "" {
		printer.Write(centerText("TIN: "+*company.TIN, columns) + "\n")
	}
	if company.ReceiptHeader != nil && *company.ReceiptHeader != "" {
		printer.Formfeed()
		printer.Write(centerText(*company.ReceiptHeader, columns) + "\n")
	}
}

// printMeta writes receipt number, date, cashier, and payment type.
func (service *PrintService) printMeta(
	printer *escpos.Escpos,
	sale *models.Sale,
	columns int,
) {
	printer.SetAlign("left")
	printer.Write(dividerLine(columns, '-') + "\n")
	printer.Write(fmt.Sprintf("Receipt : %s\n", sale.ReceiptNumber))
	printer.Write(fmt.Sprintf("Date    : %s\n", sale.CreatedAt.Format("02/01/2006 15:04")))
	printer.Write(fmt.Sprintf("Cashier : %s\n", sale.User.Name))
	printer.Write(fmt.Sprintf("Payment : %s\n", strings.ToUpper(sale.PaymentType)))
	printer.Write(dividerLine(columns, '-') + "\n")

	printer.SetEmphasize(1)
	printer.Write(leftRightText("Item", "Amount", columns) + "\n")
	printer.SetEmphasize(0)
	printer.Write(dividerLine(columns, '-') + "\n")
}

// printItems writes each sale line item.
func (service *PrintService) printItems(
	printer *escpos.Escpos,
	sale *models.Sale,
	currency string,
	columns int,
) {
	for _, item := range sale.Items {
		productName := item.Product.Name
		if len(productName) > columns {
			productName = productName[:columns-1]
		}
		printer.Write(productName + "\n")

		wholesaleMarker := ""
		if item.IsWholesale {
			wholesaleMarker = " [W]"
		}

		quantityAndPrice := fmt.Sprintf(
			"  %d x %s%s",
			item.Quantity,
			formatReceiptAmount(item.UnitPrice, currency),
			wholesaleMarker,
		)
		printer.Write(leftRightText(quantityAndPrice, formatReceiptAmount(item.TotalPrice, currency), columns) + "\n")
	}
}

// printTotals writes subtotal, optional VAT line, and the grand total.
func (service *PrintService) printTotals(
	printer *escpos.Escpos,
	sale *models.Sale,
	settings *models.Settings,
	columns int,
) {
	printer.Write(dividerLine(columns, '-') + "\n")

	subtotal := sale.TotalAmount - sale.TaxAmount
	printer.Write(leftRightText("Subtotal", formatReceiptAmount(subtotal, settings.Currency), columns) + "\n")

	if settings.ShowTaxOnReceipt {
		taxLabel := fmt.Sprintf("VAT (%.0f%%)", settings.TaxRate)
		printer.Write(leftRightText(taxLabel, formatReceiptAmount(sale.TaxAmount, settings.Currency), columns) + "\n")
	}

	printer.Write(dividerLine(columns, '=') + "\n")
	printer.SetEmphasize(1)
	printer.Write(leftRightText("TOTAL", formatReceiptAmount(sale.TotalAmount, settings.Currency), columns) + "\n")
	printer.SetEmphasize(0)
}

// printFooter writes the receipt footer text and timestamp.
func (service *PrintService) printFooter(
	printer *escpos.Escpos,
	company models.Company,
	columns int,
) {
	printer.Formfeed()
	printer.SetAlign("center")

	if company.ReceiptFooter != nil && *company.ReceiptFooter != "" {
		printer.Write(centerText(*company.ReceiptFooter, columns) + "\n")
	}

	printer.Write(centerText(time.Now().Format("02/01/2006 15:04:05"), columns) + "\n")
	printer.Formfeed()
	printer.Formfeed()
}

// writeToPort opens the OS printer device file and writes the byte stream.
// Linux: /dev/usb/lp0 (USB) or /dev/ttyUSB0 (serial).
// Windows: COM3, COM4, etc.
func (service *PrintService) writeToPort(portPath string, receiptData []byte) error {
	portFile, err := os.OpenFile(portPath, os.O_WRONLY, 0600)
	if err != nil {
		return fmt.Errorf("cannot open printer port %s: %w", portPath, err)
	}
	defer portFile.Close()

	_, err = portFile.Write(receiptData)
	if err != nil {
		return fmt.Errorf("write to printer port failed: %w", err)
	}
	return nil
}

// ── Layout helpers ─────────────────────────────────────────────────────────

func dividerLine(columns int, char rune) string {
	return strings.Repeat(string(char), columns)
}

func centerText(text string, columns int) string {
	if len(text) >= columns {
		return text
	}
	paddingSize := (columns - len(text)) / 2
	return strings.Repeat(" ", paddingSize) + text
}

func leftRightText(leftText, rightText string, columns int) string {
	spacingSize := columns - len(leftText) - len(rightText)
	if spacingSize < 1 {
		spacingSize = 1
	}
	return leftText + strings.Repeat(" ", spacingSize) + rightText
}

func formatReceiptAmount(amount float64, currency string) string {
	raw := fmt.Sprintf("%.0f", amount)
	runes := []rune(raw)
	total := len(runes)
	result := ""
	for index, character := range runes {
		if index > 0 && (total-index)%3 == 0 {
			result += ","
		}
		result += string(character)
	}
	return currency + " " + result
}

// ── Logo rendering ─────────────────────────────────────────────────────────

// renderLogoToRaster decodes a base64 data URI and converts to ESC/POS
// GS v 0 raster format, scaled to fit the receipt width.
func renderLogoToRaster(dataURI string, columns int) ([]byte, error) {
	imageBytes, err := decodeDataURI(dataURI)
	if err != nil {
		return nil, err
	}

	sourceImage, err := decodeImage(imageBytes)
	if err != nil {
		return nil, err
	}

	return buildRasterCommand(sourceImage, columns), nil
}

func decodeDataURI(dataURI string) ([]byte, error) {
	separatorIndex := strings.Index(dataURI, ",")
	if separatorIndex < 0 {
		return nil, fmt.Errorf("invalid data URI: missing comma separator")
	}
	decoded, err := base64.StdEncoding.DecodeString(dataURI[separatorIndex+1:])
	if err != nil {
		return nil, fmt.Errorf("base64 decode failed: %w", err)
	}
	return decoded, nil
}

func decodeImage(imageBytes []byte) (image.Image, error) {
	sourceImage, _, err := image.Decode(bytes.NewReader(imageBytes))
	if err != nil {
		return nil, fmt.Errorf("image decode failed: %w", err)
	}
	return sourceImage, nil
}

func buildRasterCommand(sourceImage image.Image, columns int) []byte {
	targetWidthDots := columns * 8
	if targetWidthDots > 576 {
		targetWidthDots = 576
	}

	bounds := sourceImage.Bounds()
	sourceWidth := bounds.Max.X - bounds.Min.X
	sourceHeight := bounds.Max.Y - bounds.Min.Y

	scale := float64(targetWidthDots) / float64(sourceWidth)
	targetHeightDots := int(math.Round(float64(sourceHeight) * scale))

	widthBytes := (targetWidthDots + 7) / 8
	rasterData := convertToMonochrome(sourceImage, bounds, MonochromeParams{
		sourceWidth:     sourceWidth,
		sourceHeight:    sourceHeight,
		targetWidthDots: targetWidthDots,
		targetHeightDots: targetHeightDots,
		widthBytes:      widthBytes,
		scale:           scale,
	})

	xLow := byte(widthBytes & 0xFF)
	xHigh := byte((widthBytes >> 8) & 0xFF)
	yLow := byte(targetHeightDots & 0xFF)
	yHigh := byte((targetHeightDots >> 8) & 0xFF)

	command := []byte{0x1D, 0x76, 0x30, 0x00, xLow, xHigh, yLow, yHigh}
	return append(command, rasterData...)
}

type MonochromeParams struct {
	sourceWidth      int
	sourceHeight     int
	targetWidthDots  int
	targetHeightDots int
	widthBytes       int
	scale            float64
}

func convertToMonochrome(
	sourceImage image.Image,
	bounds image.Rectangle,
	params MonochromeParams,
) []byte {
	rasterData := make([]byte, params.widthBytes*params.targetHeightDots)

	for targetY := 0; targetY < params.targetHeightDots; targetY++ {
		for targetX := 0; targetX < params.targetWidthDots; targetX++ {
			sourceX := clampInt(int(float64(targetX)/params.scale), 0, params.sourceWidth-1)
			sourceY := clampInt(int(float64(targetY)/params.scale), 0, params.sourceHeight-1)

			red, green, blue, _ := sourceImage.At(bounds.Min.X+sourceX, bounds.Min.Y+sourceY).RGBA()
			gray := color.Gray{Y: uint8((red*299 + green*587 + blue*114) / 1000 / 256)}

			if gray.Y < 128 {
				bytePos := targetY*params.widthBytes + targetX/8
				bitPos := uint(7 - (targetX % 8))
				rasterData[bytePos] |= 1 << bitPos
			}
		}
	}

	return rasterData
}

func clampInt(value, minimum, maximum int) int {
	if value < minimum {
		return minimum
	}
	if value > maximum {
		return maximum
	}
	return value
}