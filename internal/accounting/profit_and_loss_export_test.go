package accounting_test

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

func workbookValue(t *testing.T, workbook *excelize.File, label string, columnName string) (string, bool) {
	t.Helper()
	sheetName := workbook.GetSheetName(0)
	sheetRows, rowsError := workbook.GetRows(sheetName)
	if rowsError != nil {
		t.Fatalf("rows: %v", rowsError)
	}
	for rowIndex, sheetRow := range sheetRows {
		if len(sheetRow) > 1 && sheetRow[1] == label {
			calculated, calcError := workbook.CalcCellValue(sheetName, columnName+strconv.Itoa(rowIndex+1), excelize.Options{RawCellValue: true})
			if calcError != nil {
				t.Fatalf("calculating %s: %v", label, calcError)
			}
			return calculated, true
		}
	}
	return "", false
}

func sheetText(t *testing.T, workbook *excelize.File) string {
	t.Helper()
	sheetRows, _ := workbook.GetRows(workbook.GetSheetName(0))
	allText := strings.Builder{}
	for _, sheetRow := range sheetRows {
		allText.WriteString(strings.Join(sheetRow, " | "))
		allText.WriteString("\n")
	}
	return allText.String()
}

func TestProfitAndLossExportsAStatementThatAgreesWithTheBooks(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Statement Shop", "owner@statement.test")
		turnOn(t, harness, company.OwnerToken, simpleBooks(false))
		startBooks(t, harness, company.OwnerToken, map[string]any{"cash_in_drawer": 50000})

		productId := newProduct(t, harness, company.OwnerToken, map[string]any{"sku": "SUKARI", "name": "Sukari", "price": 3500, "cost_price": 2500, "opening_quantity": 40})
		sell(t, harness, company.OwnerToken, "statement-sale-001", productId, 4, cash(14000))
		rentId := findAccountId(t, harness, company.OwnerToken, "rent")
		recorded := harness.Call(http.MethodPost, "/api/accounting/money", company.OwnerToken, map[string]any{
			"client_ref": "statement-rent-01", "kind": "expense", "amount": 20000, "money_account": "cash", "expense_account_id": rentId,
		})
		if recorded.Status != http.StatusCreated {
			t.Fatalf("rent returned %d %v", recorded.Status, recorded.Body)
		}

		books := harness.Call(http.MethodGet, "/api/accounting/profit-and-loss", company.OwnerToken, nil).Data()
		netProfit := number(t, books, "net_profit")
		if netProfit != 14000-10000-20000 {
			t.Fatalf("net profit is %d", netProfit)
		}

		exported := harness.Call(http.MethodGet, "/api/accounting/profit-and-loss?format=xlsx", company.OwnerToken, nil)
		if exported.Status != http.StatusOK || !strings.Contains(exported.Headers.Get("Content-Disposition"), "profit-and-loss-") {
			t.Fatalf("export returned %d %q", exported.Status, exported.Headers.Get("Content-Disposition"))
		}
		workbook, openError := excelize.OpenReader(bytes.NewReader(exported.Raw))
		if openError != nil {
			t.Fatalf("open: %v", openError)
		}
		defer workbook.Close()
		for label, want := range map[string]int64{"Total revenue": 14000, "Less: cost of goods sold": -10000, "Gross profit": 4000, "Total operating expenses": 20000, "Net loss": netProfit} {
			calculated, isFound := workbookValue(t, workbook, label, "C")
			if !isFound || calculated != strconv.FormatInt(want, 10) {
				t.Errorf("%s is %q (found %v), want %d", label, calculated, isFound, want)
			}
		}
		allText := sheetText(t, workbook)
		for _, expected := range []string{"Statement Shop", "Profit and loss", "All shops", "Compared with", "The books start on", "Prepared by", "Rent"} {
			if !strings.Contains(allText, expected) {
				t.Errorf("the workbook does not mention %q", expected)
			}
		}

		pdf := harness.Call(http.MethodGet, "/api/accounting/profit-and-loss?format=pdf&shop="+company.ShopId.String(), company.OwnerToken, nil)
		if pdf.Status != http.StatusOK || !bytes.HasPrefix(pdf.Raw, []byte("%PDF")) {
			t.Fatalf("the PDF returned %d", pdf.Status)
		}

		swahili := harness.Call(http.MethodGet, "/api/accounting/profit-and-loss?format=xlsx&lang=sw", company.OwnerToken, nil)
		swahiliBook, swahiliError := excelize.OpenReader(bytes.NewReader(swahili.Raw))
		if swahiliError != nil {
			t.Fatalf("open the Swahili workbook: %v", swahiliError)
		}
		defer swahiliBook.Close()
		if swahiliBook.GetSheetName(0) != "Faida na hasara" || !strings.Contains(sheetText(t, swahiliBook), "Hasara halisi") {
			t.Fatalf("the Swahili workbook is %q", swahiliBook.GetSheetName(0))
		}

		emptyPeriod := harness.Call(http.MethodGet, "/api/accounting/profit-and-loss?format=xlsx&from=2020-01-01&to=2020-01-31", company.OwnerToken, nil)
		emptyBook, emptyError := excelize.OpenReader(bytes.NewReader(emptyPeriod.Raw))
		if emptyError != nil {
			t.Fatalf("open the empty workbook (%d): %v", emptyPeriod.Status, emptyError)
		}
		defer emptyBook.Close()
		emptyText := sheetText(t, emptyBook)
		if !strings.Contains(emptyText, "No sales or other income in this period") || !strings.Contains(emptyText, "No expenses recorded in this period") {
			t.Fatalf("an empty period does not say so:\n%s", emptyText)
		}
		if calculated, _ := workbookValue(t, emptyBook, "Net profit", "C"); calculated != "0" {
			t.Fatalf("an empty period's net profit is %q", calculated)
		}

		refusals := map[string]string{
			"/api/accounting/profit-and-loss?format=pdf&from=2026-09-30&to=2026-09-01":             "invalid_date",
			"/api/accounting/profit-and-loss?format=pdf&shop=00000000-0000-0000-0000-000000000001": "not_found",
			"/api/accounting/profit-and-loss?format=docx":                                          "invalid_format",
		}
		for refusedPath, wantCode := range refusals {
			if refused := harness.Call(http.MethodGet, refusedPath, company.OwnerToken, nil); refused.Code() != wantCode {
				t.Errorf("%s returned %d %s, want %s", refusedPath, refused.Status, refused.Code(), wantCode)
			}
		}
	})
}
