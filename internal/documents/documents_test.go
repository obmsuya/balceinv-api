package documents

import (
	"bytes"
	"fmt"
	"image"
	"image/color"
	"image/png"
	"regexp"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/xuri/excelize/v2"
)

func sampleBranding(t *testing.T) Branding {
	t.Helper()
	logoImage := image.NewRGBA(image.Rect(0, 0, 120, 60))
	for x := range 120 {
		for y := range 60 {
			logoImage.Set(x, y, color.RGBA{R: 37, G: 99, B: 235, A: 255})
		}
	}
	logoBuffer := &bytes.Buffer{}
	encodeError := png.Encode(logoBuffer, logoImage)
	if encodeError != nil {
		t.Fatalf("encode logo: %v", encodeError)
	}
	logoPng, logoWidth, logoHeight := LogoAsPng(logoBuffer.Bytes())
	return Branding{
		CompanyName:      "Duka la Mama & Sons",
		LogoPng:          logoPng,
		LogoWidth:        logoWidth,
		LogoHeight:       logoHeight,
		PrimaryColor:     "#2563EB",
		Tin:              "123-456-789",
		Vrn:              "40-012345-A",
		Address:          "Kariakoo, Dar es Salaam",
		Phone:            "+255 712 345 678",
		CurrencyCode:     "TZS",
		CurrencyDecimals: 2,
		Timezone:         "Africa/Dar_es_Salaam",
	}
}

func sampleDocument(language string, rowCount int) Document {
	firstDay := time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC)
	rows := [][]any{}
	for rowIndex := range rowCount {
		rows = append(rows, []any{
			fmt.Sprintf("Jumla ya Makusanyo — bidhaa %d · Kiswahili", rowIndex+1),
			firstDay.AddDate(0, 0, rowIndex%30),
			int64(rowIndex + 1),
			int64(125000000 + rowIndex),
			int64(4600),
		})
	}
	return Document{
		Language:    language,
		Title:       "Jumla ya Makusanyo — TSh 1,250,000 · Kiswahili",
		Subtitle:    PeriodText(language, firstDay, firstDay.AddDate(0, 0, 29)),
		GeneratedBy: "Asha Juma",
		GeneratedAt: time.Date(2026, 9, 30, 11, 5, 0, 0, time.UTC),
		Cards: []Field{
			{Label: "Takings", Value: int64(125000000), Kind: Money},
			{Label: "Sales", Value: int64(42), Kind: Integer},
			{Label: "Margin", Value: int64(4600), Kind: Percent},
		},
		Tables: []Table{{
			Title: "Days",
			Columns: []Column{
				{Title: "Product", Kind: Text},
				{Title: "Date", Kind: Date},
				{Title: "Quantity", Kind: Integer, Sum: true},
				{Title: "Takings", Kind: Money, Sum: true},
				{Title: "Margin", Kind: Percent},
			},
			Rows: rows,
		}},
		Filters: []Field{{Label: "Shops", Value: "All shops"}},
	}
}

