package suppliers_test

import (
	"bytes"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/xuri/excelize/v2"
)

func TestSupplierStatementExportsWhatWeOwe(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Supplier Statement Duka", "owner@supstatement.test")
		otherCompany := harness.CreateCompany("Other Supplier Duka", "owner@othersupstatement.test")
		switchOn(t, harness, company.OwnerToken, map[string]any{"suppliers_enabled": true})
		switchOn(t, harness, otherCompany.OwnerToken, map[string]any{"suppliers_enabled": true})
		riceId := newProduct(t, harness, company.OwnerToken, "RICE", 10, 900)
		supplierId := newSupplier(t, harness, company.OwnerToken, map[string]any{"name": "Kariakoo Wholesale"})
		mustRecord(t, harness, company.OwnerToken, arrival("sup-statement-0001", supplierId, 0, line(riceId, 20, 1000)))
		mustRecord(t, harness, company.OwnerToken, arrival("sup-statement-0002", supplierId, 5000, line(riceId, 10, 1000)))
		paid := harness.Call(http.MethodPost, "/api/supplier-payments", company.OwnerToken, map[string]any{"supplier_id": supplierId, "amount": 8000, "method": "cash"})
		if paid.Status != http.StatusCreated {
			t.Fatalf("payment returned %d %v", paid.Status, paid.Body)
		}

		statement := harness.Call(http.MethodGet, "/api/suppliers/"+supplierId+"/statement", company.OwnerToken, nil).Data()
		exported := harness.Call(http.MethodGet, "/api/suppliers/"+supplierId+"/statement?format=xlsx", company.OwnerToken, nil)
		if exported.Status != http.StatusOK {
			t.Fatalf("export returned %d %v", exported.Status, exported.Body)
		}
		workbook, openError := excelize.OpenReader(bytes.NewReader(exported.Raw))
		if openError != nil {
			t.Fatalf("open: %v", openError)
		}
		defer workbook.Close()
		sheetName := workbook.GetSheetName(0)
		sheetRows, _ := workbook.GetRows(sheetName)
		closingRow := 0
		allText := strings.Builder{}
		for rowIndex, sheetRow := range sheetRows {
			allText.WriteString(strings.Join(sheetRow, " | ") + "\n")
			if len(sheetRow) > 1 && sheetRow[0] == "Amount we owe" {
				closingRow = rowIndex + 1
			}
		}
		closing, _ := workbook.CalcCellValue(sheetName, "F"+strconv.Itoa(closingRow), excelize.Options{RawCellValue: true})
		wantClosing := strconv.FormatFloat(statement["closing_balance"].(float64), 'f', 0, 64)
		if closing != wantClosing || closing != "17000" {
			t.Fatalf("the statement's amount we owe is %q, the API says %s\n%s", closing, wantClosing, allText.String())
		}
		if !strings.Contains(allText.String(), "Kariakoo Wholesale") || !strings.Contains(allText.String(), "Stock bought") {
			t.Errorf("the statement does not name the supplier and purchases:\n%s", allText.String())
		}

		pdf := harness.Call(http.MethodGet, "/api/suppliers/"+supplierId+"/statement?format=pdf&lang=sw", company.OwnerToken, nil)
		if pdf.Status != http.StatusOK || !bytes.HasPrefix(pdf.Raw, []byte("%PDF")) {
			t.Fatalf("the Swahili PDF returned %d", pdf.Status)
		}
		expectError(t, harness.Call(http.MethodGet, "/api/suppliers/"+supplierId+"/statement?format=doc", company.OwnerToken, nil), http.StatusBadRequest, "invalid_format")
		if foreign := harness.Call(http.MethodGet, "/api/suppliers/"+supplierId+"/statement?format=pdf", otherCompany.OwnerToken, nil); foreign.Status != http.StatusNotFound {
			t.Errorf("another company's owner got %d", foreign.Status)
		}

		agingExport := harness.Call(http.MethodGet, "/api/suppliers/aging?format=xlsx", company.OwnerToken, nil)
		agingBook, agingError := excelize.OpenReader(bytes.NewReader(agingExport.Raw))
		if agingExport.Status != http.StatusOK || agingError != nil {
			t.Fatalf("the aging export returned %d: %v", agingExport.Status, agingError)
		}
		defer agingBook.Close()
		agingSheet := agingBook.GetSheetName(0)
		agingRows, _ := agingBook.GetRows(agingSheet)
		totalOwed := ""
		for rowIndex, sheetRow := range agingRows {
			if len(sheetRow) > 0 && sheetRow[0] == "Total" {
				totalOwed, _ = agingBook.CalcCellValue(agingSheet, "G"+strconv.Itoa(rowIndex+1), excelize.Options{RawCellValue: true})
			}
		}
		if totalOwed != "17000" {
			t.Errorf("the aging workbook totals %q, want 17000", totalOwed)
		}
		otherAging := harness.Call(http.MethodGet, "/api/suppliers/aging?format=xlsx", otherCompany.OwnerToken, nil)
		otherBook, _ := excelize.OpenReader(bytes.NewReader(otherAging.Raw))
		if otherBook != nil {
			otherRows, _ := otherBook.GetRows(otherBook.GetSheetName(0))
			for _, sheetRow := range otherRows {
				if strings.Contains(strings.Join(sheetRow, " "), "Kariakoo Wholesale") {
					t.Error("another company's supplier leaked into the aging report")
				}
			}
			otherBook.Close()
		}
	})
}
