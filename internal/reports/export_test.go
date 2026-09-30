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

func valueBeside(t *testing.T, workbook *excelize.File, firstCell string, columnName string) string {
	t.Helper()
	sheetName := workbook.GetSheetName(0)
	calculated, calcError := workbook.CalcCellValue(sheetName, columnName+strconv.Itoa(rowStartingWith(t, workbook, sheetName, firstCell)), excelize.Options{RawCellValue: true})
	if calcError != nil {
		t.Fatalf("calculating beside %q: %v", firstCell, calcError)
	}
	return calculated
}

func sheetText(workbook *excelize.File) string {
	sheetRows, _ := workbook.GetRows(workbook.GetSheetName(0))
	allText := strings.Builder{}
	for _, sheetRow := range sheetRows {
		allText.WriteString(strings.Join(sheetRow, " | ") + "\n")
	}
	return allText.String()
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
	summary := harness.Call(http.MethodGet, "/api/reports/summary?"+rangeQuery+"&shop=all", company.OwnerToken, nil).Data()
	salesReport := harness.Call(http.MethodGet, "/api/reports/summary/export?"+rangeQuery+"&format=xlsx&shop=all", company.OwnerToken, nil)
	salesBook := openWorkbook(t, salesReport)
	if salesReport.Headers.Get("Content-Disposition") != `attachment; filename="sales-report-`+today()+`-to-`+today()+`.xlsx"` {
		t.Fatalf("disposition was %q", salesReport.Headers.Get("Content-Disposition"))
	}
	companyName, _ := salesBook.GetCellValue(salesBook.GetSheetName(0), "A1")
	taxLine, _ := salesBook.GetCellValue(salesBook.GetSheetName(0), "A2")
	if companyName != "Export Shop" || !strings.Contains(taxLine, "123-456-789") || !strings.Contains(taxLine, "VRN: 40-012345-A") {
		t.Fatalf("the letterhead was %q / %q", companyName, taxLine)
	}
	logoPictures, picturesError := salesBook.GetPictures(salesBook.GetSheetName(0), "E1")
	if picturesError != nil || len(logoPictures) != 1 {
		t.Fatalf("the logo was not placed: %d %v", len(logoPictures), picturesError)
	}
	salesText := sheetText(salesBook)
	for _, expected := range []string{"Sales report", "All shops", "Compared with", "Takings, including VAT", "HOW CUSTOMERS PAID", "Prepared by"} {
		if !strings.Contains(salesText, expected) {
			t.Errorf("the sales report does not show %q", expected)
		}
	}
	wantGross := strconv.FormatFloat(summary["gross_profit"].(float64), 'f', 0, 64)
	sheetRows, _ := salesBook.GetRows(salesBook.GetSheetName(0))
	for rowIndex, sheetRow := range sheetRows {
		if len(sheetRow) > 1 && sheetRow[1] == "Gross profit" {
			calculated, _ := salesBook.CalcCellValue(salesBook.GetSheetName(0), "C"+strconv.Itoa(rowIndex+1), excelize.Options{RawCellValue: true})
			formula, _ := salesBook.GetCellFormula(salesBook.GetSheetName(0), "C"+strconv.Itoa(rowIndex+1))
			if calculated != wantGross || formula == "" {
				t.Fatalf("gross profit calculates to %q with formula %q, want %s", calculated, formula, wantGross)
			}
		}
	}
	if strings.Contains(salesText, "Other Soda") || strings.Contains(salesText, "99,990") {
		t.Fatal("another company's sales leaked into the report")
	}

	daily := openWorkbook(t, harness.Call(http.MethodGet, "/api/reports/daily/export?"+rangeQuery+"&format=xlsx", company.OwnerToken, nil))
	if valueBeside(t, daily, "Total", "C") != "4720" || valueBeside(t, daily, "Total", "B") != "2" {
		t.Fatalf("the daily totals were %s takings from %s sales", valueBeside(t, daily, "Total", "C"), valueBeside(t, daily, "Total", "B"))
	}
	headerRow := rowStartingWith(t, daily, daily.GetSheetName(0), "Date")
	panes, panesError := daily.GetPanes(daily.GetSheetName(0))
	if panesError != nil || !panes.Freeze || panes.YSplit != headerRow {
		t.Fatalf("panes were %+v", panes)
	}
	takingsType, _ := daily.GetCellType(daily.GetSheetName(0), "C"+strconv.Itoa(headerRow+1))
	if takingsType == excelize.CellTypeSharedString || takingsType == excelize.CellTypeInlineString {
		t.Fatal("money was written as text")
	}

	products := openWorkbook(t, harness.Call(http.MethodGet, "/api/reports/products/export?"+rangeQuery+"&format=xlsx&sort=profit", company.OwnerToken, nil))
	productsText := sheetText(products)
	if !strings.Contains(productsText, "Soda") || !strings.Contains(productsText, "Coffee") || !strings.Contains(productsText, "Ranked by: Profit") || strings.Contains(productsText, "Other Soda") {
		t.Fatalf("the products report was:\n%s", productsText)
	}

	stock := openWorkbook(t, harness.Call(http.MethodGet, "/api/reports/inventory/export?format=xlsx", company.OwnerToken, nil))
	stockValue := strconv.FormatInt(98*600+99*900+7*200, 10)
	if valueBeside(t, stock, "Total", "F") != stockValue {
		t.Fatalf("stock at cost totals %s, want %s", valueBeside(t, stock, "Total", "F"), stockValue)
	}
	notSelling := openWorkbook(t, harness.Call(http.MethodGet, "/api/reports/dead-stock/export?format=xlsx", company.OwnerToken, nil))
	if !strings.Contains(sheetText(notSelling), "Idle") || !strings.Contains(sheetText(notSelling), "Never sold") {
		t.Fatalf("the not-selling report was:\n%s", sheetText(notSelling))
	}

	swahili := openWorkbook(t, harness.Call(http.MethodGet, "/api/reports/daily/export?"+rangeQuery+"&format=xlsx&lang=sw", company.OwnerToken, nil))
	if swahili.GetSheetName(0) != "Mauzo kwa siku" || rowStartingWith(t, swahili, swahili.GetSheetName(0), "Jumla") == 0 {
		t.Fatalf("the Swahili sheet was %q", swahili.GetSheetName(0))
	}
	languageSaved := harness.Call(http.MethodPut, "/api/auth/language", company.OwnerToken, map[string]any{"locale": "sw"})
	if languageSaved.Status != http.StatusOK {
		t.Fatalf("language returned %d", languageSaved.Status)
	}
	if usersLanguage := openWorkbook(t, harness.Call(http.MethodGet, "/api/reports/daily/export?"+rangeQuery, company.OwnerToken, nil)); usersLanguage.GetSheetName(0) != "Mauzo kwa siku" {
		t.Fatalf("the export ignored the user's language: %q", usersLanguage.GetSheetName(0))
	}
	if englishOverride := openWorkbook(t, harness.Call(http.MethodGet, "/api/reports/daily/export?"+rangeQuery+"&lang=en", company.OwnerToken, nil)); englishOverride.GetSheetName(0) != "Sales per day" {
		t.Fatalf("lang=en was ignored: %q", englishOverride.GetSheetName(0))
	}

	for _, reportName := range []string{"summary", "daily", "products", "cashiers", "shops", "inventory", "dead-stock"} {
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
	if !strings.Contains(stockExport.Headers.Get("Content-Disposition"), "stock-on-hand-"+today()+".pdf") || !containsUtf16(stockExport.Raw, "Stock on hand") {
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
