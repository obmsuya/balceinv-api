package sales_test

import (
	"net/http"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/google/uuid"
)

func discountedLine(productId string, quantity int, kind string, value int64) map[string]any {
	return map[string]any{"product_id": productId, "quantity": quantity, "manual_discount": map[string]any{"kind": kind, "value": value}}
}

func TestCashierDiscountNeedsOnlyThePermissionAndCanGoToTheFullPrice(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Discount Shop", "owner@discount.test")
		plainCashierToken := harness.CreateStaff(company, "plain@discount.test", []string{"sales:create"}, []uuid.UUID{company.ShopId})
		trustedCashierToken := harness.CreateStaff(company, "trusted@discount.test", []string{"sales:create", "till_discounts:create"}, []uuid.UUID{company.ShopId})
		riceId := newProduct(t, harness, company.OwnerToken, map[string]any{"sku": "RICE", "name": "Rice", "price": 10000, "opening_quantity": 50})

		quoteWithoutPermission := harness.Call(http.MethodPost, "/api/sales/quote", plainCashierToken, map[string]any{"items": []any{discountedLine(riceId, 1, "percent", 1000)}})
		saleWithoutPermission := sell(harness, plainCashierToken, "discount-plain-0001", []map[string]any{discountedLine(riceId, 1, "percent", 1000)}, cash(10000))
		if quoteWithoutPermission.Status != http.StatusForbidden || quoteWithoutPermission.Code() != "till_discount_not_allowed" ||
			saleWithoutPermission.Status != http.StatusForbidden || saleWithoutPermission.Code() != "till_discount_not_allowed" {
			t.Fatalf("a cashier without the permission got quote %d %v and sale %d %v", quoteWithoutPermission.Status, quoteWithoutPermission.Body, saleWithoutPermission.Status, saleWithoutPermission.Body)
		}

		allowedSale := sell(harness, trustedCashierToken, "discount-allowed-0001", []map[string]any{discountedLine(riceId, 2, "percent", 10000), discountedLine(riceId, 1, "amount", 1000)}, cash(9000))
		allowedData := allowedSale.Data()
		if allowedSale.Status != http.StatusCreated || allowedData["discount_total"] != float64(21000) || allowedData["total"] != float64(9000) {
			t.Fatalf("a full and a partial cashier discount returned %d %v", allowedSale.Status, allowedSale.Body)
		}

		savedSale := harness.Call(http.MethodGet, "/api/sales/"+allowedData["id"].(string), company.OwnerToken, nil)
		savedLines := savedSale.Data()["items"].([]any)
		firstLine := savedLines[0].(map[string]any)
		secondLine := savedLines[1].(map[string]any)
		if firstLine["manual_discount_amount"] != float64(20000) || firstLine["line_total"] != float64(0) ||
			secondLine["manual_discount_amount"] != float64(1000) || secondLine["line_total"] != float64(9000) || savedSale.Data()["cashier_name"] == nil {
			t.Fatalf("the saved sale shows %v", savedSale.Body)
		}

		ownerSale := sell(harness, company.OwnerToken, "discount-owner-0001", []map[string]any{discountedLine(riceId, 1, "percent", 5000)}, cash(5000))
		if ownerSale.Status != http.StatusCreated || ownerSale.Data()["total"] != float64(5000) {
			t.Fatalf("the owner giving 50%% returned %d %v", ownerSale.Status, ownerSale.Body)
		}

		badKind := harness.Call(http.MethodPost, "/api/sales/quote", company.OwnerToken, map[string]any{"items": []any{discountedLine(riceId, 1, "free", 1)}})
		if badKind.Status != http.StatusBadRequest {
			t.Fatalf("an unknown discount kind returned %d %v", badKind.Status, badKind.Body)
		}
	})
}
