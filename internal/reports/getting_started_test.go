package reports_test

import (
	"net/http"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
)

func gettingStartedOf(t *testing.T, harness *apptest.Harness, sessionToken string) map[string]any {
	t.Helper()
	dashboard := harness.Call(http.MethodGet, "/api/dashboard", sessionToken, nil)
	if dashboard.Status != http.StatusOK {
		t.Fatalf("dashboard returned %d: %v", dashboard.Status, dashboard.Body)
	}
	return dashboard.Data()["getting_started"].(map[string]any)
}

func TestDashboardGettingStartedFollowsSetup(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Checklist Shop", "owner@checklist.test")
		otherCompany := harness.CreateCompany("Other Checklist", "owner@otherchecklist.test")

		fresh := gettingStartedOf(t, harness, company.OwnerToken)
		for _, item := range []string{"first_product", "first_cashier", "first_sale", "logo"} {
			if fresh[item] != false {
				t.Fatalf("a new company has %s done: %v", item, fresh)
			}
		}

		productId := newProduct(t, harness, company.OwnerToken, map[string]any{"sku": "CHK", "name": "Checklist item", "price": 1000, "cost_price": 600, "opening_quantity": 5})
		cashierRole := harness.Call(http.MethodPost, "/api/roles", company.OwnerToken, map[string]any{"name": "Cashier", "permission_ids": []string{"sales:create"}})
		harness.Call(http.MethodPost, "/api/users", company.OwnerToken, map[string]any{"name": "Cashier", "email": "cashier@checklist.test", "password": "cashier-password", "role_id": cashierRole.Data()["id"]})
		sell(t, harness, company.OwnerToken, "checklist-sale", productId, 1, []map[string]any{{"method": "cash", "amount": 1000}})
		settingsResponse := harness.Call(http.MethodPut, "/api/settings", company.OwnerToken, map[string]any{"business_phone": "0713000000", "business_address": "Kariakoo"})
		if settingsResponse.Status != http.StatusOK {
			t.Fatalf("save business details returned %d: %v", settingsResponse.Status, settingsResponse.Body)
		}

		done := gettingStartedOf(t, harness, company.OwnerToken)
		for _, item := range []string{"business_details", "first_product", "first_cashier", "first_sale"} {
			if done[item] != true {
				t.Fatalf("%s not done after setup: %v", item, done)
			}
		}
		if other := gettingStartedOf(t, harness, otherCompany.OwnerToken); other["first_product"] != false || other["first_sale"] != false {
			t.Fatalf("another company's setup leaked: %v", other)
		}
	})
}
