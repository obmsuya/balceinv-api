package sales_test

import (
	"net/http"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/google/uuid"
)

func TestSalesListShowsTheShopOfEachSale(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Shop Sales", "owner@shopsales.test")
		branchId := mustData(t, harness.Call(http.MethodPost, "/api/shops", company.OwnerToken, map[string]any{"name": "Branch"}), http.StatusCreated)["id"].(string)

		sellIn := func(shopId string, clientRef string) {
			mustData(t, harness.Call(http.MethodPost, "/api/auth/switch-shop", company.OwnerToken, map[string]any{"shop_id": shopId}), http.StatusOK)
			productId := mustData(t, harness.Call(http.MethodPost, "/api/products", company.OwnerToken, map[string]any{"sku": clientRef, "name": clientRef, "price": 1000, "opening_quantity": 3}), http.StatusCreated)["id"]
			mustData(t, harness.Call(http.MethodPost, "/api/sales", company.OwnerToken, map[string]any{
				"client_ref": clientRef,
				"items":      []map[string]any{{"product_id": productId, "quantity": 1}},
				"payments":   []map[string]any{{"method": "cash", "amount": 1000}},
			}), http.StatusCreated)
		}
		sellIn(company.ShopId.String(), "main-sale")
		sellIn(branchId, "branch-sale")

		currentShopSales := harness.Call(http.MethodGet, "/api/sales", company.OwnerToken, nil).Items()
		if len(currentShopSales) != 1 || currentShopSales[0].(map[string]any)["shop_name"] != "Branch" {
			t.Fatalf("the current shop's list was %v", currentShopSales)
		}

		everySale := harness.Call(http.MethodGet, "/api/sales?shop=all", company.OwnerToken, nil)
		shopNames := map[string]bool{}
		for _, sale := range everySale.Items() {
			shopNames[sale.(map[string]any)["shop_name"].(string)] = true
		}
		if len(everySale.Items()) != 2 || !shopNames["Branch"] || len(shopNames) != 2 {
			t.Fatalf("all shops listed %v", everySale.Items())
		}
		if total := harness.Call(http.MethodGet, "/api/sales/totals?shop=all", company.OwnerToken, nil).Data()["sale_count"]; total != float64(2) {
			t.Fatalf("all-shop totals counted %v sales", total)
		}

		mainCashierToken := harness.CreateStaff(company, "cashier@shopsales.test", []string{"sales:view"}, []uuid.UUID{company.ShopId})
		if staffSales := harness.Call(http.MethodGet, "/api/sales?shop=all", mainCashierToken, nil).Items(); len(staffSales) != 1 {
			t.Fatalf("staff saw %d sales across shops, want only their shop's", len(staffSales))
		}
		if harness.Call(http.MethodGet, "/api/sales?shop="+branchId, mainCashierToken, nil).Code() != "shop_not_assigned" {
			t.Fatal("staff listed sales of a shop they do not work in")
		}
	})
}

func mustData(t *testing.T, called apptest.Response, wantStatus int) map[string]any {
	t.Helper()
	if called.Status != wantStatus {
		t.Fatalf("call returned %d, want %d: %v", called.Status, wantStatus, called.Body)
	}
	return called.Data()
}
