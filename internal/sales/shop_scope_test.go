package sales_test

import (
	"net/http"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/google/uuid"
)

func TestStaffOnlyOpenSalesFromTheirShops(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Two Shops", "owner@twoshops.test")
		branchShop := harness.Call(http.MethodPost, "/api/shops", company.OwnerToken, map[string]any{"name": "Branch"})
		if branchShop.Status != http.StatusCreated {
			t.Fatalf("create branch returned %d %v", branchShop.Status, branchShop.Body)
		}
		branchShopId := uuid.MustParse(branchShop.Data()["id"].(string))

		sellerPermissions := []string{"sales:create", "sales:view", "sales:delete"}
		mainCashierToken := harness.CreateStaff(company, "main@twoshops.test", sellerPermissions, []uuid.UUID{company.ShopId})
		branchCashierToken := harness.CreateStaff(company, "branch@twoshops.test", sellerPermissions, []uuid.UUID{branchShopId})

		product := harness.Call(http.MethodPost, "/api/products", company.OwnerToken, map[string]any{"sku": "MAIN-1", "name": "Main", "price": 1000, "opening_quantity": 5})
		if product.Status != http.StatusCreated {
			t.Fatalf("create product returned %d %v", product.Status, product.Body)
		}
		sold := harness.Call(http.MethodPost, "/api/sales", mainCashierToken, map[string]any{
			"client_ref": "scope-sale-1",
			"items":      []map[string]any{{"product_id": product.Data()["id"], "quantity": 1}},
			"payments":   []map[string]any{{"method": "cash", "amount": 1000}},
		})
		if sold.Status != http.StatusCreated {
			t.Fatalf("sale returned %d %v", sold.Status, sold.Body)
		}
		saleId := sold.Data()["id"].(string)

		for _, path := range []string{"/api/sales/" + saleId, "/api/sales/" + saleId + "/receipt"} {
			if harness.Call(http.MethodGet, path, branchCashierToken, nil).Status != http.StatusNotFound {
				t.Fatalf("a cashier from another shop opened %s", path)
			}
			if harness.Call(http.MethodGet, path, mainCashierToken, nil).Status != http.StatusOK {
				t.Fatalf("the shop's own cashier could not open %s", path)
			}
			if harness.Call(http.MethodGet, path, company.OwnerToken, nil).Status != http.StatusOK {
				t.Fatalf("the owner could not open %s", path)
			}
		}

		voidBody := map[string]any{"reason": "Wrong shop"}
		if harness.Call(http.MethodPost, "/api/sales/"+saleId+"/void", branchCashierToken, voidBody).Status != http.StatusNotFound {
			t.Fatal("a cashier from another shop voided the sale")
		}
		if harness.Call(http.MethodPost, "/api/sales/"+saleId+"/void", mainCashierToken, voidBody).Status != http.StatusOK {
			t.Fatal("the shop's own cashier could not void the sale")
		}
	})
}
