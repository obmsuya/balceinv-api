package products_test

import (
	"net/http"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/google/uuid"
)

func TestOpeningStockGoesToTheChosenShop(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Two Shops", "owner@twoshops.test")
		branchId := harness.Call(http.MethodPost, "/api/shops", company.OwnerToken, map[string]any{"name": "Branch"}).Data()["id"].(string)
		clerkToken := harness.CreateStaff(company, "clerk@twoshops.test", []string{"products:create", "products:view"}, []uuid.UUID{company.ShopId})

		branchProduct := createProduct(t, harness, company.OwnerToken, productBody("BRANCH-1", 1000, map[string]any{"opening_quantity": 7, "shop_id": branchId}))
		if harness.QueryIntForCompany(company.Id, `SELECT COALESCE(SUM(quantity), 0) FROM shop_stock WHERE product_id = $1 AND shop_id = $2`, branchProduct["id"], branchId) != 7 {
			t.Fatal("the opening stock did not land in the chosen shop")
		}
		if harness.QueryIntForCompany(company.Id, `SELECT COALESCE(SUM(quantity), 0) FROM shop_stock WHERE product_id = $1 AND shop_id = $2`, branchProduct["id"], company.ShopId) != 0 {
			t.Fatal("the opening stock also landed in the main shop")
		}

		notMine := harness.Call(http.MethodPost, "/api/products", clerkToken, productBody("BRANCH-2", 1000, map[string]any{"opening_quantity": 3, "shop_id": branchId}))
		if notMine.Code() != "shop_not_assigned" {
			t.Fatalf("stocking a shop the clerk does not work in returned %d %v", notMine.Status, notMine.Body)
		}
		mine := harness.Call(http.MethodPost, "/api/products", clerkToken, productBody("MAIN-1", 1000, map[string]any{"opening_quantity": 3, "shop_id": company.ShopId.String()}))
		if mine.Status != http.StatusCreated {
			t.Fatalf("stocking the clerk's own shop returned %d %v", mine.Status, mine.Body)
		}

		otherCompany := harness.CreateCompany("Other Shops", "owner@othershops.test")
		foreign := harness.Call(http.MethodPost, "/api/products", otherCompany.OwnerToken, productBody("FOREIGN-1", 1000, map[string]any{"opening_quantity": 1, "shop_id": branchId}))
		if foreign.Code() != "shop_not_assigned" {
			t.Fatalf("stocking another business's shop returned %d %v", foreign.Status, foreign.Body)
		}
	})
}
