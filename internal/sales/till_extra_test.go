package sales_test

import (
	"net/http"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/google/uuid"
)

func lineWithExtra(productId string, quantity int, kind string, value int64) map[string]any {
	return map[string]any{"product_id": productId, "quantity": quantity, "extra": map[string]any{"kind": kind, "value": value}}
}

func TestAnyCashierCanAddAnExtraAndItIsRevenue(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Extra Shop", "owner@extra.test")
		plainCashierToken := harness.CreateStaff(company, "plain@extra.test", []string{"sales:create"}, []uuid.UUID{company.ShopId})
		riceId := newProduct(t, harness, company.OwnerToken, map[string]any{"sku": "RICE", "name": "Rice", "price": 10000, "opening_quantity": 50})

		quote := harness.Call(http.MethodPost, "/api/sales/quote", plainCashierToken, map[string]any{"items": []any{lineWithExtra(riceId, 2, "amount", 1500)}})
		if quote.Status != http.StatusOK || quote.Data()["total"] != float64(23000) {
			t.Fatalf("a plain cashier's extra quoted %d %v", quote.Status, quote.Body)
		}

		sale := sell(harness, plainCashierToken, "extra-plain-0001", []map[string]any{lineWithExtra(riceId, 2, "amount", 1500), lineWithExtra(riceId, 1, "percent", 2000)}, cash(35000))
		if sale.Status != http.StatusCreated || sale.Data()["total"] != float64(35000) || sale.Data()["discount_total"] != float64(0) {
			t.Fatalf("a sale with extras returned %d %v", sale.Status, sale.Body)
		}

		savedLines := harness.Call(http.MethodGet, "/api/sales/"+sale.Data()["id"].(string), company.OwnerToken, nil).Data()["items"].([]any)
		firstLine := savedLines[0].(map[string]any)
		secondLine := savedLines[1].(map[string]any)
		if firstLine["extra_unit_amount"] != float64(1500) || firstLine["unit_price"] != float64(11500) || firstLine["line_total"] != float64(23000) ||
			secondLine["extra_unit_amount"] != float64(2000) || secondLine["line_total"] != float64(12000) {
			t.Fatalf("the saved extras are %v and %v", firstLine, secondLine)
		}

		badExtra := harness.Call(http.MethodPost, "/api/sales/quote", plainCashierToken, map[string]any{"items": []any{lineWithExtra(riceId, 1, "amount", 0)}})
		if badExtra.Status != http.StatusBadRequest {
			t.Fatalf("a zero extra returned %d %v", badExtra.Status, badExtra.Body)
		}
	})
}
