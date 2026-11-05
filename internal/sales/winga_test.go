package sales_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/google/uuid"
)

func wingaLine(name string, quantity int, unitPrice int64, wingaShare int64) map[string]any {
	return map[string]any{"name": name, "quantity": quantity, "unit_price": unitPrice, "winga_share": wingaShare}
}

func sellWithWinga(harness *apptest.Harness, sessionToken string, clientRef string, items []map[string]any, wingaLines []map[string]any, payments []map[string]any) apptest.Response {
	return harness.Call(http.MethodPost, "/api/sales", sessionToken, map[string]any{"client_ref": clientRef, "items": items, "winga_lines": wingaLines, "payments": payments})
}

func TestWingaLinesAreOffUntilTurnedOnThenSellWithoutStock(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Winga Shop", "owner@winga.test")
		cashierToken := harness.CreateStaff(company, "cashier@winga.test", []string{"sales:create"}, []uuid.UUID{company.ShopId})
		riceId := newProduct(t, harness, company.OwnerToken, map[string]any{"sku": "RICE", "name": "Rice", "price": 10000, "cost_price": 6000, "opening_quantity": 10})
		phoneCase := wingaLine(" Phone case ", 2, 15000, 20000)

		turnedOff := sellWithWinga(harness, cashierToken, "winga-off-0001", nil, []map[string]any{phoneCase}, cash(30000))
		if turnedOff.Status != http.StatusConflict || turnedOff.Code() != "winga_off" {
			t.Fatalf("a winga line while the feature is off returned %d %v", turnedOff.Status, turnedOff.Body)
		}
		harness.Call(http.MethodPut, "/api/features", company.OwnerToken, map[string]any{"winga_enabled": true})

		emptySale := sellWithWinga(harness, cashierToken, "winga-empty-0001", nil, nil, cash(1000))
		tooBigShare := sellWithWinga(harness, cashierToken, "winga-share-0001", nil, []map[string]any{wingaLine("Charger", 1, 5000, 5001)}, cash(5000))
		blankName := sellWithWinga(harness, cashierToken, "winga-blank-0001", nil, []map[string]any{wingaLine("   ", 1, 5000, 0)}, cash(5000))
		if emptySale.Code() != "empty_sale" || tooBigShare.Code() != "invalid_winga_line" || blankName.Code() != "invalid_winga_line" {
			t.Fatalf("bad winga sales returned %s, %s and %s", emptySale.Code(), tooBigShare.Code(), blankName.Code())
		}

		quote := harness.Call(http.MethodPost, "/api/sales/quote", cashierToken, map[string]any{"items": []any{map[string]any{"product_id": riceId, "quantity": 1}}, "winga_lines": []any{phoneCase}})
		if quote.Status != http.StatusOK || quote.Data()["total"] != float64(40000) || len(quote.Data()["winga_lines"].([]any)) != 1 {
			t.Fatalf("a winga quote returned %d %v", quote.Status, quote.Body)
		}

		mixedSale := sellWithWinga(harness, cashierToken, "winga-mixed-0001", []map[string]any{{"product_id": riceId, "quantity": 1}}, []map[string]any{phoneCase}, cash(40000))
		mixedData := mixedSale.Data()
		if mixedSale.Status != http.StatusCreated || mixedData["total"] != float64(40000) || mixedData["subtotal"] != float64(40000) {
			t.Fatalf("a sale with a winga line returned %d %v", mixedSale.Status, mixedSale.Body)
		}
		savedWinga := mixedData["winga_lines"].([]any)[0].(map[string]any)
		if savedWinga["name"] != "Phone case" || savedWinga["line_total"] != float64(30000) || savedWinga["winga_share"] != float64(20000) {
			t.Fatalf("the saved winga line is %v", savedWinga)
		}

		wingaOnly := sellWithWinga(harness, cashierToken, "winga-only-0001", nil, []map[string]any{wingaLine("Earphones", 1, 8000, 0)}, cash(8000))
		if wingaOnly.Status != http.StatusCreated || wingaOnly.Data()["total"] != float64(8000) {
			t.Fatalf("a winga-only sale returned %d %v", wingaOnly.Status, wingaOnly.Body)
		}
		for _, listedSale := range harness.Call(http.MethodGet, "/api/sales", company.OwnerToken, nil).Items() {
			listedFields := listedSale.(map[string]any)
			wantedUnits := map[string]float64{mixedData["id"].(string): 3, wingaOnly.Data()["id"].(string): 1}[listedFields["id"].(string)]
			if listedFields["unit_count"] != wantedUnits {
				t.Fatalf("sale %v lists %v units, want %v", listedFields["receipt_number"], listedFields["unit_count"], wantedUnits)
			}
		}

		if harness.QueryIntForCompany(company.Id, `SELECT quantity FROM shop_stock WHERE product_id = $1`, riceId) != 9 {
			t.Fatal("winga lines moved stock")
		}
		if harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM stock_movements WHERE reason = 'sale'`) != 1 {
			t.Fatal("winga lines wrote stock movements")
		}

		darEsSalaam, _ := time.LoadLocation("Africa/Dar_es_Salaam")
		todayInShop := time.Now().In(darEsSalaam).Format("2006-01-02")
		summary := harness.Call(http.MethodGet, "/api/reports/summary?from="+todayInShop+"&to="+todayInShop, company.OwnerToken, nil).Data()
		if summary["total"] != float64(48000) || summary["winga_total"] != float64(38000) || summary["winga_share_total"] != float64(20000) ||
			summary["cost_total"] != float64(26000) || summary["gross_profit"] != float64(22000) || summary["units_sold"] != float64(4) {
			t.Fatalf("the summary with winga lines is %v", summary)
		}
	})
}
