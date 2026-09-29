package printing

import (
	"bytes"
	"fmt"
	"image"
	_ "image/jpeg"
	_ "image/png"
	"math"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/sales"
	"github.com/kenshaw/escpos"
)

var receiptLabels = map[string]map[string]string{
	"en": {"receipt": "Receipt", "date": "Date", "cashier": "Served by", "item": "Item", "amount": "Amount", "subtotal": "Subtotal", "discounts": "Discounts", "total": "TOTAL", "tax": "Includes VAT", "paid": "Paid", "change": "Change", "tin": "TIN", "tel": "Tel", "note": "Note", "efd": "EFD verification", "efdPending": "EFD receipt to follow", "thanks": "Thank you for shopping with us", "cash": "Cash", "card": "Card", "mobile": "Mobile money", "customer": "Customer", "credit": "Balance owed", "order": "Order", "test": "Test print OK"},
	"sw": {"receipt": "Risiti", "date": "Tarehe", "cashier": "Umehudumiwa na", "item": "Bidhaa", "amount": "Kiasi", "subtotal": "Jumla ndogo", "discounts": "Punguzo", "total": "JUMLA", "tax": "Inajumuisha VAT", "paid": "Umelipa", "change": "Chenji", "tin": "TIN", "tel": "Simu", "note": "Maelezo", "efd": "Uthibitisho wa EFD", "efdPending": "Risiti ya EFD itafuata", "thanks": "Asante kwa kununua kwetu", "cash": "Taslimu", "card": "Kadi", "mobile": "Pesa ya simu", "customer": "Mteja", "credit": "Deni", "order": "Oda", "test": "Jaribio la printa limefaulu"},
}

func labelsFor(language string) map[string]string {
	chosenLabels, isKnown := receiptLabels[language]
	if !isKnown {
		return receiptLabels["en"]
	}
	return chosenLabels
}

func columnCount(paperWidthMillimeters int) int {
	if paperWidthMillimeters == 58 {
		return 32
	}
	return 48
}

