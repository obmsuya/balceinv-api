package transfers_test

import (
	"net/http"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/google/uuid"
)

func newProduct(t *testing.T, harness *apptest.Harness, sessionToken string, sku string, openingQuantity int, minimumStock int) string {
	t.Helper()
	createResponse := harness.Call(http.MethodPost, "/api/products", sessionToken, map[string]any{
		"sku": sku, "name": "Product " + sku, "price": 1000, "opening_quantity": openingQuantity, "min_stock": minimumStock,
	})
	if createResponse.Status != http.StatusCreated {
		t.Fatalf("create %s returned %d: %v", sku, createResponse.Status, createResponse.Body)
	}
	return createResponse.Data()["id"].(string)
}

func item(productId string, quantity int) map[string]any {
	return map[string]any{"product_id": productId, "quantity": quantity}
}

func TestTransfersMoveStockBetweenShops(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Two Shops", "owner@twoshops.test")
		otherCompany := harness.CreateCompany("Far Away", "owner@faraway.test")

		branchId := harness.Call(http.MethodPost, "/api/shops", company.OwnerToken, map[string]any{"name": "Branch"}).Data()["id"].(string)
		closedId := harness.Call(http.MethodPost, "/api/shops", company.OwnerToken, map[string]any{"name": "Closed"}).Data()["id"].(string)
		harness.Call(http.MethodDelete, "/api/shops/"+closedId, company.OwnerToken, nil)
		foreignShopId := otherCompany.ShopId.String()

		riceId := newProduct(t, harness, company.OwnerToken, "RICE", 10, 3)
		oilId := newProduct(t, harness, company.OwnerToken, "OIL", 4, 2)
		foreignProductId := newProduct(t, harness, otherCompany.OwnerToken, "FOREIGN", 5, 1)

		stockIn := func(shopId string, productId string) int64 {
			return harness.QueryIntForCompany(company.Id, `SELECT COALESCE(SUM(quantity), 0) FROM shop_stock WHERE shop_id = $1 AND product_id = $2`, shopId, productId)
		}

		sent := harness.Call(http.MethodPost, "/api/stock-transfers", company.OwnerToken, map[string]any{
			"to_shop_id": branchId, "note": " Weekly restock ", "items": []any{item(riceId, 6), item(oilId, 4)},
		})
		sentData := sent.Data()
		if sent.Status != http.StatusCreated || sentData["item_count"] != float64(2) || sentData["total_units"] != float64(10) ||
			sentData["from_shop_name"] != "Main Shop" || sentData["to_shop_name"] != "Branch" || sentData["note"] != "Weekly restock" {
			t.Fatalf("transfer returned %d %v", sent.Status, sent.Body)
		}
		mainShopId := company.ShopId.String()
		if stockIn(mainShopId, riceId) != 4 || stockIn(mainShopId, oilId) != 0 || stockIn(branchId, riceId) != 6 || stockIn(branchId, oilId) != 4 {
			t.Fatal("stock did not move between the shops")
		}
		transferId := sentData["id"].(string)
		linkedMovements := harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM stock_movements WHERE reference = $1 AND reason IN ('transfer_out', 'transfer_in')`, transferId)
		outNotices := harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM notifications WHERE shop_id = $1 AND product_id = $2 AND kind = 'out_of_stock'`, mainShopId, oilId)
		if linkedMovements != 4 || outNotices != 1 {
			t.Fatalf("transfer wrote %d linked movements and %d out-of-stock notices, want 4 and 1", linkedMovements, outNotices)
		}

		transfersBefore := harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM stock_transfers`)
		movementsBefore := harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM stock_movements`)
		tooMuch := harness.Call(http.MethodPost, "/api/stock-transfers", company.OwnerToken, map[string]any{
			"to_shop_id": branchId, "items": []any{item(riceId, 1), item(oilId, 1)},
		})
		if tooMuch.Status != http.StatusConflict || tooMuch.Code() != "insufficient_stock" {
			t.Fatalf("sending missing stock returned %d %s, want 409", tooMuch.Status, tooMuch.Code())
		}
		if harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM stock_transfers`) != transfersBefore ||
			harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM stock_movements`) != movementsBefore || stockIn(mainShopId, riceId) != 4 {
			t.Fatal("a refused transfer still changed something")
		}

		rejected := []struct {
			body       map[string]any
			wantStatus int
		}{
			{map[string]any{"to_shop_id": mainShopId, "items": []any{item(riceId, 1)}}, http.StatusBadRequest},
			{map[string]any{"to_shop_id": branchId, "items": []any{item(riceId, 1), item(riceId, 1)}}, http.StatusBadRequest},
			{map[string]any{"to_shop_id": branchId, "items": []any{}}, http.StatusBadRequest},
			{map[string]any{"to_shop_id": branchId, "items": []any{item(riceId, 0)}}, http.StatusBadRequest},
			{map[string]any{"to_shop_id": "branch", "items": []any{item(riceId, 1)}}, http.StatusBadRequest},
			{map[string]any{"to_shop_id": closedId, "items": []any{item(riceId, 1)}}, http.StatusNotFound},
			{map[string]any{"to_shop_id": foreignShopId, "items": []any{item(riceId, 1)}}, http.StatusNotFound},
			{map[string]any{"to_shop_id": branchId, "items": []any{item(foreignProductId, 1)}}, http.StatusNotFound},
			{map[string]any{"to_shop_id": branchId, "items": []any{item(uuid.Must(uuid.NewV7()).String(), 1)}}, http.StatusNotFound},
		}
		for _, rejectedCase := range rejected {
			rejectedResponse := harness.Call(http.MethodPost, "/api/stock-transfers", company.OwnerToken, rejectedCase.body)
			if rejectedResponse.Status != rejectedCase.wantStatus {
				t.Fatalf("%v returned %d, want %d", rejectedCase.body, rejectedResponse.Status, rejectedCase.wantStatus)
			}
		}

		keeperToken := harness.CreateStaff(company, "keeper@twoshops.test", []string{"stock_movements:view", "stock_movements:create"}, []uuid.UUID{company.ShopId})
		fromUnassigned := harness.Call(http.MethodPost, "/api/stock-transfers", keeperToken, map[string]any{
			"from_shop_id": branchId, "to_shop_id": mainShopId, "items": []any{item(riceId, 1)},
		})
		fromAssigned := harness.Call(http.MethodPost, "/api/stock-transfers", keeperToken, map[string]any{
			"to_shop_id": branchId, "items": []any{item(riceId, 1)},
		})
		if fromUnassigned.Status != http.StatusForbidden || fromUnassigned.Code() != "shop_not_assigned" || fromAssigned.Status != http.StatusCreated {
			t.Fatalf("keeper from branch returned %d, from main %d; want 403 and 201", fromUnassigned.Status, fromAssigned.Status)
		}
		viewerToken := harness.CreateStaff(company, "viewer@twoshops.test", []string{"stock_movements:view"}, []uuid.UUID{company.ShopId})
		if harness.Call(http.MethodPost, "/api/stock-transfers", viewerToken, map[string]any{"to_shop_id": branchId, "items": []any{item(riceId, 1)}}).Status != http.StatusForbidden {
			t.Fatal("a viewer was allowed to send stock")
		}

		mainList := harness.Call(http.MethodGet, "/api/stock-transfers", company.OwnerToken, nil)
		detail := harness.Call(http.MethodGet, "/api/stock-transfers/"+transferId, company.OwnerToken, nil)
		detailItems, _ := detail.Data()["items"].([]any)
		if mainList.Data()["total"] != float64(2) || len(detailItems) != 2 || detailItems[0].(map[string]any)["product_name"] != "Product OIL" {
			t.Fatalf("list total %v, detail items %v", mainList.Data()["total"], detailItems)
		}
		if harness.Call(http.MethodGet, "/api/stock-transfers/"+transferId, otherCompany.OwnerToken, nil).Status != http.StatusNotFound {
			t.Fatal("another company can read this transfer")
		}
		harness.Call(http.MethodPost, "/api/auth/switch-shop", company.OwnerToken, map[string]any{"shop_id": branchId})
		if harness.Call(http.MethodGet, "/api/stock-transfers", company.OwnerToken, nil).Data()["total"] != float64(2) {
			t.Fatal("the receiving shop does not see the transfers")
		}

		mismatches := harness.QueryIntForCompany(company.Id, `
			SELECT COUNT(*) FROM shop_stock ss
			WHERE ss.quantity <> (SELECT COALESCE(SUM(m.change), 0) FROM stock_movements m
			                      WHERE m.company_id = ss.company_id AND m.shop_id = ss.shop_id AND m.product_id = ss.product_id)
		`)
		if mismatches != 0 {
			t.Fatalf("%d shop stock rows disagree with their movements after transfers", mismatches)
		}
	})
}
