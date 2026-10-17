package notifications_test

import (
	"net/http"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/google/uuid"
)

func TestAlertsNameTheirShopAndTheOwnerSeesEveryShop(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Alert Shops", "owner@alertshops.test")
		mustCall := func(method string, path string, sessionToken string, body map[string]any, wantStatus int) map[string]any {
			t.Helper()
			called := harness.Call(method, path, sessionToken, body)
			if called.Status != wantStatus {
				t.Fatalf("%s %s returned %d, want %d: %v", method, path, called.Status, wantStatus, called.Body)
			}
			return called.Data()
		}
		branchId := mustCall(http.MethodPost, "/api/shops", company.OwnerToken, map[string]any{"name": "Branch"}, http.StatusCreated)["id"].(string)
		productId := mustCall(http.MethodPost, "/api/products", company.OwnerToken, map[string]any{"sku": "OIL", "name": "Oil", "price": 5000, "opening_quantity": 10}, http.StatusCreated)["id"].(string)

		mustCall(http.MethodPost, "/api/stock-transfers", company.OwnerToken, map[string]any{
			"to_shop_id": branchId,
			"items":      []map[string]any{{"product_id": productId, "quantity": 8}},
		}, http.StatusCreated)
		mustCall(http.MethodPost, "/api/auth/switch-shop", company.OwnerToken, map[string]any{"shop_id": branchId}, http.StatusOK)
		mustCall(http.MethodPost, "/api/stock-movements", company.OwnerToken, map[string]any{"product_id": productId, "reason": "damage", "change": -4}, http.StatusCreated)

		branchAlerts := harness.Call(http.MethodGet, "/api/notifications", company.OwnerToken, nil).Items()
		if len(branchAlerts) != 1 || branchAlerts[0].(map[string]any)["shop_name"] != "Branch" {
			t.Fatalf("the branch's alerts were %v", branchAlerts)
		}

		alertShops := map[string]bool{}
		for _, alert := range harness.Call(http.MethodGet, "/api/notifications?shop=all", company.OwnerToken, nil).Items() {
			alertShops[alert.(map[string]any)["shop_name"].(string)] = true
		}
		if len(alertShops) != 2 || !alertShops["Branch"] {
			t.Fatalf("all shops' alerts came from %v", alertShops)
		}
		if unread := mustCall(http.MethodGet, "/api/notifications/unread-count?shop=all", company.OwnerToken, nil, http.StatusOK)["count"]; unread != float64(2) {
			t.Fatalf("unread across shops was %v", unread)
		}

		mainOnlyToken := harness.CreateStaff(company, "keeper@alertshops.test", []string{"notifications:view"}, []uuid.UUID{company.ShopId})
		if staffAlerts := harness.Call(http.MethodGet, "/api/notifications?shop=all", mainOnlyToken, nil).Items(); len(staffAlerts) != 1 {
			t.Fatalf("staff saw %d alerts across shops, want only their shop's", len(staffAlerts))
		}
		if harness.Call(http.MethodGet, "/api/notifications?shop="+branchId, mainOnlyToken, nil).Code() != "shop_not_assigned" {
			t.Fatal("staff saw alerts of a shop they do not work in")
		}

		if changed := mustCall(http.MethodPost, "/api/notifications/read-all?shop=all", company.OwnerToken, nil, http.StatusOK)["changed"]; changed != float64(2) {
			t.Fatalf("marking all shops read changed %v", changed)
		}
	})
}