func BuildReceipt(receiptView sales.ReceiptView, logoImage image.Image, openDrawer bool, printedAt time.Time, companyLocation *time.Location) []byte {
	columns := columnCount(receiptView.PaperWidthMillis)
	labels := labelsFor(receiptView.ReceiptLanguage)
	saleView := receiptView.Sale
	money := func(minorUnits int64) string {
		return formatMoney(minorUnits, saleView.CurrencyCode, saleView.CurrencyDecimals)
	}

	receiptBuffer := &bytes.Buffer{}
	printer := escpos.New(receiptBuffer)
	printer.Init()

	if logoImage != nil {
		receiptBuffer.Write(rasterCommand(logoImage, columns))
		printer.Formfeed()
	}

	printer.SetAlign("center")
	printer.SetEmphasize(1)
	printer.SetFontSize(2, 2)
	printer.Write(fitText(receiptView.Company.Name, columns/2) + "\n")
	printer.SetFontSize(1, 1)
	printer.SetEmphasize(0)
	printer.Write(centerText(receiptView.Shop.Name, columns) + "\n")
	shopAddress := firstNonEmpty(receiptView.Shop.Address, receiptView.Company.Address)
	if shopAddress != nil {
		printer.Write(centerText(*shopAddress, columns) + "\n")
	}
	phoneNumber := firstNonEmpty(receiptView.Shop.Phone, receiptView.Company.Phone)
	if phoneNumber != nil {
		printer.Write(centerText(labels["tel"]+": "+*phoneNumber, columns) + "\n")
	}
	if receiptView.Company.Tin != nil && *receiptView.Company.Tin != "" {
		printer.Write(centerText(labels["tin"]+": "+*receiptView.Company.Tin, columns) + "\n")
	}
	if receiptView.Company.ReceiptHeader != nil && *receiptView.Company.ReceiptHeader != "" {
		printer.Write(wrapCentered(*receiptView.Company.ReceiptHeader, columns))
	}

	printer.SetAlign("left")
	printer.Write(strings.Repeat("-", columns) + "\n")
	printer.Write(labels["receipt"] + ": " + saleView.ReceiptNumber + "\n")
	printer.Write(labels["date"] + ": " + saleView.CreatedAt.In(companyLocation).Format("02/01/2006 15:04") + "\n")
	printer.Write(labels["cashier"] + ": " + saleView.CashierName + "\n")
	if saleView.CustomerName != nil {
		printer.Write(fitText(labels["customer"]+": "+*saleView.CustomerName, columns) + "\n")
	}
	if saleView.OrderNumber != nil {
		printer.Write(labels["order"] + ": " + *saleView.OrderNumber + "\n")
	}
	printer.Write(strings.Repeat("-", columns) + "\n")
	printer.SetEmphasize(1)
	printer.Write(leftRightText(labels["item"], labels["amount"], columns) + "\n")
	printer.SetEmphasize(0)

	for _, saleLine := range saleView.Items {
		lineName := saleLine.ProductName
		if saleLine.VariantLabel != "" {
			lineName += " " + saleLine.VariantLabel
		}
		printer.Write(fitText(lineName, columns) + "\n")
		for _, addonView := range saleLine.Addons {
			printer.Write(fitText("  + "+addonView.Name, columns) + "\n")
		}
		wholesaleMarker := ""
		if saleLine.IsWholesale {
			wholesaleMarker = " [W]"
		}
		quantityText := fmt.Sprintf("  %d x %s%s", saleLine.Quantity, money(saleLine.UnitPrice+saleLine.AddonsUnitTotal), wholesaleMarker)
		printer.Write(leftRightText(quantityText, money(saleLine.LineTotal+saleLine.DiscountAmount), columns) + "\n")
		if saleLine.DiscountAmount > 0 {
			discountName := labels["discounts"]
			if saleLine.DiscountName != nil {
				discountName = *saleLine.DiscountName
			}
			printer.Write(leftRightText("  "+fitText(discountName, columns-16), "-"+money(saleLine.DiscountAmount), columns) + "\n")
		}
	}

	printer.Write(strings.Repeat("-", columns) + "\n")
	if saleView.DiscountTotal > 0 {
		printer.Write(leftRightText(labels["subtotal"], money(saleView.Subtotal), columns) + "\n")
		printer.Write(leftRightText(labels["discounts"], "-"+money(saleView.DiscountTotal), columns) + "\n")
	}
	printer.Write(strings.Repeat("=", columns) + "\n")
	printer.SetEmphasize(1)
	printer.Write(leftRightText(labels["total"], money(saleView.Total), columns) + "\n")
	printer.SetEmphasize(0)
	if receiptView.ShowTax && saleView.TaxTotal > 0 {
		taxLabel := fmt.Sprintf("%s %s%%", labels["tax"], trimRate(saleView.TaxRateBasisPoints))
		printer.Write(leftRightText(taxLabel, money(saleView.TaxTotal), columns) + "\n")
	}

	printer.Write(strings.Repeat("-", columns) + "\n")
	for _, paymentView := range saleView.Payments {
		if paymentView.Method == sales.PaymentCredit {
			continue
		}
		methodLabel := labels[paymentView.Method]
		printer.Write(leftRightText(labels["paid"]+" ("+methodLabel+")", money(paymentView.Amount), columns) + "\n")
	}
	if saleView.CreditAmount > 0 {
		printer.SetEmphasize(1)
		printer.Write(leftRightText(labels["credit"], money(saleView.CreditAmount), columns) + "\n")
		printer.SetEmphasize(0)
	}
	if saleView.ChangeGiven > 0 {
		printer.SetEmphasize(1)
		printer.Write(leftRightText(labels["change"], money(saleView.ChangeGiven), columns) + "\n")
		printer.SetEmphasize(0)
	}
	if saleView.Note != nil && *saleView.Note != "" {
		printer.Write(fitText(labels["note"]+": "+*saleView.Note, columns*3) + "\n")
	}

	if saleView.Fiscal != nil {
		printer.Write(strings.Repeat("-", columns) + "\n")
		printer.SetAlign("center")
		isSent := saleView.Fiscal.Status == "sent"
		if isSent {
			printer.Write(labels["efd"] + "\n")
			if saleView.Fiscal.VerificationCode != nil {
				printer.SetEmphasize(1)
				printer.Write(*saleView.Fiscal.VerificationCode + "\n")
				printer.SetEmphasize(0)
			}
		} else {
			printer.Write(labels["efdPending"] + "\n")
		}
	}

	printer.Formfeed()
	printer.SetAlign("center")
	footerText := labels["thanks"]
	if receiptView.Company.ReceiptFooter != nil && *receiptView.Company.ReceiptFooter != "" {
		footerText = *receiptView.Company.ReceiptFooter
	}
	printer.Write(wrapCentered(footerText, columns))
	printer.Write(centerText(printedAt.In(companyLocation).Format("02/01/2006 15:04:05"), columns) + "\n")
	printer.Formfeed()
	printer.Formfeed()
	printer.Cut()
	if openDrawer {
		receiptBuffer.Write([]byte{0x1B, 0x70, 0x00, 0x19, 0xFA})
	}
	printer.End()

	return receiptBuffer.Bytes()
}

func BuildTestReceipt(paperWidthMillimeters int, language string, printedAt time.Time) []byte {
	columns := columnCount(paperWidthMillimeters)
	receiptBuffer := &bytes.Buffer{}
	printer := escpos.New(receiptBuffer)
	printer.Init()
	printer.SetAlign("center")
	printer.SetEmphasize(1)
	printer.Write("Balce\n")
	printer.SetEmphasize(0)
	printer.Write(centerText(labelsFor(language)["test"], columns) + "\n")
	printer.Write(centerText(fmt.Sprintf("%d mm, %d columns", paperWidthMillimeters, columns), columns) + "\n")
	printer.Write(centerText(printedAt.Format("02/01/2006 15:04:05"), columns) + "\n")
	printer.Formfeed()
	printer.Cut()
	printer.End()
	return receiptBuffer.Bytes()
}