func TestExcelIsStyledTraceableAndPrintable(t *testing.T) {
	branding := sampleBranding(t)
	workbookBytes, excelError := Excel(branding, sampleDocument(Swahili, 3))
	if excelError != nil {
		t.Fatalf("excel: %v", excelError)
	}
	workbook, openError := excelize.OpenReader(bytes.NewReader(workbookBytes))
	if openError != nil {
		t.Fatalf("open: %v", openError)
	}
	defer workbook.Close()

	sheetNames := workbook.GetSheetList()
	if strings.Join(sheetNames, "|") != "Muhtasari|Days|Vichujio" {
		t.Fatalf("sheets were %v", sheetNames)
	}

	headerRow := findRow(t, workbook, "Days", "Product")
	headerStyleId, headerStyleError := workbook.GetCellStyle("Days", cellAt(1, headerRow))
	if headerStyleError != nil {
		t.Fatalf("header style: %v", headerStyleError)
	}
	headerStyle, getStyleError := workbook.GetStyle(headerStyleId)
	if getStyleError != nil {
		t.Fatalf("get style: %v", getStyleError)
	}
	if len(headerStyle.Fill.Color) == 0 || !strings.EqualFold(headerStyle.Fill.Color[0], "2563EB") || headerStyle.Font == nil || !headerStyle.Font.Bold {
		t.Fatalf("header style was %+v %+v", headerStyle.Fill, headerStyle.Font)
	}

	panes, panesError := workbook.GetPanes("Days")
	if panesError != nil || !panes.Freeze || panes.YSplit != headerRow {
		t.Fatalf("panes were %+v %v", panes, panesError)
	}

	firstData := headerRow + 1
	moneyCell := cellAt(4, firstData)
	moneyType, typeError := workbook.GetCellType("Days", moneyCell)
	if typeError != nil || moneyType == excelize.CellTypeSharedString || moneyType == excelize.CellTypeInlineString {
		t.Fatalf("money cell type %v %v", moneyType, typeError)
	}
	rawMoney, rawError := workbook.GetCellValue("Days", moneyCell, excelize.Options{RawCellValue: true})
	moneyNumber, parseError := strconv.ParseFloat(rawMoney, 64)
	if rawError != nil || parseError != nil || moneyNumber != 1250000 {
		t.Fatalf("money cell held %q (%v %v)", rawMoney, rawError, parseError)
	}
	dateCell := cellAt(2, firstData)
	rawDate, dateError := workbook.GetCellValue("Days", dateCell, excelize.Options{RawCellValue: true})
	dateSerial, dateParseError := strconv.ParseFloat(rawDate, 64)
	if dateError != nil || dateParseError != nil || dateSerial < 46000 {
		t.Fatalf("date cell held %q, want an Excel date number", rawDate)
	}

	totalsRow := firstData + 3
	for _, totalsColumn := range []int{3, 4} {
		totalsFormula, formulaError := workbook.GetCellFormula("Days", cellAt(totalsColumn, totalsRow))
		columnName, _ := excelize.ColumnNumberToName(totalsColumn)
		wantFormula := fmt.Sprintf("SUM(%s%d:%s%d)", columnName, firstData, columnName, firstData+2)
		if formulaError != nil || strings.TrimPrefix(totalsFormula, "=") != wantFormula {
			t.Fatalf("totals formula in column %d was %q, want %q", totalsColumn, totalsFormula, wantFormula)
		}
	}
	totalLabel, _ := workbook.GetCellValue("Days", cellAt(1, totalsRow))
	if totalLabel != "Jumla" {
		t.Fatalf("totals label was %q", totalLabel)
	}
	marginFormula, _ := workbook.GetCellFormula("Days", cellAt(5, totalsRow))
	if marginFormula != "" {
		t.Fatalf("a percent column was summed: %q", marginFormula)
	}

	pageLayout, layoutError := workbook.GetPageLayout("Days")
	if layoutError != nil || pageLayout.Size == nil || *pageLayout.Size != 9 || pageLayout.FitToWidth == nil || *pageLayout.FitToWidth != 1 {
		t.Fatalf("page layout was %+v %v", pageLayout, layoutError)
	}
	footer, footerError := workbook.GetHeaderFooter("Days")
	if footerError != nil || !strings.Contains(footer.OddFooter, "Ukurasa &P kati ya &N") || !strings.Contains(footer.OddFooter, "Mama && Sons") {
		t.Fatalf("footer was %+v", footer)
	}
	printTitles := false
	for _, definedName := range workbook.GetDefinedName() {
		printTitles = printTitles || (definedName.Name == "_xlnm.Print_Titles" && definedName.Scope == "Days")
	}
	if !printTitles {
		t.Fatalf("the header row does not repeat when printed: %+v", workbook.GetDefinedName())
	}

	properties, propertiesError := workbook.GetDocProps()
	if propertiesError != nil || properties.Creator != "Balce" || properties.Title == "" {
		t.Fatalf("properties were %+v %v", properties, propertiesError)
	}
	appProperties, appError := workbook.GetAppProps()
	if appError != nil || appProperties.Company != "Duka la Mama & Sons" {
		t.Fatalf("app properties were %+v %v", appProperties, appError)
	}

	pictures, picturesError := workbook.GetPictures("Days", cellAt(5, 1))
	if picturesError != nil || len(pictures) != 1 {
		t.Fatalf("logo pictures were %d %v", len(pictures), picturesError)
	}

	filterRow := findRow(t, workbook, "Vichujio", "Kichujio")
	filterValue, _ := workbook.GetCellValue("Vichujio", cellAt(2, filterRow+1))
	if filterValue != "All shops" {
		t.Fatalf("filters sheet held %q", filterValue)
	}
	summaryRow := findRow(t, workbook, "Muhtasari", "Kipengele")
	cardMoney, _ := workbook.GetCellValue("Muhtasari", cellAt(2, summaryRow+1), excelize.Options{RawCellValue: true})
	if cardMoney != "1250000" {
		t.Fatalf("summary money held %q", cardMoney)
	}
}

func TestExcelWithoutLogoOrRowsStillOpens(t *testing.T) {
	branding := sampleBranding(t)
	branding.LogoPng = nil
	emptyDocument := sampleDocument(English, 0)
	emptyDocument.Cards = nil
	emptyDocument.Filters = nil
	workbookBytes, excelError := Excel(branding, emptyDocument)
	if excelError != nil {
		t.Fatalf("excel: %v", excelError)
	}
	workbook, openError := excelize.OpenReader(bytes.NewReader(workbookBytes))
	if openError != nil {
		t.Fatalf("open: %v", openError)
	}
	defer workbook.Close()
	headerRow := findRow(t, workbook, "Days", "Product")
	emptyText, _ := workbook.GetCellValue("Days", cellAt(1, headerRow+1))
	if emptyText != "Nothing to show" {
		t.Fatalf("empty table said %q", emptyText)
	}
}

