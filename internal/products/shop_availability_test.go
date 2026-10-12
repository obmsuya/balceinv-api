package products_test

import (
	"net/http"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
)

func listedProductNames(t *testing.T, harness *apptest.Harness, sessionToken string, query string) map[string]bool {
	t.Helper()
	listResponse := harness.Call(http.MethodGet, "/api/products"+query, sessionToken, nil)
	if listResponse.Status != http.StatusOK {
		t.Fatalf("listing products returned %d %v", listResponse.Status, listResponse.Body)
	}
	names := map[string]bool{}
	for _, rawProduct := range listResponse.Items() {
		names[rawProduct.(map[string]any)["name"].(string)] = true
	}
	return names
}

func TestEachShopSellsOnlyTheProductsItCarries(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Two Shop Duka", "owner@twoshopduka.test")
		branchId := harness.Call(http.MethodPost, "/api/shops", company.OwnerToken, map[string]any{"name": "Branch"}).Data()["id"].(string)
		mainId := company.ShopId.String()

		sugar := createProduct(t, harness, company.OwnerToken, map[string]any{"sku": "SUGAR", "name": "Sugar", "price": 3500, "opening_quantity": 5})
		paint := createProduct(t, harness, company.OwnerToken, map[string]any{"sku": "PAINT", "name": "Paint", "price": 20000, "shop_ids": []string{branchId}})
		paintId := paint["id"].(string)

		mainNames := listedProductNames(t, harness, company.OwnerToken, "")
		if !mainNames["Sugar"] || mainNames["Paint"] {
			t.Fatalf("the main shop lists %v, want Sugar without Paint", mainNames)
		}
		allNames := listedProductNames(t, harness, company.OwnerToken, "?include_other_shops=true")
		if !allNames["Paint"] {
			t.Fatalf("showing all products left out Paint: %v", allNames)
		}
		paintDetail := harness.Call(http.MethodGet, "/api/products/"+paintId, company.OwnerToken, nil).Data()
		hiddenShops, _ := paintDetail["hidden_shop_ids"].([]any)
		if paintDetail["in_shop"] != false || len(hiddenShops) != 1 || hiddenShops[0] != mainId {
			t.Fatalf("Paint's detail was %v", paintDetail)
		}

		paintSale := harness.Call(http.MethodPost, "/api/sales", company.OwnerToken, map[string]any{
			"client_ref": "shop-products-sale-1", "items": []map[string]any{{"product_id": paintId, "quantity": 1}}, "payments": []map[string]any{{"method": "cash", "amount": 20000}},
		})
		if paintSale.Code() != "product_not_in_shop" {
			t.Fatalf("selling Paint in the main shop returned %d %v", paintSale.Status, paintSale.Body)
		}
		if harness.Call(http.MethodGet, "/api/products/lookup?code=PAINT", company.OwnerToken, nil).Code() != "product_not_in_shop" {
			t.Fatal("scanning Paint in the main shop found it")
		}

		variant := createProduct(t, harness, company.OwnerToken, map[string]any{"sku": "PAINT-RED", "name": "Paint", "variant_label": "Red", "price": 21000, "parent_id": paintId})
		if harness.Call(http.MethodGet, "/api/products/"+variant["id"].(string), company.OwnerToken, nil).Data()["in_shop"] != false {
			t.Fatal("a new variant of Paint showed in a shop that does not carry Paint")
		}

		keptSugar := harness.Call(http.MethodPost, "/api/products/shops", company.OwnerToken, map[string]any{"product_ids": []string{sugar["id"].(string)}, "shop_id": mainId, "available": false})
		kept, _ := keptSugar.Data()["kept_with_stock"].([]any)
		if keptSugar.Status != http.StatusOK || keptSugar.Data()["changed"] != float64(0) || len(kept) != 1 {
			t.Fatalf("removing Sugar from a shop that has 5 returned %d %v", keptSugar.Status, keptSugar.Body)
		}
		removedFromBranch := harness.Call(http.MethodPost, "/api/products/shops", company.OwnerToken, map[string]any{"product_ids": []string{sugar["id"].(string)}, "shop_id": branchId, "available": false})
		if removedFromBranch.Data()["changed"] != float64(1) {
			t.Fatalf("removing Sugar from the empty branch returned %v", removedFromBranch.Body)
		}
		if harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM shop_stock WHERE product_id = $1 AND shop_id = $2 AND NOT is_listed`, sugar["id"], branchId) != 1 {
			t.Fatal("Sugar is still listed in the branch")
		}

		arrived := harness.Call(http.MethodPost, "/api/stock-movements", company.OwnerToken, map[string]any{"product_id": paintId, "reason": "purchase", "change": 4})
		if arrived.Status >= 300 {
			t.Fatalf("receiving Paint returned %d %v", arrived.Status, arrived.Body)
		}
		if !listedProductNames(t, harness, company.OwnerToken, "")["Paint"] {
			t.Fatal("Paint did not appear in the main shop after stock arrived there")
		}

		movedAway := harness.Call(http.MethodPut, "/api/products/"+paintId, company.OwnerToken, map[string]any{"sku": "PAINT", "name": "Paint", "price": 20000, "shop_ids": []string{branchId}})
		if movedAway.Code() != "shop_has_stock" {
			t.Fatalf("removing Paint from a shop holding 4 returned %d %v", movedAway.Status, movedAway.Body)
		}

		otherCompany := harness.CreateCompany("Other Duka", "owner@otherduka.test")
		foreign := harness.Call(http.MethodPost, "/api/products/shops", otherCompany.OwnerToken, map[string]any{"product_ids": []string{paintId}, "shop_id": branchId, "available": true})
		if foreign.Code() != "shop_not_found" {
			t.Fatalf("another business changing this shop returned %d %v", foreign.Status, foreign.Body)
		}
	})
}
