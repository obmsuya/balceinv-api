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

func openWorkbook(t *testing.T, exported apptest.Response) *excelize.File {
	t.Helper()
	if exported.Status != http.StatusOK {
		t.Fatalf("export returned %d %v", exported.Status, exported.Body)
	}
	workbook, openError := excelize.OpenReader(bytes.NewReader(exported.Raw))
	if openError != nil {
		t.Fatalf("open: %v", openError)
	}
	t.Cleanup(func() { workbook.Close() })
	return workbook
}

func calculatedBeside(t *testing.T, workbook *excelize.File, label string, labelColumn int, valueColumn string) string {
	t.Helper()
	sheetName := workbook.GetSheetName(0)
	sheetRows, _ := workbook.GetRows(sheetName)
	for rowIndex, sheetRow := range sheetRows {
		if len(sheetRow) > labelColumn && sheetRow[labelColumn] == label {
			calculated, calcError := workbook.CalcCellValue(sheetName, valueColumn+strconv.Itoa(rowIndex+1), excelize.Options{RawCellValue: true})
			if calcError != nil {
				t.Fatalf("calculating %s: %v", label, calcError)
			}
			return calculated
		}
	}
	t.Fatalf("no row labelled %q", label)
	return ""
}

func TestBookStatementsAgreeWithTheLedgerInExcelAndPdf(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Books Shop", "owner@books.test")
		turnOn(t, harness, company.OwnerToken, map[string]any{"accounting_mode": "full", "vat_registered": true, "vat_number": "40-000111-A"})
		startBooks(t, harness, company.OwnerToken, map[string]any{"cash_in_drawer": 100000, "bank": 500000})

		productId := newProduct(t, harness, company.OwnerToken, map[string]any{"sku": "SOAP", "name": "Soap", "price": 1180, "cost_price": 600, "opening_quantity": 100})
		sell(t, harness, company.OwnerToken, "books-export-sale-01", productId, 10, cash(11800))
		sell(t, harness, company.OwnerToken, "books-export-sale-02", productId, 5, []map[string]any{{"method": "mobile", "amount": 5900}})
		rentId := findAccountId(t, harness, company.OwnerToken, "rent")
		recorded := harness.Call(http.MethodPost, "/api/accounting/money", company.OwnerToken, map[string]any{
			"client_ref": "books-export-rent-1", "kind": "expense", "amount": 23600, "money_account": "cash", "expense_account_id": rentId,
			"includes_vat": true, "supplier_tin": "100-200-300", "receipt_number": "EFD-7",
		})
		if recorded.Status != http.StatusCreated {
			t.Fatalf("rent returned %d %v", recorded.Status, recorded.Body)
		}

		balanceSheet := harness.Call(http.MethodGet, "/api/accounting/balance-sheet", company.OwnerToken, nil).Data()
		balanceBook := openWorkbook(t, harness.Call(http.MethodGet, "/api/accounting/balance-sheet?format=xlsx", company.OwnerToken, nil))
		totalAssets := strconv.FormatInt(number(t, balanceSheet, "total_assets"), 10)
		if calculatedBeside(t, balanceBook, "Total assets", 1, "C") != totalAssets || calculatedBeside(t, balanceBook, "Liabilities and equity", 1, "C") != totalAssets {
			t.Errorf("the balance sheet workbook does not balance to %s", totalAssets)
		}

		trialBalance := harness.Call(http.MethodGet, "/api/accounting/trial-balance", company.OwnerToken, nil).Data()
		trialBook := openWorkbook(t, harness.Call(http.MethodGet, "/api/accounting/trial-balance?format=xlsx", company.OwnerToken, nil))
		totalDebits := strconv.FormatInt(number(t, trialBalance, "total_debit_balance"), 10)
		if calculatedBeside(t, trialBook, "Total", 0, "C") != totalDebits || calculatedBeside(t, trialBook, "Total", 0, "D") != totalDebits {
			t.Errorf("the trial balance workbook does not total %s on both sides", totalDebits)
		}

		cashStatement := harness.Call(http.MethodGet, "/api/accounting/statement?account=cash", company.OwnerToken, nil).Data()
		cashBook := openWorkbook(t, harness.Call(http.MethodGet, "/api/accounting/statement?account=cash&format=xlsx", company.OwnerToken, nil))
		headerRow, _ := cashBook.GetRows(cashBook.GetSheetName(0))
		balanceColumn := ""
		for _, sheetRow := range headerRow {
			if len(sheetRow) > 1 && sheetRow[0] == "Date" && balanceColumn == "" {
				balanceColumn, _ = excelize.ColumnNumberToName(len(sheetRow))
			}
		}
		if calculatedBeside(t, cashBook, "Closing balance", 0, balanceColumn) != strconv.FormatInt(number(t, cashStatement, "closing_balance"), 10) {
			t.Errorf("the cash book's running balance %q (column %s) does not reach the ledger's closing balance %v\n%s", calculatedBeside(t, cashBook, "Closing balance", 0, balanceColumn), balanceColumn, cashStatement["closing_balance"], sheetText(t, cashBook))
		}
		lines := cashStatement["lines"].([]any)
		rentLine := lines[len(lines)-1].(map[string]any)
		if counterparts, _ := rentLine["counterparts"].([]any); len(counterparts) != 2 {
			t.Errorf("the rent line shows %v as its other side, want rent and VAT", rentLine["counterparts"])
		}
		if !strings.Contains(sheetText(t, cashBook), "Money out · Rent") {
			t.Error("the cash book does not say what the money went on")
		}

		vatReport := harness.Call(http.MethodGet, "/api/accounting/vat", company.OwnerToken, nil).Data()
		vatBook := openWorkbook(t, harness.Call(http.MethodGet, "/api/accounting/vat?format=xlsx", company.OwnerToken, nil))
		if calculatedBeside(t, vatBook, "Total", 0, "D") != strconv.FormatInt(number(t, vatReport, "total_to_pay"), 10) {
			t.Error("the VAT workbook's total to pay differs from the VAT report")
		}

		overview := harness.Call(http.MethodGet, "/api/accounting/overview", company.OwnerToken, nil).Data()
		overviewBook := openWorkbook(t, harness.Call(http.MethodGet, "/api/accounting/overview?format=xlsx", company.OwnerToken, nil))
		netWorth := strconv.FormatInt(number(t, overview, "what_i_own")-number(t, overview, "what_i_owe"), 10)
		if calculatedBeside(t, overviewBook, "Net worth", 1, "C") != netWorth {
			t.Errorf("the money summary's net worth is not %s", netWorth)
		}

		for _, exportPath := range []string{"balance-sheet", "trial-balance", "statement?account=cash", "vat", "overview"} {
			separator := "?"
			if strings.Contains(exportPath, "?") {
				separator = "&"
			}
			exported := harness.Call(http.MethodGet, "/api/accounting/"+exportPath+separator+"format=pdf&lang=sw", company.OwnerToken, nil)
			if exported.Status != http.StatusOK || !bytes.HasPrefix(exported.Raw, []byte("%PDF")) {
				t.Errorf("%s as a Swahili PDF returned %d", exportPath, exported.Status)
			}
		}
		swahiliBook := openWorkbook(t, harness.Call(http.MethodGet, "/api/accounting/overview?format=xlsx&lang=sw", company.OwnerToken, nil))
		if swahiliBook.GetSheetName(0) != "Muhtasari wa fedha" {
			t.Errorf("the Swahili money summary sheet is %q", swahiliBook.GetSheetName(0))
		}
	})
}
