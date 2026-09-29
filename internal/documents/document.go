package documents

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"
)

type Kind int

const (
	Text Kind = iota
	Integer
	Money
	Percent
	Date
	DateTime
)

const (
	FormatExcel = "xlsx"
	FormatPdf   = "pdf"

	ExcelContentType = "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet"
	PdfContentType   = "application/pdf"

	English = "en"
	Swahili = "sw"
)

var ErrUnknownFormat = errors.New("choose xlsx or pdf")

type Column struct {
	Title string
	Kind  Kind
	Sum   bool
}

type Table struct {
	Title   string
	Columns []Column
	Rows    [][]any
}

type Field struct {
	Label  string
	Value  any
	Kind   Kind
	Strong bool
}

type Document struct {
	Language    string
	Title       string
	Number      string
	Subtitle    string
	GeneratedBy string
	GeneratedAt time.Time
	Landscape   bool
	Details     []Field
	Cards       []Field
	Tables      []Table
	Totals      []Field
	Notes       []string
	QrCode      string
	QrCaption   string
	Filters     []Field
}

type File struct {
	Name        string
	ContentType string
	Bytes       []byte
}

func Render(branding Branding, document Document, format string, baseName string) (File, error) {
	switch format {
	case FormatExcel:
		workbookBytes, excelError := Excel(branding, document)
		if excelError != nil {
			return File{}, excelError
		}
		return File{Name: baseName + ".xlsx", ContentType: ExcelContentType, Bytes: workbookBytes}, nil
	case FormatPdf:
		pdfBytes, pdfError := Pdf(branding, document)
		if pdfError != nil {
			return File{}, pdfError
		}
		return File{Name: baseName + ".pdf", ContentType: PdfContentType, Bytes: pdfBytes}, nil
	default:
		return File{}, ErrUnknownFormat
	}
}

func NormalizeFormat(requestedFormat string) (string, error) {
	switch strings.ToLower(strings.TrimSpace(requestedFormat)) {
	case "", FormatExcel:
		return FormatExcel, nil
	case FormatPdf:
		return FormatPdf, nil
	default:
		return "", ErrUnknownFormat
	}
}

func ResolveLanguage(candidates ...string) string {
	for _, candidate := range candidates {
		trimmedCandidate := strings.ToLower(strings.TrimSpace(candidate))
		if trimmedCandidate == English || trimmedCandidate == Swahili {
			return trimmedCandidate
		}
	}
	return English
}

func (document Document) language() string {
	return ResolveLanguage(document.Language)
}

func FormatMoney(minorUnits int64, decimals int) string {
	isNegative := minorUnits < 0
	absoluteUnits := minorUnits
	if isNegative {
		absoluteUnits = -minorUnits
	}
	divisor := int64(1)
	for range decimals {
		divisor *= 10
	}
	wholeText := groupThousands(strconv.FormatInt(absoluteUnits/divisor, 10))
	if decimals > 0 {
		wholeText += "." + fmt.Sprintf("%0*d", decimals, absoluteUnits%divisor)
	}
	if isNegative {
		return "-" + wholeText
	}
	return wholeText
}

func FormatCurrency(minorUnits int64, branding Branding) string {
	return strings.TrimSpace(branding.CurrencyCode + " " + FormatMoney(minorUnits, branding.CurrencyDecimals))
}

func FormatInteger(value int64) string {
	if value < 0 {
		return "-" + groupThousands(strconv.FormatInt(-value, 10))
	}
	return groupThousands(strconv.FormatInt(value, 10))
}

func FormatPercent(basisPoints int64) string {
	return strconv.FormatFloat(float64(basisPoints)/100, 'f', 1, 64) + "%"
}

func groupThousands(digits string) string {
	if len(digits) <= 3 {
		return digits
	}
	leadingDigits := len(digits) % 3
	if leadingDigits == 0 {
		leadingDigits = 3
	}
	groupedBuilder := strings.Builder{}
	groupedBuilder.WriteString(digits[:leadingDigits])
	for groupStart := leadingDigits; groupStart < len(digits); groupStart += 3 {
		groupedBuilder.WriteString(",")
		groupedBuilder.WriteString(digits[groupStart : groupStart+3])
	}
	return groupedBuilder.String()
}

func FormatDate(language string, moment time.Time) string {
	return fmt.Sprintf("%d %s %d", moment.Day(), monthName(language, moment.Month()), moment.Year())
}

func FormatDateTime(language string, moment time.Time) string {
	return FormatDate(language, moment) + " " + moment.Format("15:04")
}

func PeriodText(language string, firstDay time.Time, lastDay time.Time) string {
	if firstDay.Year() == lastDay.Year() && firstDay.YearDay() == lastDay.YearDay() {
		return FormatDate(language, firstDay)
	}
	return FormatDate(language, firstDay) + " – " + FormatDate(language, lastDay)
}

func FormatValue(branding Branding, language string, kind Kind, value any) string {
	switch typedValue := value.(type) {
	case nil:
		return ""
	case string:
		return typedValue
	case time.Time:
		localMoment := typedValue.In(branding.Location())
		if kind == Date {
			return FormatDate(language, localMoment)
		}
		return FormatDateTime(language, localMoment)
	case *time.Time:
		if typedValue == nil {
			return ""
		}
		return FormatValue(branding, language, kind, *typedValue)
	}

	integerValue, isInteger := asInteger(value)
	if !isInteger {
		return fmt.Sprint(value)
	}
	switch kind {
	case Money:
		return FormatMoney(integerValue, branding.CurrencyDecimals)
	case Percent:
		return FormatPercent(integerValue)
	default:
		return FormatInteger(integerValue)
	}
}

func asInteger(value any) (int64, bool) {
	switch typedValue := value.(type) {
	case int:
		return int64(typedValue), true
	case int32:
		return int64(typedValue), true
	case int64:
		return typedValue, true
	default:
		return 0, false
	}
}

func SafeFileName(rawName string) string {
	nameBuilder := strings.Builder{}
	for _, character := range rawName {
		isLetter := (character >= 'a' && character <= 'z') || (character >= 'A' && character <= 'Z')
		isDigit := character >= '0' && character <= '9'
		if isLetter || isDigit || character == '-' || character == '_' || character == '.' {
			nameBuilder.WriteRune(character)
			continue
		}
		nameBuilder.WriteRune('-')
	}
	safeName := strings.Trim(nameBuilder.String(), "-.")
	if safeName == "" {
		return "document"
	}
	return safeName
}
