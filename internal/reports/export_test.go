package reports_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"strconv"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"
)

func logoPng(t *testing.T) []byte {
	t.Helper()
	logoImage := image.NewRGBA(image.Rect(0, 0, 40, 20))
	logoImage.Set(3, 3, color.RGBA{R: 200, A: 255})
	logoBuffer := &bytes.Buffer{}
	encodeError := png.Encode(logoBuffer, logoImage)
	if encodeError != nil {
		t.Fatalf("encode logo: %v", encodeError)
	}
	return logoBuffer.Bytes()
}

func openWorkbook(t *testing.T, exported apptest.Response) *excelize.File {
	t.Helper()
	if exported.Status != http.StatusOK {
		t.Fatalf("export returned %d %v", exported.Status, exported.Body)
	}
	if exported.Headers.Get("Content-Type") != "application/vnd.openxmlformats-officedocument.spreadsheetml.sheet" {
		t.Fatalf("content type was %q", exported.Headers.Get("Content-Type"))
	}
	workbook, openError := excelize.OpenReader(bytes.NewReader(exported.Raw))
	if openError != nil {
		t.Fatalf("the workbook does not open: %v", openError)
	}
	t.Cleanup(func() { workbook.Close() })
	return workbook
}

func rowStartingWith(t *testing.T, workbook *excelize.File, sheetName string, firstCell string) int {
	t.Helper()
	sheetRows, rowsError := workbook.GetRows(sheetName)
	if rowsError != nil {
		t.Fatalf("rows of %s: %v", sheetName, rowsError)
	}
	for rowIndex, rowCells := range sheetRows {
		if len(rowCells) > 0 && rowCells[0] == firstCell {
			return rowIndex + 1
		}
	}
	t.Fatalf("no row in %s starts with %q: %v", sheetName, firstCell, sheetRows)
	return 0
}

func containsUtf16(pdfBytes []byte, wanted string) bool {
	encoded := []byte{}
	for _, unit := range utf16.Encode([]rune(wanted)) {
		encoded = append(encoded, byte(unit>>8), byte(unit))
	}
	return bytes.Contains(pdfBytes, encoded)
}

func TestReportExportsAreBrandedAndTraceable(t *testing.T) {
	for modeName, startHarness := range map[string]func(*testing.T, testkit.EngineCase) *apptest.Harness{"cloud": apptest.Start, "desktop": apptest.StartDesktop} {
		t.Run(modeName, func(t *testing.T) {
			testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
				checkReportExports(t, startHarness(t, engineCase))
			})
		})
	}
}

