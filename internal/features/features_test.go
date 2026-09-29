package features_test

import (
	"net/http"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/google/uuid"
)

func TestFeatureSwitchesStartOffAndFollowTheirRules(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Feature Shop", "owner@features.test")
		otherCompany := harness.CreateCompany("Other Shop", "owner@otherfeatures.test")
		cashierToken := harness.CreateStaff(company, "cashier@features.test", []string{"sales:create"}, []uuid.UUID{company.ShopId})

		startingFeatures := harness.Call(http.MethodGet, "/api/features", cashierToken, nil).Data()
		for _, switchName := range []string{"suppliers_enabled", "purchase_orders_enabled", "customers_enabled", "credit_sales_enabled", "customer_orders_enabled", "vat_registered"} {
			if startingFeatures[switchName] != false {
				t.Fatalf("%s started as %v", switchName, startingFeatures[switchName])
			}
		}
		if startingFeatures["accounting_mode"] != "off" {
			t.Fatalf("accounting started as %v", startingFeatures["accounting_mode"])
		}

		if harness.Call(http.MethodPut, "/api/features", cashierToken, map[string]any{"customers_enabled": true}).Status != http.StatusForbidden {
			t.Fatal("a cashier changed the feature switches")
		}

		refusals := []map[string]any{
			{"credit_sales_enabled": true},
			{"customer_orders_enabled": true},
			{"purchase_orders_enabled": true},
			{"vat_registered": true},
			{"accounting_mode": "fancy"},
		}
		for _, refusedChange := range refusals {
			refused := harness.Call(http.MethodPut, "/api/features", company.OwnerToken, refusedChange)
			if refused.Status != http.StatusBadRequest {
				t.Fatalf("%v was accepted: %d %v", refusedChange, refused.Status, refused.Body)
			}
		}

		saved := harness.Call(http.MethodPut, "/api/features", company.OwnerToken, map[string]any{
			"customers_enabled": true, "credit_sales_enabled": true, "suppliers_enabled": true,
			"accounting_mode": "simple", "vat_registered": true, "vat_number": " 40-012345-A ",
		})
		if saved.Status != http.StatusOK || saved.Data()["credit_sales_enabled"] != true || saved.Data()["vat_number"] != "40-012345-A" {
			t.Fatalf("saving features returned %d %v", saved.Status, saved.Body)
		}

		meFeatures := harness.Call(http.MethodGet, "/api/auth/me", cashierToken, nil).Data()["features"].(map[string]any)
		if meFeatures["credit_sales_enabled"] != true || meFeatures["accounting_mode"] != "simple" {
			t.Fatalf("the signed-in user sees features %v", meFeatures)
		}
		otherFeatures := harness.Call(http.MethodGet, "/api/features", otherCompany.OwnerToken, nil).Data()
		if otherFeatures["customers_enabled"] != false || otherFeatures["accounting_mode"] != "off" {
			t.Fatalf("another company's switches changed: %v", otherFeatures)
		}

		if harness.Call(http.MethodPut, "/api/features", company.OwnerToken, map[string]any{"customers_enabled": false}).Status != http.StatusBadRequest {
			t.Fatal("customers were turned off while credit sales still need them")
		}
	})
}
