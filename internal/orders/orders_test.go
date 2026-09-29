package orders_test

import (
	"net/http"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/google/uuid"
)

func newProduct(t *testing.T, harness *apptest.Harness, ownerToken string, sku string, price int64, openingQuantity int) string {
	t.Helper()
	created := harness.Call(http.MethodPost, "/api/products", ownerToken, map[string]any{"sku": sku, "name": sku, "price": price, "opening_quantity": openingQuantity})
	if created.Status != http.StatusCreated {
		t.Fatalf("create %s returned %d %v", sku, created.Status, created.Body)
	}
	return created.Data()["id"].(string)
}

func orderLine(productId string, quantity int) map[string]any {
	return map[string]any{"product_id": productId, "quantity": quantity}
}

func TestCustomerOrdersAreOffUntilTurnedOn(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Orderless Shop", "owner@orderless.test")
		harness.Call(http.MethodPut, "/api/features", company.OwnerToken, map[string]any{"customers_enabled": true})
		someId := uuid.Must(uuid.NewV7()).String()

		for _, entryPoint := range []struct{ method, path string }{
			{http.MethodGet, "/api/orders"},
			{http.MethodPost, "/api/orders"},
			{http.MethodGet, "/api/orders/" + someId},
			{http.MethodPost, "/api/orders/" + someId + "/deposits"},
			{http.MethodPost, "/api/orders/" + someId + "/ready"},
			{http.MethodPost, "/api/orders/" + someId + "/collect"},
			{http.MethodPost, "/api/orders/" + someId + "/cancel"},
		} {
			refused := harness.Call(entryPoint.method, entryPoint.path, company.OwnerToken, map[string]any{})
			if refused.Status != http.StatusForbidden || refused.Code() != "feature_off" {
				t.Fatalf("%s %s with orders off returned %d %s", entryPoint.method, entryPoint.path, refused.Status, refused.Code())
			}
		}
	})
}

