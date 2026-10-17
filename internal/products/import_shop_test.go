package products_test

import (
	"net/http"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/google/uuid"
)

func TestImportPutsOpeningStockInTheChosenShop(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Import Shops", "owner@importshops.test")
		branch := harness.Call(http.MethodPost, "/api/shops", company.OwnerToken, map[string]any{"name": "Branch"})
		if branch.Status != http.StatusCreated {
			t.Fatalf("create branch returned %d %v", branch.Status, branch.Body)
		}
		branchId := branch.Data()["id"].(string)
		importInto := func(sessionToken string, shopId string, csvText string) apptest.Response {
			formValues := map[string]string{}
			if shopId != "" {
				formValues["shop_id"] = shopId
			}
			return harness.UploadForm("/api/products/upload", sessionToken, "file", "shop.csv", []byte(csvText), formValues, nil)
		}
		stockIn := func(shopId string, sku string) int64 {
			return harness.QueryIntForCompany(company.Id, `
				SELECT COALESCE(SUM(ss.quantity), 0) FROM shop_stock ss
				JOIN products p ON p.company_id = ss.company_id AND p.id = ss.product_id
				WHERE ss.company_id = $1 AND ss.shop_id = $2 AND p.sku = $3`, company.Id, shopId, sku)
		}

		currentShopImport := importInto(company.OwnerToken, "", "name,sku,price,quantity\nRice,RICE,3000,7\n")
		if currentShopImport.Status != http.StatusCreated || stockIn(company.ShopId.String(), "RICE") != 7 {
			t.Fatalf("import without a shop returned %d %v", currentShopImport.Status, currentShopImport.Body)
		}

		branchImport := importInto(company.OwnerToken, branchId, "name,sku,price,quantity\nBeans,BEANS,2500,9\n")
		if branchImport.Status != http.StatusCreated || stockIn(branchId, "BEANS") != 9 || stockIn(company.ShopId.String(), "BEANS") != 0 {
			t.Fatalf("import into the branch returned %d %v", branchImport.Status, branchImport.Body)
		}

		mainOnlyToken := harness.CreateStaff(company, "stocker@importshops.test", []string{"products:create", "products:view"}, []uuid.UUID{company.ShopId})
		if importInto(mainOnlyToken, branchId, "name,sku,price,quantity\nSalt,SALT,500,4\n").Code() != "shop_not_assigned" {
			t.Fatal("staff imported stock into a shop they do not work in")
		}
		if importInto(company.OwnerToken, "not-a-shop", "name,sku,price,quantity\nSalt,SALT,500,4\n").Status != http.StatusBadRequest {
			t.Fatal("an invalid shop id was accepted")
		}
		if harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM products WHERE company_id = $1 AND sku = 'SALT'`, company.Id) != 0 {
			t.Fatal("a refused import created products")
		}
	})
}
