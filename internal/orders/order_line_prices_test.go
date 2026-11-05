package orders_test

import (
	"net/http"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/google/uuid"
)

func TestOrderLinesTakeAnExtraAndACashierDiscountIntoTheSale(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Order Prices", "owner@orderprices.test")
		harness.Call(http.MethodPut, "/api/features", company.OwnerToken, map[string]any{"customers_enabled": true, "customer_orders_enabled": true})
		clerkToken := harness.CreateStaff(company, "clerk@orderprices.test", []string{"orders:view", "orders:create", "orders:edit"}, []uuid.UUID{company.ShopId})
		customerId := harness.Call(http.MethodPost, "/api/customers", company.OwnerToken, map[string]any{"name": "Baraka"}).Data()["id"].(string)
		chairId := newProduct(t, harness, company.OwnerToken, "CHAIR", 20000, 5)

		raisedLine := map[string]any{"product_id": chairId, "quantity": 2, "extra": map[string]any{"kind": "amount", "value": 3000}}
		discountedLine := map[string]any{"product_id": chairId, "quantity": 1, "manual_discount": map[string]any{"kind": "amount", "value": 2000}}

		refusedDiscount := harness.Call(http.MethodPost, "/api/orders", clerkToken, map[string]any{"customer_id": customerId, "items": []any{discountedLine}})
		if refusedDiscount.Status != http.StatusForbidden || refusedDiscount.Code() != "till_discount_not_allowed" {
			t.Fatalf("a clerk without the discount permission got %d %v", refusedDiscount.Status, refusedDiscount.Body)
		}

		clerkExtra := harness.Call(http.MethodPost, "/api/orders", clerkToken, map[string]any{"customer_id": customerId, "items": []any{raisedLine}})
		if clerkExtra.Status != http.StatusCreated || clerkExtra.Data()["total"] != float64(46000) {
			t.Fatalf("a clerk's extra on an order returned %d %v", clerkExtra.Status, clerkExtra.Body)
		}

		created := harness.Call(http.MethodPost, "/api/orders", company.OwnerToken, map[string]any{"customer_id": customerId, "items": []any{raisedLine, discountedLine}})
		order := created.Data()
		if created.Status != http.StatusCreated || order["total"] != float64(64000) || order["discount_total"] != float64(2000) {
			t.Fatalf("an order with an extra and a discount returned %d %v", created.Status, created.Body)
		}
		orderLines := order["lines"].([]any)
		if orderLines[0].(map[string]any)["extra_unit_amount"] != float64(3000) || orderLines[1].(map[string]any)["manual_discount_amount"] != float64(2000) {
			t.Fatalf("the order lines are %v", orderLines)
		}

		collected := harness.Call(http.MethodPost, "/api/orders/"+order["id"].(string)+"/collect", company.OwnerToken, map[string]any{"payments": []any{map[string]any{"method": "cash", "amount": 64000}}})
		if collected.Status != http.StatusOK {
			t.Fatalf("collecting returned %d %v", collected.Status, collected.Body)
		}
		sale := harness.Call(http.MethodGet, "/api/sales/"+collected.Data()["sale_id"].(string), company.OwnerToken, nil).Data()
		saleLines := sale["items"].([]any)
		if sale["total"] != float64(64000) || saleLines[0].(map[string]any)["extra_unit_amount"] != float64(3000) || saleLines[1].(map[string]any)["manual_discount_amount"] != float64(2000) {
			t.Fatalf("the collected sale is %v", sale)
		}
	})
}

func TestOrderWingaLinesReachTheCollectedSale(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Order Winga", "owner@orderwinga.test")
		harness.Call(http.MethodPut, "/api/features", company.OwnerToken, map[string]any{"customers_enabled": true, "customer_orders_enabled": true})
		customerId := harness.Call(http.MethodPost, "/api/customers", company.OwnerToken, map[string]any{"name": "Zawadi"}).Data()["id"].(string)
		sofaLine := map[string]any{"name": "Sofa from the market", "quantity": 1, "unit_price": 300000, "winga_share": 250000}

		turnedOff := harness.Call(http.MethodPost, "/api/orders", company.OwnerToken, map[string]any{"customer_id": customerId, "winga_lines": []any{sofaLine}})
		if turnedOff.Status != http.StatusConflict || turnedOff.Code() != "winga_off" {
			t.Fatalf("a winga order while the feature is off returned %d %v", turnedOff.Status, turnedOff.Body)
		}
		harness.Call(http.MethodPut, "/api/features", company.OwnerToken, map[string]any{"winga_enabled": true})

		created := harness.Call(http.MethodPost, "/api/orders", company.OwnerToken, map[string]any{"customer_id": customerId, "winga_lines": []any{sofaLine}})
		order := created.Data()
		if created.Status != http.StatusCreated || order["total"] != float64(300000) || len(order["winga_lines"].([]any)) != 1 {
			t.Fatalf("a winga order returned %d %v", created.Status, created.Body)
		}
		listedOrder := harness.Call(http.MethodGet, "/api/orders", company.OwnerToken, nil).Items()[0].(map[string]any)
		if listedOrder["line_count"] != float64(1) || order["line_count"] != float64(1) {
			t.Fatalf("a Brk-only order counts %v lines in the list and %v in the order", listedOrder["line_count"], order["line_count"])
		}

		collected := harness.Call(http.MethodPost, "/api/orders/"+order["id"].(string)+"/collect", company.OwnerToken, map[string]any{"payments": []any{map[string]any{"method": "cash", "amount": 300000}}})
		if collected.Status != http.StatusOK {
			t.Fatalf("collecting a winga order returned %d %v", collected.Status, collected.Body)
		}
		sale := harness.Call(http.MethodGet, "/api/sales/"+collected.Data()["sale_id"].(string), company.OwnerToken, nil).Data()
		saleWinga := sale["winga_lines"].([]any)
		if sale["total"] != float64(300000) || len(saleWinga) != 1 || saleWinga[0].(map[string]any)["winga_share"] != float64(250000) {
			t.Fatalf("the collected winga sale is %v", sale)
		}
	})
}
