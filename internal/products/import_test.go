package products_test

import (
	"fmt"
	"net/http"
	"strings"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
)

func importCsv(harness *apptest.Harness, sessionToken string, fileName string, csvText string) apptest.Response {
	return harness.Upload("/api/products/upload", sessionToken, "file", fileName, []byte(csvText))
}

func problemsOf(importResponse apptest.Response) []map[string]any {
	rawProblems, _ := importResponse.Data()["problems"].([]any)
	problems := make([]map[string]any, 0, len(rawProblems))
	for _, rawProblem := range rawProblems {
		problems = append(problems, rawProblem.(map[string]any))
	}
	return problems
}

func TestImportIsAllOrNothing(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Import Shop", "owner@import.test")

		largeCsv := strings.Builder{}
		largeCsv.WriteString("name,sku,barcode,price,costPrice,quantity,minStock,category\n")
		for rowNumber := 1; rowNumber <= 1000; rowNumber++ {
			fmt.Fprintf(&largeCsv, "Bulk item %d,BULK-%04d,77%06d,%d,%d,3,2,Bulk\n", rowNumber, rowNumber, rowNumber, 1000+rowNumber, 500)
		}
		bulkImport := importCsv(harness, company.OwnerToken, "bulk.csv", largeCsv.String())
		if bulkImport.Status != http.StatusCreated || bulkImport.Data()["created"] != float64(1000) {
			t.Fatalf("bulk import returned %d: %v", bulkImport.Status, bulkImport.Data())
		}
		importedStock := harness.QueryIntForCompany(company.Id, `SELECT COALESCE(SUM(quantity), 0) FROM shop_stock`)
		if importedStock != 3000 {
			t.Fatalf("opening stock after import is %d, want 3000", importedStock)
		}

		productsBefore := harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM products`)
		brokenCsv := "name,sku,price,quantity\n" +
			"Good one,GOOD-1,100,1\n" +
			",NO-NAME,100,1\n" +
			"Bad price,BAD-PRICE,abc,1\n" +
			"Bad count,BAD-COUNT,100,2.5\n" +
			"Repeat,GOOD-1,100,1\n" +
			"Existing,BULK-0001,100,1\n"
		brokenImport := importCsv(harness, company.OwnerToken, "broken.csv", brokenCsv)
		if brokenImport.Status != http.StatusUnprocessableEntity || brokenImport.Code() != "import_rejected" {
			t.Fatalf("broken import returned %d %s, want 422 import_rejected", brokenImport.Status, brokenImport.Code())
		}
		if harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM products`) != productsBefore {
			t.Fatal("a rejected import still created products")
		}
		expectedProblems := map[string]bool{
			"3:name":     false,
			"4:price":    false,
			"5:quantity": false,
			"6:sku":      false,
			"7:sku":      false,
		}
		for _, problem := range problemsOf(brokenImport) {
			problemKey := fmt.Sprintf("%v:%v", problem["row"], problem["column"])
			if _, isExpected := expectedProblems[problemKey]; isExpected {
				expectedProblems[problemKey] = true
			}
		}
		for problemKey, wasReported := range expectedProblems {
			if !wasReported {
				t.Fatalf("problem %s was not reported; got %v", problemKey, problemsOf(brokenImport))
			}
		}

		invalidFiles := []struct {
			fileName string
			content  string
		}{
			{"no-price.csv", "name,sku\nThing,THING-1\n"},
			{"notes.txt", "name,sku,price\nThing,THING-1,100\n"},
			{"empty.csv", "name,sku,price\n\n\n"},
		}
		for _, invalidFile := range invalidFiles {
			invalidImport := importCsv(harness, company.OwnerToken, invalidFile.fileName, invalidFile.content)
			if invalidImport.Status != http.StatusBadRequest || invalidImport.Code() != "invalid_import" {
				t.Fatalf("%s returned %d %s, want 400 invalid_import", invalidFile.fileName, invalidImport.Status, invalidImport.Code())
			}
		}
	})
}

func TestImportUnderstandsRealSpreadsheets(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Template Shop", "owner@template.test")

		templateResponse := harness.Call(http.MethodGet, "/api/products/template", company.OwnerToken, nil)
		if templateResponse.Status != http.StatusOK || !strings.Contains(templateResponse.Headers.Get("Content-Type"), "spreadsheetml") {
			t.Fatalf("template returned %d %s", templateResponse.Status, templateResponse.Headers.Get("Content-Type"))
		}
		templateImport := harness.Upload("/api/products/upload", company.OwnerToken, "file", "products-template.xlsx", templateResponse.Raw)
		if templateImport.Status != http.StatusCreated || templateImport.Data()["created"] != float64(1) {
			t.Fatalf("re-uploading the template returned %d: %v", templateImport.Status, templateImport.Body)
		}
		sampleProduct := harness.Call(http.MethodGet, "/api/products?q=SKU001", company.OwnerToken, nil).Items()[0].(map[string]any)
		if sampleProduct["price"] != float64(1000) || sampleProduct["quantity"] != float64(50) || sampleProduct["wholesale_price"] != float64(850) {
			t.Fatalf("template sample imported as %v", sampleProduct)
		}

		messyCsv := "\ufeffProduct Name;Item Code;Selling Price;Qty;Reorder Level;UOM\n" +
			"Sugar 1kg;SUG-1;\"3,500\";10;4;kg\n"
		messyImport := importCsv(harness, company.OwnerToken, "shop.csv", messyCsv)
		if messyImport.Status != http.StatusCreated {
			t.Fatalf("messy headers returned %d: %v", messyImport.Status, messyImport.Body)
		}
		sugar := harness.Call(http.MethodGet, "/api/products?q=SUG-1", company.OwnerToken, nil).Items()[0].(map[string]any)
		if sugar["price"] != float64(3500) || sugar["quantity"] != float64(10) || sugar["min_stock"] != float64(4) || sugar["unit"] != "kg" {
			t.Fatalf("messy row imported as %v", sugar)
		}

		switchCurrency := harness.Call(http.MethodPut, "/api/settings", company.OwnerToken, map[string]any{"currency_code": "KES", "currency_decimals": 2})
		if switchCurrency.Status != http.StatusOK {
			t.Fatalf("switch currency returned %d", switchCurrency.Status)
		}
		decimalCsv := "name,sku,price,costPrice\nTea,TEA-1,\"1,500.50\",KES 20\n"
		decimalImport := importCsv(harness, company.OwnerToken, "kes.csv", decimalCsv)
		if decimalImport.Status != http.StatusCreated {
			t.Fatalf("decimal import returned %d: %v", decimalImport.Status, decimalImport.Body)
		}
		tea := harness.Call(http.MethodGet, "/api/products?q=TEA-1", company.OwnerToken, nil).Items()[0].(map[string]any)
		if tea["price"] != float64(150050) || tea["cost_price"] != float64(2000) {
			t.Fatalf("KES prices stored as %v and %v minor units, want 150050 and 2000", tea["price"], tea["cost_price"])
		}
	})
}
