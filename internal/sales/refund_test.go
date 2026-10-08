package sales_test

import (
	"net/http"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/google/uuid"
)

func refundSale(harness *apptest.Harness, sessionToken string, saleId string, body map[string]any) apptest.Response {
	return harness.Call(http.MethodPost, "/api/sales/"+saleId+"/refunds", sessionToken, body)
}

func firstItemId(t *testing.T, sale apptest.Response) string {
	t.Helper()
	items := sale.Data()["items"].([]any)
	return items[0].(map[string]any)["item_id"].(string)
}

func TestRefundsReturnMoneyAndStockAndFollowTheirLimits(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Refund Shop", "owner@refund.test")
		harness.Call(http.MethodPut, "/api/features", company.OwnerToken, map[string]any{"customers_enabled": true, "credit_sales_enabled": true, "accounting_mode": "simple"})
		if harness.Call(http.MethodPost, "/api/accounting/start", company.OwnerToken, map[string]any{"mode": "today", "cash_in_drawer": 100000}).Status != http.StatusCreated {
			t.Fatal("starting the books failed")
		}
		cashierToken := harness.CreateStaff(company, "cashier@refund.test", []string{"sales:create", "sales:view"}, []uuid.UUID{company.ShopId})
		supervisorToken := harness.CreateStaff(company, "supervisor@refund.test", []string{"sales:view", "sales:edit", "sales:delete"}, []uuid.UUID{company.ShopId})
		oilId := newProduct(t, harness, company.OwnerToken, map[string]any{"sku": "OIL", "name": "Cooking oil", "price": 1000, "cost_price": 600, "opening_quantity": 20})
		mariamId := harness.Call(http.MethodPost, "/api/customers", company.OwnerToken, map[string]any{"name": "Mariam", "phone": "0765000011"}).Data()["id"].(string)

		cashSale := sell(harness, cashierToken, "refund-cash-0001", []map[string]any{line(oilId, 3)}, cash(3000))
		cashSaleId := cashSale.Data()["id"].(string)
		oilItemId := firstItemId(t, cashSale)

		partBody := map[string]any{"client_ref": "refund-part-0001", "method": "cash", "restock": true, "reason": "customer returned one bottle", "lines": []any{map[string]any{"item_id": oilItemId, "quantity": 1}}}
		if refundSale(harness, cashierToken, cashSaleId, partBody).Status != http.StatusForbidden {
			t.Fatal("a cashier without sales:edit refunded")
		}
		partRefund := refundSale(harness, supervisorToken, cashSaleId, partBody)
		if partRefund.Status != http.StatusCreated || partRefund.Data()["refunded_total"] != float64(1000) {
			t.Fatalf("a partial refund returned %d %v", partRefund.Status, partRefund.Body)
		}
		replayed := refundSale(harness, supervisorToken, cashSaleId, partBody)
		if replayed.Status != http.StatusOK || replayed.Data()["refunded_total"] != float64(1000) {
			t.Fatalf("retrying the same refund returned %d %v", replayed.Status, replayed.Body)
		}
		if harness.QueryIntForCompany(company.Id, `SELECT quantity FROM shop_stock WHERE product_id = $1`, oilId) != 18 {
			t.Fatal("the returned bottle did not go back on the shelf")
		}

		tooMany := refundSale(harness, supervisorToken, cashSaleId, map[string]any{"client_ref": "refund-many-0001", "method": "cash", "reason": "too many", "lines": []any{map[string]any{"item_id": oilItemId, "quantity": 3}}})
		if tooMany.Code() != "refund_too_many" {
			t.Fatalf("refunding more than was left returned %d %v", tooMany.Status, tooMany.Body)
		}
		onAccount := refundSale(harness, supervisorToken, cashSaleId, map[string]any{"client_ref": "refund-acct-0001", "method": "credit", "reason": "to account", "lines": []any{map[string]any{"item_id": oilItemId, "quantity": 1}}})
		if onAccount.Code() != "refund_credit_not_possible" {
			t.Fatalf("refunding a cash sale to an account returned %d %v", onAccount.Status, onAccount.Body)
		}

		restRefund := refundSale(harness, supervisorToken, cashSaleId, map[string]any{"client_ref": "refund-rest-0001", "method": "mobile", "restock": false, "reason": "both leaking", "lines": []any{map[string]any{"item_id": oilItemId, "quantity": 2}}})
		restData := restRefund.Data()
		restItems := restData["items"].([]any)
		if restRefund.Status != http.StatusCreated || restData["refunded_total"] != float64(3000) || restItems[0].(map[string]any)["refunded_quantity"] != float64(3) {
			t.Fatalf("refunding the rest returned %d %v", restRefund.Status, restRefund.Body)
		}
		if harness.QueryIntForCompany(company.Id, `SELECT quantity FROM shop_stock WHERE product_id = $1`, oilId) != 18 {
			t.Fatal("leaking bottles that were not restocked went back on the shelf")
		}
		if harness.Call(http.MethodPost, "/api/sales/"+cashSaleId+"/void", supervisorToken, map[string]any{"reason": "whole thing"}).Code() != "refund_not_possible" {
			t.Fatal("a refunded sale was voided")
		}
		if harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM journal_entries WHERE source_type = 'sale_void' AND party_type IS NULL`) != 2 {
			t.Fatal("the two refunds are not both in the books")
		}

		creditSale := sellTo(harness, cashierToken, "refund-credit-0001", mariamId, []map[string]any{line(oilId, 2)}, []map[string]any{credit(2000)})
		creditSaleId := creditSale.Data()["id"].(string)
		creditRefund := refundSale(harness, supervisorToken, creditSaleId, map[string]any{"client_ref": "refund-credit-0002", "method": "credit", "restock": true, "reason": "wrong brand", "lines": []any{map[string]any{"item_id": firstItemId(t, creditSale), "quantity": 1}}})
		if creditRefund.Status != http.StatusCreated {
			t.Fatalf("a refund to the account returned %d %v", creditRefund.Status, creditRefund.Body)
		}
		if balance := harness.Call(http.MethodGet, "/api/customers/"+mariamId, company.OwnerToken, nil).Data()["balance"]; balance != float64(1000) {
			t.Fatalf("Mariam owes %v after a 1,000 refund to her account, want 1000", balance)
		}

		voidedSale := sell(harness, cashierToken, "refund-voided-0001", []map[string]any{line(oilId, 1)}, cash(1000))
		voidedSaleId := voidedSale.Data()["id"].(string)
		harness.Call(http.MethodPost, "/api/sales/"+voidedSaleId+"/void", supervisorToken, map[string]any{"reason": "mistake"})
		if refundSale(harness, supervisorToken, voidedSaleId, map[string]any{"client_ref": "refund-voided-0002", "method": "cash", "reason": "late", "lines": []any{map[string]any{"item_id": firstItemId(t, voidedSale), "quantity": 1}}}).Code() != "refund_not_possible" {
			t.Fatal("a voided sale was refunded")
		}

		totals := harness.Call(http.MethodGet, "/api/sales/totals", company.OwnerToken, nil).Data()
		if totals["total"] != float64(5000) || totals["refund_total"] != float64(4000) {
			t.Fatalf("totals after refunds: %v", totals)
		}
		summary := harness.Call(http.MethodGet, "/api/reports/summary", company.OwnerToken, nil).Data()
		if summary["refund_total"] != float64(4000) || summary["net_sales"] != float64(1000) || summary["cost_total"] != float64(1800) {
			t.Fatalf("the summary after refunds: %v", summary)
		}
	})
}

func TestRefundNotesWaitForTheReceiptAndCarryOnlyTheRefund(t *testing.T) {
	fake := startFakeEfd(t)
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Refund Efd", "owner@refundefd.test")
		harness.Call(http.MethodPut, "/api/features", company.OwnerToken, map[string]any{"vat_registered": true, "vat_number": "40-000111-A"})
		harness.Call(http.MethodPut, "/api/settings", company.OwnerToken, map[string]any{"efd_enabled": true, "efd_endpoint": fake.server.URL + "/receipts", "efd_api_key": "efd-refund-key"})
		juiceId := newProduct(t, harness, company.OwnerToken, map[string]any{"sku": "JUICE", "name": "Juice", "price": 2360, "opening_quantity": 10})

		fake.isDown.Store(true)
		sale := sell(harness, company.OwnerToken, "refund-efd-0001", []map[string]any{line(juiceId, 2)}, cash(4720))
		saleId := sale.Data()["id"].(string)
		refunded := refundSale(harness, company.OwnerToken, saleId, map[string]any{"client_ref": "refund-efd-0002", "method": "cash", "restock": true, "reason": "one was warm", "lines": []any{map[string]any{"item_id": firstItemId(t, sale), "quantity": 1}}})
		refundId := refunded.Data()["refunds"].([]any)[0].(map[string]any)["id"].(string)
		fake.isDown.Store(false)

		harness.Call(http.MethodPost, "/api/sales/fiscal/send-waiting", company.OwnerToken, nil)
		fake.mutex.Lock()
		lastPayload := fake.lastPayload
		lastIdemKey := fake.lastIdemKey
		fake.mutex.Unlock()
		if lastPayload.DocumentType != "credit_note" || lastPayload.CreditFor != "refund" || lastIdemKey != refundId+":refund-note" || lastPayload.Totals.Total != 2360 || lastPayload.Totals.Tax != 360 ||
			len(lastPayload.Items) != 1 || lastPayload.Items[0].Quantity != 1 || lastPayload.Reason == nil || *lastPayload.Reason != "one was warm" {
			t.Fatalf("the EFD got %+v with key %s", lastPayload, lastIdemKey)
		}
		afterSend := harness.Call(http.MethodGet, "/api/sales/"+saleId, company.OwnerToken, nil).Data()
		if afterSend["fiscal"].(map[string]any)["status"] != "sent" || afterSend["refunds"].([]any)[0].(map[string]any)["fiscal"].(map[string]any)["status"] != "sent" {
			t.Fatalf("after sending: receipt %v refunds %v", afterSend["fiscal"], afterSend["refunds"])
		}
	})
}