func formatMoney(minorUnits int64, currencyCode string, decimals int) string {
	isNegative := minorUnits < 0
	if isNegative {
		minorUnits = -minorUnits
	}
	divisor := int64(math.Pow10(decimals))
	wholePart := minorUnits / divisor
	fractionPart := minorUnits % divisor

	wholeDigits := fmt.Sprintf("%d", wholePart)
	groupedDigits := ""
	for digitIndex, digit := range wholeDigits {
		if digitIndex > 0 && (len(wholeDigits)-digitIndex)%3 == 0 {
			groupedDigits += ","
		}
		groupedDigits += string(digit)
	}
	if decimals > 0 {
		groupedDigits += fmt.Sprintf(".%0*d", decimals, fractionPart)
	}
	if isNegative {
		groupedDigits = "-" + groupedDigits
	}
	return currencyCode + " " + groupedDigits
}

func trimRate(basisPoints int) string {
	return strings.TrimSuffix(strings.TrimRight(fmt.Sprintf("%.2f", float64(basisPoints)/100), "0"), ".")
}

func firstNonEmpty(candidates ...*string) *string {
	for _, candidate := range candidates {
		if candidate != nil && strings.TrimSpace(*candidate) != "" {
			return candidate
		}
	}
	return nil
}

func fitText(text string, columns int) string {
	textRunes := []rune(text)
	if len(textRunes) <= columns {
		return text
	}
	return string(textRunes[:columns-1]) + "…"
}

func centerText(text string, columns int) string {
	textLength := len([]rune(text))
	if textLength >= columns {
		return fitText(text, columns)
	}
	return strings.Repeat(" ", (columns-textLength)/2) + text
}

func wrapCentered(text string, columns int) string {
	wrappedText := ""
	for _, paragraph := range strings.Split(text, "\n") {
		currentLine := ""
		for _, word := range strings.Fields(paragraph) {
			candidateLine := strings.TrimSpace(currentLine + " " + word)
			if len([]rune(candidateLine)) > columns && currentLine != "" {
				wrappedText += centerText(currentLine, columns) + "\n"
				currentLine = word
				continue
			}
			currentLine = candidateLine
		}
		wrappedText += centerText(currentLine, columns) + "\n"
	}
	return wrappedText
}

func leftRightText(leftText string, rightText string, columns int) string {
	spacing := columns - len([]rune(leftText)) - len([]rune(rightText))
	if spacing < 1 {
		leftText = fitText(leftText, columns-len([]rune(rightText))-1)
		spacing = 1
	}
	return leftText + strings.Repeat(" ", spacing) + rightText
}

func rasterCommand(sourceImage image.Image, columns int) []byte {
	targetWidthDots := columns * 8
	if targetWidthDots > 384 {
		targetWidthDots = 384
	}
	bounds := sourceImage.Bounds()
	sourceWidth := bounds.Dx()
	sourceHeight := bounds.Dy()
	if sourceWidth == 0 || sourceHeight == 0 {
		return nil
	}
	scale := float64(targetWidthDots) / float64(sourceWidth)
	targetHeightDots := int(math.Round(float64(sourceHeight) * scale))
	if targetHeightDots > 240 {
		targetHeightDots = 240
		scale = float64(targetHeightDots) / float64(sourceHeight)
		targetWidthDots = int(math.Round(float64(sourceWidth) * scale))
	}
	widthBytes := (targetWidthDots + 7) / 8
	rasterData := make([]byte, widthBytes*targetHeightDots)

	for targetY := 0; targetY < targetHeightDots; targetY++ {
		for targetX := 0; targetX < targetWidthDots; targetX++ {
			sourceX := minInt(int(float64(targetX)/scale), sourceWidth-1)
			sourceY := minInt(int(float64(targetY)/scale), sourceHeight-1)
			red, green, blue, alpha := sourceImage.At(bounds.Min.X+sourceX, bounds.Min.Y+sourceY).RGBA()
			luminance := (red*299 + green*587 + blue*114) / 1000
			isDark := alpha > 0x8000 && luminance < 0x8000
			if isDark {
				rasterData[targetY*widthBytes+targetX/8] |= 1 << uint(7-targetX%8)
			}
		}
	}

	command := []byte{0x1D, 0x76, 0x30, 0x00, byte(widthBytes & 0xFF), byte(widthBytes >> 8), byte(targetHeightDots & 0xFF), byte(targetHeightDots >> 8)}
	return append(command, rasterData...)
}

func minInt(first int, second int) int {
	if first < second {
		return first
	}
	return second
}
