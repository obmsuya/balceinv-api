package shops_test

import (
	"net/http"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/google/uuid"
)

func TestOnlyUnusedShopsCanBeDeletedPermanently(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Delete Shops", "owner@deleteshops.test")
		newShop := func(name string) string {
			return harness.Call(http.MethodPost, "/api/shops", company.OwnerToken, map[string]any{"name": name}).Data()["id"].(string)
		}
		deleteShop := func(sessionToken string, shopId string) apptest.Response {
			return harness.Call(http.MethodPost, "/api/shops/"+shopId+"/delete", sessionToken, nil)
		}

		typoId := newShop("Typo shop")
		deleted := deleteShop(company.OwnerToken, typoId)
		if deleted.Status != http.StatusOK || harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM shops WHERE id = $1`, typoId) != 0 {
			t.Fatalf("deleting an unused shop returned %d %v", deleted.Status, deleted.Body)
		}

		if deleteShop(company.OwnerToken, company.ShopId.String()).Code() != "shop_is_current" {
			t.Fatal("the shop being worked in was deleted")
		}

		usedId := newShop("Used shop")
		usedShopId := uuid.MustParse(usedId)
		stockerToken := harness.CreateStaff(company, "stocker@deleteshops.test", []string{"products:create", "products:view"}, []uuid.UUID{usedShopId})
		harness.Call(http.MethodPost, "/api/products", stockerToken, map[string]any{"sku": "USED-1", "name": "Used", "price": 1000, "opening_quantity": 2})
		if deleteShop(company.OwnerToken, usedId).Code() != "shop_in_use" {
			t.Fatal("a shop with stock history was deleted")
		}

		staffedId := newShop("Staffed shop")
		harness.CreateStaff(company, "only@deleteshops.test", []string{"sales:create"}, []uuid.UUID{uuid.MustParse(staffedId)})
		if deleteShop(company.OwnerToken, staffedId).Code() != "shop_has_staff" {
			t.Fatal("a shop whose staff work nowhere else was deleted")
		}

		otherCompany := harness.CreateCompany("Other Delete", "owner@otherdelete.test")
		spareId := newShop("Spare shop")
		if deleteShop(otherCompany.OwnerToken, spareId).Status != http.StatusNotFound {
			t.Fatal("another business deleted this shop")
		}
		if deleteShop(stockerToken, spareId).Status != http.StatusForbidden {
			t.Fatal("staff without shops:delete deleted a shop")
		}
	})
}
