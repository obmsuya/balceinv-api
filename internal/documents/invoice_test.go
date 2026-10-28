package documents

import (
	"bytes"
	"fmt"
	"strings"
	"testing"
	"time"
)

func TestAmountInWordsReadsLikeAPerson(t *testing.T) {
	shillings := Branding{CurrencyCode: "TZS", CurrencyDecimals: 0}
	cases := []struct {
		language string
		amount   int64
		wanted   string
	}{
		{English, 0, "Zero shillings only"},
		{English, 3500, "Three thousand five hundred shillings only"},
		{English, 926200, "Nine hundred and twenty-six thousand two hundred shillings only"},
		{English, 1250000, "One million two hundred and fifty thousand shillings only"},
		{English, 1000000000, "One billion shillings only"},
		{Swahili, 21, "Shilingi ishirini na moja tu"},
		{Swahili, 105, "Shilingi mia moja na tano tu"},
		{Swahili, 3500, "Shilingi elfu tatu na mia tano tu"},
		{Swahili, 95000, "Shilingi elfu tisini na tano tu"},
		{Swahili, 1250000, "Shilingi milioni moja laki mbili na elfu hamsini tu"},
		{Swahili, 200000, "Shilingi laki mbili tu"},
	}
	for _, testCase := range cases {
		got := AmountInWords(testCase.language, testCase.amount, shillings)
		if got != testCase.wanted {
			t.Fatalf("%s %d read %q, want %q", testCase.language, testCase.amount, got, testCase.wanted)
		}
	}

	dollars := Branding{CurrencyCode: "USD", CurrencyDecimals: 2}
	if got := AmountInWords(English, 1050, dollars); got != "Ten USD and fifty cents only" {
		t.Fatalf("dollars read %q", got)
	}
	if got := AmountInWords(Swahili, 1050, dollars); got != "USD kumi na senti hamsini tu" {
		t.Fatalf("Swahili dollars read %q", got)
	}
}

func TestWrapTextKeepsWordsAndBreaksLongOnes(t *testing.T) {
	wrapped := wrapText("Rice 25kg bag Mbeya super grade long grain", 30, 8.5, pdfAverageCharEm)
	if len(wrapped) < 2 || strings.Join(wrapped, " ") != "Rice 25kg bag Mbeya super grade long grain" {
		t.Fatalf("wrapped into %q", wrapped)
	}
	for _, line := range wrapText(strings.Repeat("x", 80), 30, 8.5, pdfAverageCharEm) {
		if len(line) > 30 {
			t.Fatalf("a long word was not broken: %q", line)
		}
	}
	if wrapText("   ", 30, 8.5, pdfAverageCharEm) != nil {
		t.Fatal("blank text made lines")
	}
}

func sampleInvoice(lineCount int) InvoiceDocument {
	lines := []InvoiceLine{{Description: "Rice 25kg bag, Mbeya super grade, long grain, delivered before 10 am to the Kariakoo store", Detail: "SKU RICE-25", Quantity: 6, Unit: "bag", UnitPrice: 72000, Discount: 12000, Amount: 420000}}
	for lineIndex := range lineCount {
		lines = append(lines, InvoiceLine{Description: fmt.Sprintf("Soap bar %d", lineIndex+1), Quantity: 2, Unit: "pcs", UnitPrice: 1000, Amount: 2000})
	}
	return InvoiceDocument{
		Language:       English,
		Title:          "Invoice",
		Number:         "INV-2026-0001",
		Status:         "Part paid",
		StatusTone:     ToneWarning,
		Party:          InvoiceParty{Heading: "Bill to", Name: "Juma Traders", Lines: []string{"0712 000 111", "Mbezi Beach"}},
		Facts:          []Field{{Label: "Due date", Value: time.Date(2026, 11, 27, 0, 0, 0, 0, time.UTC), Kind: Date, Strong: true}},
		Lines:          lines,
		Totals:         []Field{{Label: "Total", Value: int64(420000), Kind: Money}, {Label: "Balance due", Value: int64(20000), Kind: Money, Strong: true}},
		AmountInWords:  "Four hundred and twenty thousand shillings only",
		PaymentDetails: "CRDB 0150 1234\nLipa 5566778",
		Terms:          "Pay within 30 days",
		Notes:          []string{"Thank you"},
		Signatures:     []string{"Prepared by", "Received by"},
		GeneratedBy:    "Asha",
		GeneratedAt:    time.Date(2026, 10, 28, 9, 0, 0, 0, time.UTC),
	}
}

func TestInvoicePdfCarriesEverySectionAndPagesLongLists(t *testing.T) {
	branding := sampleBranding(t)
	shortPdf, shortError := InvoicePdf(branding, sampleInvoice(3))
	if shortError != nil || !bytes.HasPrefix(shortPdf, []byte("%PDF")) || countPages(shortPdf) != 1 {
		t.Fatalf("a short invoice failed: %v (%d pages)", shortError, countPages(shortPdf))
	}

	branding.LogoPng = nil
	longPdf, longError := InvoicePdf(branding, sampleInvoice(80))
	if longError != nil || countPages(longPdf) < 2 {
		t.Fatalf("a long invoice failed: %v (%d pages)", longError, countPages(longPdf))
	}

	deliveryNote := sampleInvoice(3)
	deliveryNote.Language = Swahili
	deliveryNote.HidePrices = true
	deliveryNote.Totals = nil
	deliveryNote.Status = ""
	notePdf, noteError := InvoicePdf(branding, deliveryNote)
	if noteError != nil || countPages(notePdf) != 1 {
		t.Fatalf("a delivery note failed: %v", noteError)
	}

	empty := sampleInvoice(0)
	empty.Lines = nil
	_, emptyError := InvoicePdf(branding, empty)
	if emptyError != nil {
		t.Fatalf("an invoice with no lines failed: %v", emptyError)
	}
}

func TestRenderInvoiceNamesTheFile(t *testing.T) {
	invoiceFile, renderError := RenderInvoice(sampleBranding(t), sampleInvoice(1), "invoice-INV/2026 0001")
	if renderError != nil || invoiceFile.Name != "invoice-INV-2026-0001.pdf" || invoiceFile.ContentType != PdfContentType {
		t.Fatalf("render gave %q %q %v", invoiceFile.Name, invoiceFile.ContentType, renderError)
	}
}
