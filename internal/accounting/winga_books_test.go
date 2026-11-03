package accounting_test

import (
	"net/http"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
)

func TestWingaShareIsBookedAsCostPaidFromTheTill(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Winga Books", "owner@wingabooks.test")
		turnOn(t, harness, company.OwnerToken, map[string]any{"accounting_mode": "simple", "winga_enabled": true})
		startBooks(t, harness, company.OwnerToken, map[string]any{"cash_in_drawer": 0})
		riceId := newProduct(t, harness, company.OwnerToken, map[string]any{"sku": "RICE", "name": "Rice", "price": 10000, "cost_price": 6000, "opening_quantity": 5})

		sold := harness.Call(http.MethodPost, "/api/sales", company.OwnerToken, map[string]any{
			"client_ref":  "winga-books-0001",
			"items":       []map[string]any{{"product_id": riceId, "quantity": 1}},
			"winga_lines": []map[string]any{{"name": "Phone case", "quantity": 2, "unit_price": 15000, "winga_share": 20000}},
			"payments":    []map[string]any{{"method": "cash", "amount": 40000}},
		})
		if sold.Status != http.StatusCreated {
			t.Fatalf("the winga sale returned %d %v", sold.Status, sold.Body)
		}
		assertLines(t, "winga sale", sourceLines(t, harness, company.Id, "sale", sold.Data()["id"].(string)), map[string]int64{
			"cash": 20000, "sales": -40000, "cogs": 26000, "inventory": -6000,
		})
	})
}