func TestPdfRepeatsTheTableHeaderOnEveryPage(t *testing.T) {
	branding := sampleBranding(t)
	writer, writerError := newPdfWriter(branding, sampleDocument(Swahili, 200))
	if writerError != nil {
		t.Fatalf("writer: %v", writerError)
	}
	pdfBytes, renderError := writer.render()
	if renderError != nil {
		t.Fatalf("render: %v", renderError)
	}
	if !bytes.HasPrefix(pdfBytes, []byte("%PDF")) {
		t.Fatal("the PDF does not start with %PDF")
	}
	pageCount := countPages(pdfBytes)
	if pageCount < 3 || pageCount != writer.pageCount {
		t.Fatalf("the PDF has %d pages but the layout planned %d", pageCount, writer.pageCount)
	}
	pagesWithHeader := map[int]bool{}
	for _, headerPage := range writer.tableHeaderPages {
		pagesWithHeader[headerPage] = true
	}
	firstTablePage := writer.tableHeaderPages[0]
	for pageNumber := firstTablePage; pageNumber <= writer.pageCount; pageNumber++ {
		if !pagesWithHeader[pageNumber] {
			t.Fatalf("page %d has table rows but no header: %v", pageNumber, writer.tableHeaderPages)
		}
	}
	if len(writer.tableHeaderPages) != writer.pageCount-firstTablePage+1 {
		t.Fatalf("the header was placed %v on %d pages", writer.tableHeaderPages, writer.pageCount)
	}
	if !bytes.Contains(pdfBytes, []byte("/FontFile2")) {
		t.Fatal("the UTF-8 font was not embedded")
	}
}

func TestPdfInvoiceShapeRendersWithoutLogo(t *testing.T) {
	branding := sampleBranding(t)
	branding.LogoPng = nil
	invoice := Document{
		Language:  English,
		Title:     "Invoice",
		Number:    "MAIN-20260930-0001",
		Details:   []Field{{Label: "Date", Value: time.Now(), Kind: DateTime}, {Label: "Served by", Value: "Asha"}},
		Tables:    []Table{{Columns: []Column{{Title: "Item", Kind: Text}, {Title: "Qty", Kind: Integer}, {Title: "Unit price", Kind: Money}, {Title: "Amount", Kind: Money}}, Rows: [][]any{{"Soda", int64(2), int64(118000), int64(236000)}}}},
		Totals:    []Field{{Label: "Total", Value: int64(236000), Kind: Money, Strong: true}},
		QrCode:    "https://verify.tra.go.tz/ABC123",
		QrCaption: "EFD verification\nABC123",
		Notes:     []string{"Thank you\nKaribu tena"},
	}
	pdfBytes, pdfError := Pdf(branding, invoice)
	if pdfError != nil || !bytes.HasPrefix(pdfBytes, []byte("%PDF")) || countPages(pdfBytes) != 1 {
		t.Fatalf("invoice PDF failed: %v (%d pages)", pdfError, countPages(pdfBytes))
	}
}

func TestMoneyAndLanguageHelpers(t *testing.T) {
	cases := map[string]string{
		FormatMoney(125000000, 2): "1,250,000.00",
		FormatMoney(-1250, 0):     "-1,250",
		FormatMoney(5, 2):         "0.05",
		FormatMoney(999, 0):       "999",
		FormatPercent(4600):       "46.0%",
		ResolveLanguage("", "SW"): "sw",
		ResolveLanguage("fr"):     "en",
		PeriodText(Swahili, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), time.Date(2026, 8, 31, 0, 0, 0, 0, time.UTC)): "1 Ago 2026 – 31 Ago 2026",
		SafeFileName("invoice-MAIN/2026 09"): "invoice-MAIN-2026-09",
	}
	for got, want := range cases {
		if got != want {
			t.Fatalf("got %q, want %q", got, want)
		}
	}
}

func findRow(t *testing.T, workbook *excelize.File, sheetName string, firstCellText string) int {
	t.Helper()
	sheetRows, rowsError := workbook.GetRows(sheetName)
	if rowsError != nil {
		t.Fatalf("rows of %s: %v", sheetName, rowsError)
	}
	for rowIndex, rowCells := range sheetRows {
		if len(rowCells) > 0 && rowCells[0] == firstCellText {
			return rowIndex + 1
		}
	}
	t.Fatalf("no row in %s starts with %q", sheetName, firstCellText)
	return 0
}

var pageObjectPattern = regexp.MustCompile(`/Type\s*/Page[^s]`)

func countPages(pdfBytes []byte) int {
	return len(pageObjectPattern.FindAll(pdfBytes, -1))
}