func checkReportExports(t *testing.T, harness *apptest.Harness) {
	company := harness.CreateCompany("Export Shop", "owner@export.test")
	otherCompany := harness.CreateCompany("Other Export Shop", "owner@otherexport.test")

	logoUpload := harness.Upload("/api/settings/upload-logo", company.OwnerToken, "file", "logo.png", logoPng(t))
	if logoUpload.Status != http.StatusOK {
		t.Fatalf("logo upload returned %d %v", logoUpload.Status, logoUpload.Body)
	}
	settingsSaved := harness.Call(http.MethodPut, "/api/settings", company.OwnerToken, map[string]any{"business_tin": "123-456-789", "business_phone": "+255 700 000 000"})
	if settingsSaved.Status != http.StatusOK {
		t.Fatalf("settings returned %d %v", settingsSaved.Status, settingsSaved.Body)
	}
	featuresSaved := harness.Call(http.MethodPut, "/api/features", company.OwnerToken, map[string]any{"vat_registered": true, "vat_number": "40-012345-A"})
	if featuresSaved.Status != http.StatusOK {
		t.Fatalf("features returned %d %v", featuresSaved.Status, featuresSaved.Body)
	}

	sodaId := newProduct(t, harness, company.OwnerToken, map[string]any{"sku": "SODA", "name": "Soda", "price": 1180, "cost_price": 600, "opening_quantity": 100})
	coffeeId := newProduct(t, harness, company.OwnerToken, map[string]any{"sku": "COFFEE", "name": "Coffee", "price": 2360, "cost_price": 900, "opening_quantity": 100})
	newProduct(t, harness, company.OwnerToken, map[string]any{"sku": "IDLE", "name": "Idle", "price": 500, "cost_price": 200, "opening_quantity": 7})
	sell(t, harness, company.OwnerToken, "export-sale-1", sodaId, 2, cash(5000))
	sell(t, harness, company.OwnerToken, "export-sale-2", coffeeId, 1, []map[string]any{{"method": "card", "amount": 2360}})
	otherSodaId := newProduct(t, harness, otherCompany.OwnerToken, map[string]any{"sku": "SODA", "name": "Other Soda", "price": 99990, "cost_price": 1, "opening_quantity": 10})
	sell(t, harness, otherCompany.OwnerToken, "other-export-sale", otherSodaId, 1, cash(99990))

	rangeQuery := "from=" + today() + "&to=" + today()
	fullReport := harness.Call(http.MethodGet, "/api/reports/summary/export?"+rangeQuery+"&format=xlsx&shop=all", company.OwnerToken, nil)
	workbook := openWorkbook(t, fullReport)
	wantDisposition := `attachment; filename="report-` + today() + `-to-` + today() + `.xlsx"`
	if fullReport.Headers.Get("Content-Disposition") != wantDisposition {
		t.Fatalf("disposition was %q, want %q", fullReport.Headers.Get("Content-Disposition"), wantDisposition)
	}
	if strings.Join(workbook.GetSheetList(), "|") != "Summary|Days|Products|Staff|Shops|Not selling|Filters" {
		t.Fatalf("sheets were %v", workbook.GetSheetList())
	}

	companyName, _ := workbook.GetCellValue("Days", "A1")
	taxLine, _ := workbook.GetCellValue("Days", "A2")
	if companyName != "Export Shop" || !strings.Contains(taxLine, "123-456-789") || !strings.Contains(taxLine, "VRN: 40-012345-A") {
		t.Fatalf("the letterhead was %q / %q", companyName, taxLine)
	}
	logoPictures, picturesError := workbook.GetPictures("Days", "G1")
	if picturesError != nil || len(logoPictures) != 1 {
		t.Fatalf("the logo was not placed: %d %v", len(logoPictures), picturesError)
	}

	headerRow := rowStartingWith(t, workbook, "Days", "Date")
	headerStyleId, _ := workbook.GetCellStyle("Days", "A"+strconv.Itoa(headerRow))
	headerStyle, styleError := workbook.GetStyle(headerStyleId)
	if styleError != nil || len(headerStyle.Fill.Color) == 0 || !strings.EqualFold(headerStyle.Fill.Color[0], "5EA500") {
		t.Fatalf("the header is not in the brand colour: %+v %v", headerStyle.Fill, styleError)
	}
	panes, panesError := workbook.GetPanes("Days")
	if panesError != nil || !panes.Freeze || panes.YSplit != headerRow {
		t.Fatalf("panes were %+v", panes)
	}
	dayRow := strconv.Itoa(headerRow + 1)
	rawTakings, _ := workbook.GetCellValue("Days", "C"+dayRow, excelize.Options{RawCellValue: true})
	if rawTakings != "4720" {
		t.Fatalf("the day's takings cell held %q, want the number 4720", rawTakings)
	}
	takingsType, _ := workbook.GetCellType("Days", "C"+dayRow)
	if takingsType == excelize.CellTypeSharedString || takingsType == excelize.CellTypeInlineString {
		t.Fatal("money was written as text")
	}
	totalsRow := strconv.Itoa(headerRow + 2)
	takingsFormula, _ := workbook.GetCellFormula("Days", "C"+totalsRow)
	if strings.TrimPrefix(takingsFormula, "=") != "SUM(C"+dayRow+":C"+dayRow+")" {
		t.Fatalf("the takings total was %q", takingsFormula)
	}

	productHeader := rowStartingWith(t, workbook, "Products", "Product")
	productCells, _ := workbook.GetRows("Products")
	firstProduct := productCells[productHeader]
	if firstProduct[0] != "Soda" && firstProduct[0] != "Coffee" {
		t.Fatalf("products were %v", productCells[productHeader:])
	}
	for _, sheetRow := range productCells {
		for _, cellText := range sheetRow {
			if strings.Contains(cellText, "Other Soda") {
				t.Fatal("another company's product leaked into the export")
			}
		}
	}

	summaryHeader := rowStartingWith(t, workbook, "Summary", "Item")
	takingsLabel, _ := workbook.GetCellValue("Summary", "A"+strconv.Itoa(summaryHeader+1))
	summaryTakings, _ := workbook.GetCellValue("Summary", "B"+strconv.Itoa(summaryHeader+1), excelize.Options{RawCellValue: true})
	if takingsLabel != "Takings (incl. tax) (TZS)" || summaryTakings != "4720" {
		t.Fatalf("the summary started %q = %q", takingsLabel, summaryTakings)
	}
	filterHeader := rowStartingWith(t, workbook, "Filters", "Filter")
	shopsFilter, _ := workbook.GetCellValue("Filters", "B"+strconv.Itoa(filterHeader+2))
	if shopsFilter != "All shops" {
		t.Fatalf("the shops filter said %q", shopsFilter)
	}

	swahiliReport := harness.Call(http.MethodGet, "/api/reports/summary/export?"+rangeQuery+"&format=xlsx&lang=sw", company.OwnerToken, nil)
	swahiliWorkbook := openWorkbook(t, swahiliReport)
	if strings.Join(swahiliWorkbook.GetSheetList(), "|") != "Muhtasari|Siku|Bidhaa|Wafanyakazi|Maduka|Bidhaa zisizouzwa|Vichujio" {
		t.Fatalf("Swahili sheets were %v", swahiliWorkbook.GetSheetList())
	}
	swahiliTotal, _ := swahiliWorkbook.GetCellValue("Siku", "A"+strconv.Itoa(rowStartingWith(t, swahiliWorkbook, "Siku", "Tarehe")+2))
	if swahiliTotal != "Jumla" {
		t.Fatalf("the Swahili totals label was %q", swahiliTotal)
	}

	languageSaved := harness.Call(http.MethodPut, "/api/auth/language", company.OwnerToken, map[string]any{"locale": "sw"})
	if languageSaved.Status != http.StatusOK {
		t.Fatalf("language returned %d", languageSaved.Status)
	}
	usersLanguage := openWorkbook(t, harness.Call(http.MethodGet, "/api/reports/daily/export?"+rangeQuery, company.OwnerToken, nil))
	if usersLanguage.GetSheetList()[1] != "Siku" {
		t.Fatalf("the export ignored the user's language: %v", usersLanguage.GetSheetList())
	}
	englishOverride := openWorkbook(t, harness.Call(http.MethodGet, "/api/reports/daily/export?"+rangeQuery+"&lang=en", company.OwnerToken, nil))
	if englishOverride.GetSheetList()[1] != "Days" {
		t.Fatalf("lang=en was ignored: %v", englishOverride.GetSheetList())
	}

	for _, reportName := range []string{"summary", "daily", "products", "cashiers", "shops", "inventory"} {
		for _, exportFormat := range []string{"xlsx", "pdf"} {
			exported := harness.Call(http.MethodGet, "/api/reports/"+reportName+"/export?"+rangeQuery+"&format="+exportFormat, company.OwnerToken, nil)
			if exported.Status != http.StatusOK || !strings.Contains(exported.Headers.Get("Content-Disposition"), "."+exportFormat+`"`) {
				t.Fatalf("%s as %s returned %d %v", reportName, exportFormat, exported.Status, exported.Body)
			}
			if exportFormat == "pdf" && (!bytes.HasPrefix(exported.Raw, []byte("%PDF")) || exported.Headers.Get("Content-Type") != "application/pdf") {
				t.Fatalf("%s PDF was not a PDF", reportName)
			}
		}
	}
	swahiliPdf := harness.Call(http.MethodGet, "/api/reports/summary/export?"+rangeQuery+"&format=pdf", company.OwnerToken, nil)
	if !containsUtf16(swahiliPdf.Raw, "Ripoti ya mauzo") {
		t.Fatal("the PDF title is not in the user's Swahili")
	}
	stockExport := harness.Call(http.MethodGet, "/api/reports/inventory/export?format=pdf&lang=en", company.OwnerToken, nil)
	if !strings.Contains(stockExport.Headers.Get("Content-Disposition"), "stock-report-"+today()+".pdf") || !containsUtf16(stockExport.Raw, "Stock report") {
		t.Fatalf("the stock export was %q", stockExport.Headers.Get("Content-Disposition"))
	}

	if refused := harness.Call(http.MethodGet, "/api/reports/summary/export?format=csv", company.OwnerToken, nil); refused.Status != http.StatusBadRequest || refused.Code() != "invalid_format" {
		t.Fatalf("csv returned %d %v", refused.Status, refused.Body)
	}
	if unknown := harness.Call(http.MethodGet, "/api/reports/payroll/export", company.OwnerToken, nil); unknown.Status != http.StatusNotFound {
		t.Fatalf("an unknown report returned %d", unknown.Status)
	}
	if backwards := harness.Call(http.MethodGet, "/api/reports/daily/export?from=2026-09-30&to=2026-09-01", company.OwnerToken, nil); backwards.Status != http.StatusBadRequest || backwards.Code() != "invalid_filter" {
		t.Fatalf("a backwards range returned %d %v", backwards.Status, backwards.Body)
	}
	if badSort := harness.Call(http.MethodGet, "/api/reports/products/export?sort=loudest", company.OwnerToken, nil); badSort.Status != http.StatusBadRequest {
		t.Fatalf("an unknown sort returned %d", badSort.Status)
	}
	if foreignShop := harness.Call(http.MethodGet, "/api/reports/daily/export?shop="+otherCompany.ShopId.String(), company.OwnerToken, nil); foreignShop.Status != http.StatusNotFound {
		t.Fatalf("another company's shop returned %d", foreignShop.Status)
	}

	cashierToken := harness.CreateStaff(company, "cashier@export.test", []string{"sales:create"}, []uuid.UUID{company.ShopId})
	if refused := harness.Call(http.MethodGet, "/api/reports/summary/export", cashierToken, nil); refused.Status != http.StatusForbidden {
		t.Fatalf("a cashier without reports:view got %d", refused.Status)
	}
	if anonymous := harness.Call(http.MethodGet, "/api/reports/summary/export", "", nil); anonymous.Status != http.StatusUnauthorized {
		t.Fatalf("an anonymous export returned %d", anonymous.Status)
	}
}
