package documents

import (
	"strings"
	"time"
)

type Tone int

const (
	ToneNeutral Tone = iota
	ToneGood
	ToneWarning
	ToneBad
)

type InvoiceParty struct {
	Heading string
	Name    string
	Lines   []string
}

type InvoiceLine struct {
	Description string
	Detail      string
	Quantity    int64
	Unit        string
	UnitPrice   int64
	Discount    int64
	Amount      int64
}

type InvoiceDocument struct {
	Language       string
	Title          string
	Number         string
	Status         string
	StatusTone     Tone
	Party          InvoiceParty
	Facts          []Field
	Lines          []InvoiceLine
	HidePrices     bool
	Totals         []Field
	AmountInWords  string
	PaymentDetails string
	Terms          string
	Notes          []string
	Signatures     []string
	QrCode         string
	QrCaption      string
	GeneratedBy    string
	GeneratedAt    time.Time
}

func RenderInvoice(branding Branding, invoice InvoiceDocument, baseName string) (File, error) {
	pdfBytes, pdfError := InvoicePdf(branding, invoice)
	if pdfError != nil {
		return File{}, pdfError
	}
	return File{Name: SafeFileName(baseName) + ".pdf", ContentType: PdfContentType, Bytes: pdfBytes}, nil
}

var englishOnes = []string{"zero", "one", "two", "three", "four", "five", "six", "seven", "eight", "nine", "ten",
	"eleven", "twelve", "thirteen", "fourteen", "fifteen", "sixteen", "seventeen", "eighteen", "nineteen"}

var englishTens = []string{"", "", "twenty", "thirty", "forty", "fifty", "sixty", "seventy", "eighty", "ninety"}

var swahiliOnes = []string{"sifuri", "moja", "mbili", "tatu", "nne", "tano", "sita", "saba", "nane", "tisa"}

var swahiliTens = []string{"", "kumi", "ishirini", "thelathini", "arobaini", "hamsini", "sitini", "sabini", "themanini", "tisini"}

func AmountInWords(language string, minorUnits int64, branding Branding) string {
	if minorUnits < 0 {
		minorUnits = -minorUnits
	}
	divisor := int64(1)
	for range branding.CurrencyDecimals {
		divisor *= 10
	}
	wholeUnits := minorUnits / divisor
	fractionUnits := minorUnits % divisor
	isShilling := strings.EqualFold(branding.CurrencyCode, "TZS") || branding.CurrencyCode == ""

	if ResolveLanguage(language) == Swahili {
		currencyName := branding.CurrencyCode
		if isShilling {
			currencyName = "Shilingi"
		}
		wordsText := currencyName + " " + swahiliNumber(wholeUnits)
		if fractionUnits > 0 {
			wordsText += " na senti " + swahiliNumber(fractionUnits)
		}
		return wordsText + " tu"
	}

	currencyName := branding.CurrencyCode
	if isShilling {
		currencyName = "shillings"
	}
	wordsText := englishNumber(wholeUnits) + " " + currencyName
	if fractionUnits > 0 {
		wordsText += " and " + englishNumber(fractionUnits) + " cents"
	}
	wordsText += " only"
	return strings.ToUpper(wordsText[:1]) + wordsText[1:]
}

func englishNumber(value int64) string {
	if value == 0 {
		return englishOnes[0]
	}
	scaleNames := []string{"", "thousand", "million", "billion", "trillion"}
	groups := []string{}
	for scaleIndex := 0; value > 0 && scaleIndex < len(scaleNames); scaleIndex++ {
		groupValue := value % 1000
		value /= 1000
		if groupValue == 0 {
			continue
		}
		groupText := englishUnderThousand(groupValue)
		if scaleNames[scaleIndex] != "" {
			groupText += " " + scaleNames[scaleIndex]
		}
		groups = append([]string{groupText}, groups...)
	}
	return strings.Join(groups, " ")
}

func englishUnderThousand(value int64) string {
	parts := []string{}
	if value >= 100 {
		parts = append(parts, englishOnes[value/100]+" hundred")
		value %= 100
		if value > 0 {
			parts = append(parts, "and")
		}
	}
	if value >= 20 {
		tensText := englishTens[value/10]
		if value%10 > 0 {
			tensText += "-" + englishOnes[value%10]
		}
		parts = append(parts, tensText)
	} else if value > 0 {
		parts = append(parts, englishOnes[value])
	}
	return strings.Join(parts, " ")
}

func swahiliNumber(value int64) string {
	if value == 0 {
		return swahiliOnes[0]
	}
	segments := []string{}
	if trillions := value / 1_000_000_000_000; trillions > 0 {
		segments = append(segments, "trilioni "+swahiliUnderThousand(trillions))
	}
	if billions := value / 1_000_000_000 % 1000; billions > 0 {
		segments = append(segments, "bilioni "+swahiliUnderThousand(billions))
	}
	if millions := value / 1_000_000 % 1000; millions > 0 {
		segments = append(segments, "milioni "+swahiliUnderThousand(millions))
	}
	thousands := value / 1000 % 1000
	if thousands >= 100 {
		segments = append(segments, "laki "+swahiliUnderHundred(thousands/100))
	}
	if thousands%100 > 0 {
		segments = append(segments, "elfu "+swahiliUnderHundred(thousands%100))
	}
	if remainder := value % 1000; remainder > 0 {
		segments = append(segments, swahiliUnderThousand(remainder))
	}
	if len(segments) == 1 {
		return segments[0]
	}
	return strings.Join(segments[:len(segments)-1], " ") + " na " + segments[len(segments)-1]
}

func swahiliUnderThousand(value int64) string {
	if value < 100 {
		return swahiliUnderHundred(value)
	}
	hundredsText := "mia " + swahiliOnes[value/100]
	remainder := value % 100
	switch {
	case remainder == 0:
		return hundredsText
	case remainder < 10:
		return hundredsText + " na " + swahiliOnes[remainder]
	default:
		return hundredsText + " " + swahiliUnderHundred(remainder)
	}
}

func swahiliUnderHundred(value int64) string {
	if value < 10 {
		return swahiliOnes[value]
	}
	tensText := swahiliTens[value/10]
	if value%10 > 0 {
		tensText += " na " + swahiliOnes[value%10]
	}
	return tensText
}
