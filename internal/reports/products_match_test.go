package reports_test

import (
	"net/http"
	"strings"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
)

func TestProductsSoldAddsUpToTheSalesReport(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Matching Shop", "owner@matching.test")
		harness.Call(http.MethodPut, "/api/features", company.OwnerToken, map[string]any{"vat_registered": true, "vat_number": "40-000222-B", "winga_enabled": true})
		sodaId := newProduct(t, harness, company.OwnerToken, map[string]any{"sku": "SODA", "name": "Soda", "price": 1190, "cost_price": 600, "opening_quantity": 100})
		riceId := newProduct(t, harness, company.OwnerToken, map[string]any{"sku": "RICE", "name": "Rice", "price": 10070, "cost_price": 6000, "opening_quantity": 50})

		brkSale := harness.Call(http.MethodPost, "/api/sales", company.OwnerToken, map[string]any{
			"client_ref":  "matching-sale-0001",
			"items":       []any{map[string]any{"product_id": sodaId, "quantity": 3}},
			"winga_lines": []any{map[string]any{"name": "Phone case", "quantity": 1, "unit_price": 15000, "winga_share": 10000}},
			"payments":    cash(18570),
		})
		if brkSale.Status != http.StatusCreated {
			t.Fatalf("the Brk sale returned %d %v", brkSale.Status, brkSale.Body)
		}
		mixedSale := harness.Call(http.MethodPost, "/api/sales", company.OwnerToken, map[string]any{
			"client_ref": "matching-sale-0002",
			"items":      []any{map[string]any{"product_id": riceId, "quantity": 1}, map[string]any{"product_id": sodaId, "quantity": 2}},
			"payments":   cash(12450),
		})
		if mixedSale.Status != http.StatusCreated {
			t.Fatalf("the mixed sale returned %d %v", mixedSale.Status, mixedSale.Body)
		}
		sodaItemId := brkSale.Data()["items"].([]any)[0].(map[string]any)["item_id"].(string)
		refund := harness.Call(http.MethodPost, "/api/sales/"+brkSale.Data()["id"].(string)+"/refunds", company.OwnerToken, map[string]any{
			"client_ref": "matching-refund-0001", "method": "cash", "restock": true, "reason": "one soda came back",
			"lines": []any{map[string]any{"item_id": sodaItemId, "quantity": 1}},
		})
		if refund.Status != http.StatusCreated {
			t.Fatalf("the refund returned %d %v", refund.Status, refund.Body)
		}

		rangeQuery := "?from=" + today() + "&to=" + today()
		summary := harness.Call(http.MethodGet, "/api/reports/summary"+rangeQuery, company.OwnerToken, nil).Data()
		productRows := harness.Call(http.MethodGet, "/api/reports/products"+rangeQuery+"&limit=100", company.OwnerToken, nil).Body["data"].([]any)

		sums := map[string]int64{}
		rowsByName := map[string]map[string]any{}
		for _, productRow := range productRows {
			row := productRow.(map[string]any)
			rowsByName[row["name"].(string)] = row
			for _, key := range []string{"quantity", "revenue", "net_revenue", "cost_total", "gross_profit"} {
				sums[key] += number(t, row, key)
			}
		}
		if sums["revenue"] != number(t, summary, "total")-number(t, summary, "refund_total") {
			t.Fatalf("products takings %d, want sales takings %d less refunds %d", sums["revenue"], number(t, summary, "total"), number(t, summary, "refund_total"))
		}
		for productKey, summaryKey := range map[string]string{"net_revenue": "net_sales", "cost_total": "cost_total", "gross_profit": "gross_profit"} {
			if sums[productKey] != number(t, summary, summaryKey) {
				t.Fatalf("products %s adds up to %d, the sales report says %s = %d (%v)", productKey, sums[productKey], summaryKey, number(t, summary, summaryKey), productRows)
			}
		}

		phoneCase, soda := rowsByName["Phone case"], rowsByName["Soda"]
		if phoneCase == nil || phoneCase["is_brk"] != true || number(t, phoneCase, "revenue") != 15000 || number(t, phoneCase, "cost_total") != 10000 {
			t.Fatalf("the Brk row is %v", phoneCase)
		}
		if soda == nil || soda["is_brk"] != false || number(t, soda, "quantity") != 4 || number(t, soda, "revenue") != 4*1190 || number(t, soda, "cost_total") != 4*600 || number(t, soda, "sale_count") != 2 {
			t.Fatalf("the soda row is %v", soda)
		}

		dashboardProducts := harness.Call(http.MethodGet, "/api/dashboard", company.OwnerToken, nil).Data()["top_products"].([]any)
		if len(dashboardProducts) != 2 {
			t.Fatalf("best sellers must list stock products only, got %v", dashboardProducts)
		}

		salesBook := openWorkbook(t, harness.Call(http.MethodGet, "/api/reports/summary/export"+rangeQuery+"&format=xlsx&lang=en", company.OwnerToken, nil))
		if !strings.Contains(sheetText(salesBook), "Less: refunds") {
			t.Fatalf("the sales report has no refunds row:\n%s", sheetText(salesBook))
		}
		salesPdf := harness.Call(http.MethodGet, "/api/reports/summary/export"+rangeQuery+"&format=pdf&lang=sw", company.OwnerToken, nil)
		if salesPdf.Status != http.StatusOK {
			t.Fatalf("the sales report PDF with a refund returned %d %v", salesPdf.Status, salesPdf.Body)
		}
		productsBook := openWorkbook(t, harness.Call(http.MethodGet, "/api/reports/products/export"+rangeQuery+"&format=xlsx&lang=en", company.OwnerToken, nil))
		for _, wanted := range []string{"Phone case", "Brk", "Refunds in the period"} {
			if !strings.Contains(sheetText(productsBook), wanted) {
				t.Fatalf("the products sheet has no %q:\n%s", wanted, sheetText(productsBook))
			}
		}
	})
}
