package stock_test

import (
	"net/http"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/google/uuid"
)

func TestOwnerSeesStockInEveryShop(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Stock Shops", "owner@stockshops.test")
		branch := harness.Call(http.MethodPost, "/api/shops", company.OwnerToken, map[string]any{"name": "Branch"})
		if branch.Status != http.StatusCreated {
			t.Fatalf("create branch returned %d %v", branch.Status, branch.Body)
		}
		branchId := branch.Data()["id"].(string)

		product := harness.Call(http.MethodPost, "/api/products", company.OwnerToken, map[string]any{"sku": "RICE", "name": "Rice", "price": 3000, "opening_quantity": 10})
		if product.Status != http.StatusCreated {
			t.Fatalf("create product returned %d %v", product.Status, product.Body)
		}
		productId := product.Data()["id"].(string)

		sent := harness.Call(http.MethodPost, "/api/stock-transfers", company.OwnerToken, map[string]any{
			"to_shop_id": branchId,
			"items":      []map[string]any{{"product_id": productId, "quantity": 4}},
		})
		if sent.Status != http.StatusCreated {
			t.Fatalf("transfer returned %d %v", sent.Status, sent.Body)
		}

		currentShop := harness.Call(http.MethodGet, "/api/stock?product_id="+productId, company.OwnerToken, nil).Items()
		if len(currentShop) != 1 || currentShop[0].(map[string]any)["quantity"] != float64(6) || currentShop[0].(map[string]any)["shops"] != nil {
			t.Fatalf("the current shop showed %v", currentShop)
		}

		everyShop := harness.Call(http.MethodGet, "/api/stock?shop=all&product_id="+productId, company.OwnerToken, nil).Items()
		if len(everyShop) != 1 {
			t.Fatalf("all shops listed %v", everyShop)
		}
		allShopsLevel := everyShop[0].(map[string]any)
		quantityByShop := map[string]float64{}
		for _, shopLevel := range allShopsLevel["shops"].([]any) {
			quantityByShop[shopLevel.(map[string]any)["shop_name"].(string)] = shopLevel.(map[string]any)["quantity"].(float64)
		}
		if allShopsLevel["quantity"] != float64(10) || quantityByShop["Branch"] != 4 || len(quantityByShop) != 2 {
			t.Fatalf("all shops showed %v", allShopsLevel)
		}

		branchOnly := harness.Call(http.MethodGet, "/api/stock?shop="+branchId+"&product_id="+productId, company.OwnerToken, nil).Items()
		if len(branchOnly) != 1 || branchOnly[0].(map[string]any)["quantity"] != float64(4) {
			t.Fatalf("the branch showed %v", branchOnly)
		}

		summary := harness.Call(http.MethodGet, "/api/stock/summary?shop=all", company.OwnerToken, nil).Data()
		if summary["total_units"] != float64(10) {
			t.Fatalf("the all-shop summary was %v", summary)
		}

		movementShops := map[string]bool{}
		for _, movement := range harness.Call(http.MethodGet, "/api/stock-movements?shop=all", company.OwnerToken, nil).Items() {
			movementShops[movement.(map[string]any)["shop_name"].(string)] = true
		}
		if !movementShops["Branch"] || len(movementShops) != 2 {
			t.Fatalf("stock history across shops named %v", movementShops)
		}

		mainOnlyToken := harness.CreateStaff(company, "keeper@stockshops.test", []string{"stock_movements:view"}, []uuid.UUID{company.ShopId})
		if harness.Call(http.MethodGet, "/api/stock?shop="+branchId, mainOnlyToken, nil).Code() != "shop_not_assigned" {
			t.Fatal("staff saw stock of a shop they do not work in")
		}
		staffAll := harness.Call(http.MethodGet, "/api/stock?shop=all&product_id="+productId, mainOnlyToken, nil).Items()
		if len(staffAll) != 1 || staffAll[0].(map[string]any)["quantity"] != float64(6) {
			t.Fatalf("staff all-shops view was %v, want only their shop", staffAll)
		}
	})
}
