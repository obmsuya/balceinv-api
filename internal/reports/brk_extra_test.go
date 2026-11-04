package reports_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/google/uuid"
)

func TestBrkAndExtraShowInTheReports(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Brk Shop", "owner@brk.test")
		cashierToken := harness.CreateStaff(company, "cashier@brk.test", []string{"sales:create"}, []uuid.UUID{company.ShopId})
		harness.Call(http.MethodPut, "/api/features", company.OwnerToken, map[string]any{"winga_enabled": true})
		riceId := newProduct(t, harness, company.OwnerToken, map[string]any{"sku": "RICE", "name": "Rice", "price": 10000, "cost_price": 6000, "opening_quantity": 50})

		brkSale := harness.Call(http.MethodPost, "/api/sales", cashierToken, map[string]any{
			"client_ref":  "brk-report-0001",
			"items":       []any{map[string]any{"product_id": riceId, "quantity": 2, "extra": map[string]any{"kind": "amount", "value": 1500}}},
			"winga_lines": []any{map[string]any{"name": "Phone case", "quantity": 1, "unit_price": 15000, "winga_share": 10000}},
			"payments":    cash(38000),
		})
		if brkSale.Status != http.StatusCreated || brkSale.Data()["total"] != float64(38000) {
			t.Fatalf("the brk sale returned %d %v", brkSale.Status, brkSale.Body)
		}
		sell(t, harness, company.OwnerToken, "brk-report-0002", riceId, 1, cash(10000))

		rangeQuery := "?from=" + today() + "&to=" + today()
		todayRow := harness.Call(http.MethodGet, "/api/reports/daily"+rangeQuery, company.OwnerToken, nil).Body["data"].([]any)[0].(map[string]any)
		if number(t, todayRow, "extra_total") != 3000 || number(t, todayRow, "winga_total") != 15000 || number(t, todayRow, "winga_share_total") != 10000 ||
			number(t, todayRow, "total") != 48000 || number(t, todayRow, "cost_total") != 28000 {
			t.Fatalf("today's row is %v", todayRow)
		}

		staffRows := harness.Call(http.MethodGet, "/api/reports/cashiers"+rangeQuery, company.OwnerToken, nil).Body["data"].([]any)
		cashierRow := staffRows[0].(map[string]any)
		ownerRow := staffRows[1].(map[string]any)
		if number(t, cashierRow, "total") != 38000 || number(t, cashierRow, "extra_total") != 3000 || number(t, cashierRow, "winga_total") != 15000 || number(t, cashierRow, "winga_share_total") != 10000 ||
			number(t, ownerRow, "extra_total") != 0 || number(t, ownerRow, "winga_total") != 0 {
			t.Fatalf("the staff rows are %v", staffRows)
		}

		linesBook := openWorkbook(t, harness.Call(http.MethodGet, "/api/reports/brk-extra/export"+rangeQuery+"&format=xlsx&lang=en", company.OwnerToken, nil))
		linesText := sheetText(linesBook)
		receiptNumber := brkSale.Data()["receipt_number"].(string)
		for _, wanted := range []string{"Brk and Extra", "Brk share", "Phone case", "Rice", receiptNumber} {
			if !strings.Contains(linesText, wanted) {
				t.Fatalf("the Brk and Extra sheet has no %q:\n%s", wanted, linesText)
			}
		}
		if strings.Contains(linesText, "brk-report-0002") || strings.Count(linesText, receiptNumber) != 2 {
			t.Fatalf("the Brk and Extra sheet lists the wrong lines:\n%s", linesText)
		}

		dailyBook := openWorkbook(t, harness.Call(http.MethodGet, "/api/reports/daily/export"+rangeQuery+"&format=xlsx&lang=sw", company.OwnerToken, nil))
		if !strings.Contains(sheetText(dailyBook), "Sehemu ya Brk") || !strings.Contains(sheetText(dailyBook), "Ziada") {
			t.Fatalf("the daily sheet has no Brk or Extra columns:\n%s", sheetText(dailyBook))
		}

		linesPdf := harness.Call(http.MethodGet, "/api/reports/brk-extra/export"+rangeQuery+"&format=pdf", company.OwnerToken, nil)
		if linesPdf.Status != http.StatusOK || linesPdf.Headers.Get("Content-Type") != "application/pdf" {
			t.Fatalf("the Brk and Extra PDF returned %d %q", linesPdf.Status, linesPdf.Headers.Get("Content-Type"))
		}

		quietCompany := harness.CreateCompany("Quiet Shop", "owner@quiet.test")
		quietBook := openWorkbook(t, harness.Call(http.MethodGet, "/api/reports/brk-extra/export"+rangeQuery+"&format=xlsx&lang=en", quietCompany.OwnerToken, nil))
		if !strings.Contains(sheetText(quietBook), "No Extra or Brk in this period") {
			t.Fatalf("another company's sheet is %s", sheetText(quietBook))
		}
		quietDaily := openWorkbook(t, harness.Call(http.MethodGet, "/api/reports/daily/export"+rangeQuery+"&format=xlsx&lang=en", quietCompany.OwnerToken, nil))
		if strings.Contains(sheetText(quietDaily), "Brk") {
			t.Fatalf("a shop without Brk got Brk columns:\n%s", sheetText(quietDaily))
		}
	})
}
