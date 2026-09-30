package customers_test

import (
	"bytes"
	"net/http"
	"strconv"
	"strings"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/google/uuid"
	"github.com/xuri/excelize/v2"
)

func TestCustomerStatementExportsWhatTheCustomerOwes(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Statement Duka", "owner@custstatement.test")
		otherCompany := harness.CreateCompany("Other Duka", "owner@othercuststatement.test")
		turnOn(t, harness, company.OwnerToken, map[string]any{"customers_enabled": true, "credit_sales_enabled": true})
		turnOn(t, harness, otherCompany.OwnerToken, map[string]any{"customers_enabled": true})
		productId := newProduct(t, harness, company.OwnerToken, "RICE", 3000, 50)
		customerId := harness.Call(http.MethodPost, "/api/customers", company.OwnerToken, map[string]any{"name": "Mama Rehema", "phone": "0713444555"}).Data()["id"].(string)
		sellOnCredit(t, harness, company.OwnerToken, "cust-statement-1", customerId, productId, 4, []map[string]any{{"method": "credit", "amount": 12000}})
		sellOnCredit(t, harness, company.OwnerToken, "cust-statement-2", customerId, productId, 2, []map[string]any{{"method": "cash", "amount": 1000}, {"method": "credit", "amount": 5000}})
		paid := harness.Call(http.MethodPost, "/api/customers/"+customerId+"/payments", company.OwnerToken, map[string]any{"amount": 7000, "method": "mobile"})
		if paid.Status != http.StatusCreated {
			t.Fatalf("payment returned %d %v", paid.Status, paid.Body)
		}

		statement := harness.Call(http.MethodGet, "/api/customers/"+customerId+"/statement", company.OwnerToken, nil).Data()
		exported := harness.Call(http.MethodGet, "/api/customers/"+customerId+"/statement?format=xlsx", company.OwnerToken, nil)
		if exported.Status != http.StatusOK || !strings.Contains(exported.Headers.Get("Content-Disposition"), "customer-statement-Mama-Rehema") {
			t.Fatalf("export returned %d %q", exported.Status, exported.Headers.Get("Content-Disposition"))
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
			if len(sheetRow) > 1 && sheetRow[0] == "Amount due" {
				closingRow = rowIndex + 1
			}
		}
		closing, _ := workbook.CalcCellValue(sheetName, "F"+strconv.Itoa(closingRow), excelize.Options{RawCellValue: true})
		if closing != strconv.FormatFloat(statement["closing_balance"].(float64), 'f', 0, 64) || closing != "10000" {
			t.Fatalf("the statement's amount due is %q, the API says %v\n%s", closing, statement["closing_balance"], allText.String())
		}
		for _, expected := range []string{"Mama Rehema", "0713444555", "Sale on credit", "Mobile money"} {
			if !strings.Contains(allText.String(), expected) {
				t.Errorf("the statement does not show %q", expected)
			}
		}

		pdf := harness.Call(http.MethodGet, "/api/customers/"+customerId+"/statement?format=pdf&lang=sw", company.OwnerToken, nil)
		if pdf.Status != http.StatusOK || !bytes.HasPrefix(pdf.Raw, []byte("%PDF")) {
			t.Fatalf("the Swahili PDF returned %d", pdf.Status)
		}
		if refused := harness.Call(http.MethodGet, "/api/customers/"+customerId+"/statement?format=doc", company.OwnerToken, nil); refused.Code() != "invalid_format" {
			t.Errorf("an unknown format returned %d %s", refused.Status, refused.Code())
		}
		if foreign := harness.Call(http.MethodGet, "/api/customers/"+customerId+"/statement?format=pdf", otherCompany.OwnerToken, nil); foreign.Status != http.StatusNotFound {
			t.Errorf("another company's owner got %d", foreign.Status)
		}
		if missing := harness.Call(http.MethodGet, "/api/customers/"+uuid.NewString()+"/statement?format=pdf", company.OwnerToken, nil); missing.Status != http.StatusNotFound {
			t.Errorf("a missing customer returned %d", missing.Status)
		}
	})
}