func TestOrdersReserveStockTakeDepositsAndBecomeOneSale(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Cake Shop", "owner@cake.test")
		otherCompany := harness.CreateCompany("Other Cakes", "owner@othercake.test")
		harness.Call(http.MethodPut, "/api/features", company.OwnerToken, map[string]any{"customers_enabled": true, "customer_orders_enabled": true, "credit_sales_enabled": true})
		harness.Call(http.MethodPut, "/api/features", otherCompany.OwnerToken, map[string]any{"customers_enabled": true, "customer_orders_enabled": true})
		cashierToken := harness.CreateStaff(company, "cashier@cake.test", []string{"sales:create", "orders:view"}, []uuid.UUID{company.ShopId})
		clerkToken := harness.CreateStaff(company, "clerk@cake.test", []string{"orders:view", "orders:create", "orders:edit"}, []uuid.UUID{company.ShopId})

		cakeId := newProduct(t, harness, company.OwnerToken, "CAKE", 25000, 3)
		candleId := newProduct(t, harness, company.OwnerToken, "CANDLE", 500, 20)
		stockOf := func(productId string) int64 {
			return harness.QueryIntForCompany(company.Id, `SELECT quantity FROM shop_stock WHERE product_id = $1`, productId)
		}
		neemaId := harness.Call(http.MethodPost, "/api/customers", company.OwnerToken, map[string]any{"name": "Neema", "phone": "0713000001", "credit_limit": 5000}).Data()["id"].(string)
		retiredId := harness.Call(http.MethodPost, "/api/customers", company.OwnerToken, map[string]any{"name": "Retired"}).Data()["id"].(string)
		harness.Call(http.MethodDelete, "/api/customers/"+retiredId, company.OwnerToken, nil)

		refusals := []struct {
			name       string
			token      string
			body       map[string]any
			wantStatus int
			wantCode   string
		}{
			{"a cashier without orders:create", cashierToken, map[string]any{"customer_id": neemaId, "items": []any{orderLine(cakeId, 1)}}, http.StatusForbidden, "forbidden"},
			{"more cakes than the shelf holds", clerkToken, map[string]any{"customer_id": neemaId, "items": []any{orderLine(candleId, 2), orderLine(cakeId, 4)}}, http.StatusConflict, "insufficient_stock"},
			{"a deposit above the total", clerkToken, map[string]any{"customer_id": neemaId, "items": []any{orderLine(cakeId, 1)}, "deposit": map[string]any{"method": "cash", "amount": 25001}}, http.StatusBadRequest, "deposit_too_high"},
			{"a deactivated customer", clerkToken, map[string]any{"customer_id": retiredId, "items": []any{orderLine(cakeId, 1)}}, http.StatusBadRequest, "customer_required"},
			{"a bad due date", clerkToken, map[string]any{"customer_id": neemaId, "items": []any{orderLine(cakeId, 1)}, "due_date": "31/12/2026"}, http.StatusBadRequest, "validation_failed"},
			{"a deposit paid on credit", clerkToken, map[string]any{"customer_id": neemaId, "items": []any{orderLine(cakeId, 1)}, "deposit": map[string]any{"method": "credit", "amount": 100}}, http.StatusBadRequest, "validation_failed"},
		}
		for _, refusal := range refusals {
			refused := harness.Call(http.MethodPost, "/api/orders", refusal.token, refusal.body)
			if refused.Status != refusal.wantStatus || refused.Code() != refusal.wantCode {
				t.Fatalf("%s returned %d %s, want %d %s", refusal.name, refused.Status, refused.Code(), refusal.wantStatus, refusal.wantCode)
			}
		}
		if stockOf(cakeId) != 3 || stockOf(candleId) != 20 || harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM customer_orders`) != 0 {
			t.Fatal("a refused order still touched stock or saved an order")
		}

		created := harness.Call(http.MethodPost, "/api/orders", clerkToken, map[string]any{
			"customer_id": neemaId, "items": []any{orderLine(cakeId, 2), orderLine(candleId, 10)}, "due_date": "2026-12-24", "note": " Happy birthday Amani ",
			"deposit": map[string]any{"method": "mobile", "amount": 20000},
		})
		order := created.Data()
		if created.Status != http.StatusCreated || order["number"] != "ORD-000001" || order["status"] != "open" || order["total"] != float64(55000) ||
			order["deposit_total"] != float64(20000) || order["balance_due"] != float64(35000) || order["due_date"] != "2026-12-24" || order["note"] != "Happy birthday Amani" ||
			len(order["lines"].([]any)) != 2 || len(order["payments"].([]any)) != 1 {
			t.Fatalf("creating an order returned %d %v", created.Status, created.Body)
		}
		orderId := order["id"].(string)
		if stockOf(cakeId) != 1 || stockOf(candleId) != 10 {
			t.Fatalf("the order did not reserve stock: cake %d candle %d", stockOf(cakeId), stockOf(candleId))
		}
		if harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM stock_movements WHERE reason = 'order_reserved' AND reference = 'ORD-000001'`) != 2 {
			t.Fatal("the reservation is not in the stock history")
		}
		shortTill := harness.Call(http.MethodPost, "/api/sales", cashierToken, map[string]any{"client_ref": "till-after-order", "items": []any{orderLine(cakeId, 2)}, "payments": []any{map[string]any{"method": "cash", "amount": 50000}}})
		if shortTill.Status != http.StatusConflict || shortTill.Code() != "insufficient_stock" {
			t.Fatalf("the till sold reserved cakes: %d %s", shortTill.Status, shortTill.Code())
		}

		if harness.Call(http.MethodGet, "/api/orders/"+orderId, otherCompany.OwnerToken, nil).Status != http.StatusNotFound {
			t.Fatal("another company can open this order")
		}
		if len(harness.Call(http.MethodGet, "/api/orders", otherCompany.OwnerToken, nil).Items()) != 0 {
			t.Fatal("another company sees this company's orders")
		}
		if harness.Call(http.MethodPost, "/api/orders/"+orderId+"/cancel", clerkToken, map[string]any{"reason": "No"}).Status != http.StatusForbidden {
			t.Fatal("a clerk without orders:delete cancelled an order")
		}

		tooMuchDeposit := harness.Call(http.MethodPost, "/api/orders/"+orderId+"/deposits", clerkToken, map[string]any{"method": "cash", "amount": 35001})
		if tooMuchDeposit.Status != http.StatusBadRequest || tooMuchDeposit.Code() != "deposit_too_high" {
			t.Fatalf("a deposit past the total returned %d %s", tooMuchDeposit.Status, tooMuchDeposit.Code())
		}
		moreDeposit := harness.Call(http.MethodPost, "/api/orders/"+orderId+"/deposits", clerkToken, map[string]any{"method": "cash", "amount": 5000})
		if moreDeposit.Status != http.StatusOK || moreDeposit.Data()["balance_due"] != float64(30000) {
			t.Fatalf("a second deposit returned %d %v", moreDeposit.Status, moreDeposit.Body)
		}

		ready := harness.Call(http.MethodPost, "/api/orders/"+orderId+"/ready", clerkToken, nil)
		if ready.Status != http.StatusOK || ready.Data()["status"] != "ready" || ready.Data()["ready_at"] == nil {
			t.Fatalf("marking ready returned %d %v", ready.Status, ready.Body)
		}
		readyTwice := harness.Call(http.MethodPost, "/api/orders/"+orderId+"/ready", clerkToken, nil)
		if readyTwice.Status != http.StatusConflict || readyTwice.Code() != "invalid_status" {
			t.Fatalf("marking ready twice returned %d %s", readyTwice.Status, readyTwice.Code())
		}
		if len(harness.Call(http.MethodGet, "/api/orders?status=ready", clerkToken, nil).Items()) != 1 || len(harness.Call(http.MethodGet, "/api/orders?status=open", clerkToken, nil).Items()) != 0 {
			t.Fatal("the status tabs do not follow the order")
		}
		if harness.Call(http.MethodGet, "/api/orders?status=lost", clerkToken, nil).Code() != "invalid_filter" {
			t.Fatal("an unknown status filter was accepted")
		}
		if len(harness.Call(http.MethodGet, "/api/orders?q=ord-1", clerkToken, nil).Items()) != 1 || len(harness.Call(http.MethodGet, "/api/orders?q=neema", clerkToken, nil).Items()) != 1 {
			t.Fatal("orders cannot be found by number or customer name")
		}

		shortPayment := harness.Call(http.MethodPost, "/api/orders/"+orderId+"/collect", clerkToken, map[string]any{"payments": []any{map[string]any{"method": "cash", "amount": 29999}}})
		if shortPayment.Status != http.StatusBadRequest || shortPayment.Code() != "invalid_payment" {
			t.Fatalf("collecting with too little returned %d %s", shortPayment.Status, shortPayment.Code())
		}
		creditWithoutRight := harness.Call(http.MethodPost, "/api/orders/"+orderId+"/collect", clerkToken, map[string]any{"payments": []any{map[string]any{"method": "cash", "amount": 25000}, map[string]any{"method": "credit", "amount": 5000}}})
		if creditWithoutRight.Status != http.StatusForbidden {
			t.Fatalf("a clerk without sales:create put an order on credit: %d %s", creditWithoutRight.Status, creditWithoutRight.Code())
		}
		overLimit := harness.Call(http.MethodPost, "/api/orders/"+orderId+"/collect", company.OwnerToken, map[string]any{"payments": []any{map[string]any{"method": "cash", "amount": 24999}, map[string]any{"method": "credit", "amount": 5001}}})
		if overLimit.Status != http.StatusBadRequest || overLimit.Code() != "credit_limit_exceeded" {
			t.Fatalf("collecting over the credit limit returned %d %s", overLimit.Status, overLimit.Code())
		}

		collected := harness.Call(http.MethodPost, "/api/orders/"+orderId+"/collect", company.OwnerToken, map[string]any{"payments": []any{map[string]any{"method": "cash", "amount": 30000}, map[string]any{"method": "credit", "amount": 5000}}})
		collectedData := collected.Data()
		if collected.Status != http.StatusOK || collectedData["status"] != "collected" || collectedData["sale_id"] == nil || collectedData["balance_due"] != float64(0) || collectedData["sale_receipt_number"] == nil {
			t.Fatalf("collecting returned %d %v", collected.Status, collected.Body)
		}
		sale := harness.Call(http.MethodGet, "/api/sales/"+collectedData["sale_id"].(string), company.OwnerToken, nil).Data()
		salePayments := map[string]float64{}
		for _, rawPayment := range sale["payments"].([]any) {
			payment := rawPayment.(map[string]any)
			salePayments[payment["method"].(string)] = payment["amount"].(float64)
		}
		if sale["total"] != float64(55000) || sale["customer_name"] != "Neema" || sale["order_number"] != "ORD-000001" || sale["credit_amount"] != float64(5000) ||
			sale["change_given"] != float64(5000) || salePayments["mobile"] != 20000 || salePayments["cash"] != 35000 || salePayments["credit"] != 5000 {
			t.Fatalf("the collected sale %v", sale)
		}
		if stockOf(cakeId) != 1 || stockOf(candleId) != 10 {
			t.Fatalf("collecting took the stock a second time: cake %d candle %d", stockOf(cakeId), stockOf(candleId))
		}
		if harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM sales WHERE customer_id = $1`, neemaId) != 1 {
			t.Fatal("collecting did not make exactly one sale")
		}
		if harness.Call(http.MethodGet, "/api/customers/"+neemaId, company.OwnerToken, nil).Data()["balance"] != float64(5000) {
			t.Fatal("the credit part of the collected order is not owed")
		}

		for _, afterCollection := range []struct{ path, body string }{{"/collect", "payments"}, {"/cancel", "reason"}, {"/ready", ""}, {"/deposits", "amount"}} {
			refused := harness.Call(http.MethodPost, "/api/orders/"+orderId+afterCollection.path, company.OwnerToken, map[string]any{"reason": "Late", "method": "cash", "amount": 100, "refund_method": "cash"})
			if refused.Status != http.StatusConflict || refused.Code() != "invalid_status" {
				t.Fatalf("%s on a collected order returned %d %s", afterCollection.path, refused.Status, refused.Code())
			}
		}
	})
}

func TestCancellingAnOrderReturnsStockAndTheDeposit(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Tailor", "owner@tailor.test")
		harness.Call(http.MethodPut, "/api/features", company.OwnerToken, map[string]any{"customers_enabled": true, "customer_orders_enabled": true})
		clothId := newProduct(t, harness, company.OwnerToken, "CLOTH", 8000, 5)
		baraka := harness.Call(http.MethodPost, "/api/customers", company.OwnerToken, map[string]any{"name": "Baraka"}).Data()["id"].(string)

		withDeposit := harness.Call(http.MethodPost, "/api/orders", company.OwnerToken, map[string]any{"customer_id": baraka, "items": []any{orderLine(clothId, 3)}, "deposit": map[string]any{"method": "cash", "amount": 10000}}).Data()
		withoutDeposit := harness.Call(http.MethodPost, "/api/orders", company.OwnerToken, map[string]any{"customer_id": baraka, "items": []any{orderLine(clothId, 2)}}).Data()
		if withoutDeposit["number"] != "ORD-000002" || harness.QueryIntForCompany(company.Id, `SELECT quantity FROM shop_stock WHERE product_id = $1`, clothId) != 0 {
			t.Fatalf("the second order %v", withoutDeposit)
		}

		noMethod := harness.Call(http.MethodPost, "/api/orders/"+withDeposit["id"].(string)+"/cancel", company.OwnerToken, map[string]any{"reason": "Changed mind"})
		if noMethod.Status != http.StatusBadRequest || noMethod.Code() != "refund_method_required" {
			t.Fatalf("cancelling without saying how the deposit goes back returned %d %s", noMethod.Status, noMethod.Code())
		}
		noReason := harness.Call(http.MethodPost, "/api/orders/"+withDeposit["id"].(string)+"/cancel", company.OwnerToken, map[string]any{"reason": "   ", "refund_method": "cash"})
		if noReason.Status != http.StatusBadRequest {
			t.Fatalf("cancelling without a reason returned %d", noReason.Status)
		}

		cancelled := harness.Call(http.MethodPost, "/api/orders/"+withDeposit["id"].(string)+"/cancel", company.OwnerToken, map[string]any{"reason": "Changed mind", "refund_method": "mobile"})
		cancelledData := cancelled.Data()
		cancelledPayments := cancelledData["payments"].([]any)
		refund := cancelledPayments[len(cancelledPayments)-1].(map[string]any)
		if cancelled.Status != http.StatusOK || cancelledData["status"] != "cancelled" || cancelledData["cancel_reason"] != "Changed mind" || cancelledData["cancelled_by_name"] == nil ||
			cancelledData["deposit_total"] != float64(0) || refund["kind"] != "refund" || refund["method"] != "mobile" || refund["amount"] != float64(10000) {
			t.Fatalf("cancelling returned %d %v", cancelled.Status, cancelled.Body)
		}
		if harness.QueryIntForCompany(company.Id, `SELECT quantity FROM shop_stock WHERE product_id = $1`, clothId) != 3 ||
			harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM stock_movements WHERE reason = 'order_cancelled' AND reference = 'ORD-000001'`) != 1 {
			t.Fatal("cancelling did not put the cloth back")
		}
		cancelTwice := harness.Call(http.MethodPost, "/api/orders/"+withDeposit["id"].(string)+"/cancel", company.OwnerToken, map[string]any{"reason": "Again", "refund_method": "cash"})
		collectCancelled := harness.Call(http.MethodPost, "/api/orders/"+withDeposit["id"].(string)+"/collect", company.OwnerToken, map[string]any{"payments": []any{map[string]any{"method": "cash", "amount": 24000}}})
		if cancelTwice.Code() != "invalid_status" || collectCancelled.Code() != "invalid_status" {
			t.Fatalf("a cancelled order could be cancelled (%s) or collected (%s) again", cancelTwice.Code(), collectCancelled.Code())
		}
		if harness.QueryIntForCompany(company.Id, `SELECT quantity FROM shop_stock WHERE product_id = $1`, clothId) != 3 {
			t.Fatal("a refused second cancel returned stock again")
		}

		plainCancel := harness.Call(http.MethodPost, "/api/orders/"+withoutDeposit["id"].(string)+"/cancel", company.OwnerToken, map[string]any{"reason": "Not needed"})
		if plainCancel.Status != http.StatusOK || len(plainCancel.Data()["payments"].([]any)) != 0 {
			t.Fatalf("cancelling an order without a deposit returned %d %v", plainCancel.Status, plainCancel.Body)
		}
		if harness.QueryIntForCompany(company.Id, `SELECT quantity FROM shop_stock WHERE product_id = $1`, clothId) != 5 {
			t.Fatal("all the cloth should be back on the shelf")
		}

		fullyPaid := harness.Call(http.MethodPost, "/api/orders", company.OwnerToken, map[string]any{"customer_id": baraka, "items": []any{orderLine(clothId, 1)}, "deposit": map[string]any{"method": "card", "amount": 8000}}).Data()
		handedOver := harness.Call(http.MethodPost, "/api/orders/"+fullyPaid["id"].(string)+"/collect", company.OwnerToken, map[string]any{})
		if handedOver.Status != http.StatusOK || handedOver.Data()["status"] != "collected" {
			t.Fatalf("collecting a fully paid order with no payment returned %d %v", handedOver.Status, handedOver.Body)
		}
		if harness.QueryIntForCompany(company.Id, `SELECT quantity FROM shop_stock WHERE product_id = $1`, clothId) != 4 {
			t.Fatal("a fully paid collection changed stock")
		}
	})
}
