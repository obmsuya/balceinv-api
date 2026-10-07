package sales_test

import (
	"net/http"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/google/uuid"
)

func voidSale(harness *apptest.Harness, sessionToken string, saleId string, reason string) apptest.Response {
	return harness.Call(http.MethodPost, "/api/sales/"+saleId+"/void", sessionToken, map[string]any{"reason": reason})
}

func TestVoidingASaleReturnsStockReversesTheBooksAndDropsItFromTotals(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Void Shop", "owner@void.test")
		harness.Call(http.MethodPut, "/api/features", company.OwnerToken, map[string]any{"customers_enabled": true, "credit_sales_enabled": true, "accounting_mode": "simple"})
		booksStarted := harness.Call(http.MethodPost, "/api/accounting/start", company.OwnerToken, map[string]any{"mode": "today"})
		if booksStarted.Status != http.StatusCreated {
			t.Fatalf("starting the books returned %d %v", booksStarted.Status, booksStarted.Body)
		}
		cashierToken := harness.CreateStaff(company, "cashier@void.test", []string{"sales:create", "sales:view"}, []uuid.UUID{company.ShopId})
		supervisorToken := harness.CreateStaff(company, "supervisor@void.test", []string{"sales:view", "sales:delete"}, []uuid.UUID{company.ShopId})
		sugarId := newProduct(t, harness, company.OwnerToken, map[string]any{"sku": "SUGAR", "name": "Sugar", "price": 3000, "cost_price": 2000, "opening_quantity": 20})
		mariamId := harness.Call(http.MethodPost, "/api/customers", company.OwnerToken, map[string]any{"name": "Mariam", "phone": "0765000009"}).Data()["id"].(string)

		keptSale := sell(harness, cashierToken, "void-kept-0001", []map[string]any{line(sugarId, 1)}, cash(3000))
		wrongSale := sellTo(harness, cashierToken, "void-wrong-0001", mariamId, []map[string]any{line(sugarId, 4)}, []map[string]any{credit(12000)})
		if keptSale.Status != http.StatusCreated || wrongSale.Status != http.StatusCreated {
			t.Fatalf("selling returned %d %v and %d %v", keptSale.Status, keptSale.Body, wrongSale.Status, wrongSale.Body)
		}
		wrongSaleId := wrongSale.Data()["id"].(string)
		if harness.QueryIntForCompany(company.Id, `SELECT quantity FROM shop_stock WHERE product_id = $1`, sugarId) != 15 {
			t.Fatal("selling 5 sugar did not leave 15")
		}

		if voidSale(harness, cashierToken, wrongSaleId, "typed 4 instead of 1").Status != http.StatusForbidden {
			t.Fatal("a cashier without sales:delete voided a sale")
		}
		if voidSale(harness, supervisorToken, wrongSaleId, "").Status != http.StatusBadRequest {
			t.Fatal("a void without a reason was accepted")
		}

		voided := voidSale(harness, supervisorToken, wrongSaleId, "typed 4 instead of 1")
		voidedData := voided.Data()
		if voided.Status != http.StatusOK || voidedData["voided_at"] == nil || voidedData["void_reason"] != "typed 4 instead of 1" || voidedData["voided_by_name"] == nil {
			t.Fatalf("voiding returned %d %v", voided.Status, voided.Body)
		}
		if voidSale(harness, supervisorToken, wrongSaleId, "again").Code() != "sale_already_voided" {
			t.Fatal("a sale was voided twice")
		}

		if harness.QueryIntForCompany(company.Id, `SELECT quantity FROM shop_stock WHERE product_id = $1`, sugarId) != 19 {
			t.Fatal("voiding did not put the 4 sugar back")
		}
		mariam := harness.Call(http.MethodGet, "/api/customers/"+mariamId, company.OwnerToken, nil).Data()
		if mariam["balance"] != float64(0) {
			t.Fatalf("Mariam still owes %v after the credit sale was voided", mariam["balance"])
		}
		totals := harness.Call(http.MethodGet, "/api/sales/totals", company.OwnerToken, nil).Data()
		if totals["sale_count"] != float64(1) || totals["total"] != float64(3000) {
			t.Fatalf("totals after the void: %v", totals)
		}
		if harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM journal_entries WHERE source_type = 'sale_void' AND source_id = $1 AND reverses_entry_id IS NOT NULL`, wrongSaleId) != 1 {
			t.Fatal("the books have no reversal for the voided sale")
		}
		listed := harness.Call(http.MethodGet, "/api/sales", company.OwnerToken, nil).Items()
		if len(listed) != 2 {
			t.Fatalf("sales history should still list the voided sale, got %d", len(listed))
		}

		resold := sellTo(harness, cashierToken, "void-resold-0001", mariamId, []map[string]any{line(sugarId, 1)}, []map[string]any{credit(3000)})
		if resold.Status != http.StatusCreated || harness.Call(http.MethodGet, "/api/customers/"+mariamId, company.OwnerToken, nil).Data()["balance"] != float64(3000) {
			t.Fatalf("ringing the sale up again returned %d %v", resold.Status, resold.Body)
		}
	})
}

func TestVoidingASaleTheEfdAcceptedSendsACreditNote(t *testing.T) {
	fake := startFakeEfd(t)
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Void Efd", "owner@voidefd.test")
		harness.Call(http.MethodPut, "/api/features", company.OwnerToken, map[string]any{"vat_registered": true, "vat_number": "40-000111-A"})
		harness.Call(http.MethodPut, "/api/settings", company.OwnerToken, map[string]any{"efd_enabled": true, "efd_endpoint": fake.server.URL + "/receipts", "efd_api_key": "efd-void-key"})
		juiceId := newProduct(t, harness, company.OwnerToken, map[string]any{"sku": "JUICE", "name": "Juice", "price": 2360, "opening_quantity": 10})

		sentSale := sell(harness, company.OwnerToken, "void-efd-sent-1", []map[string]any{line(juiceId, 1)}, cash(2360))
		sentSaleId := sentSale.Data()["id"].(string)
		sentFiscal := harness.Call(http.MethodPost, "/api/sales/"+sentSaleId+"/fiscal", company.OwnerToken, nil)
		if sentFiscal.Data()["status"] != "sent" {
			t.Fatalf("sending the receipt returned %v", sentFiscal.Body)
		}

		voided := voidSale(harness, company.OwnerToken, sentSaleId, "customer returned it")
		creditNote, _ := voided.Data()["credit_note"].(map[string]any)
		if voided.Status != http.StatusOK || creditNote["status"] != "pending" {
			t.Fatalf("voiding an EFD sale returned %d credit note %v", voided.Status, voided.Data()["credit_note"])
		}

		sendSummary := harness.Call(http.MethodPost, "/api/sales/fiscal/send-waiting", company.OwnerToken, nil)
		if sendSummary.Status != http.StatusOK || sendSummary.Data()["sent"] != float64(1) || sendSummary.Data()["still_waiting"] != float64(0) {
			t.Fatalf("sending waiting documents returned %d %v", sendSummary.Status, sendSummary.Body)
		}
		fake.mutex.Lock()
		lastPayload := fake.lastPayload
		lastIdemKey := fake.lastIdemKey
		fake.mutex.Unlock()
		if lastPayload.DocumentType != "credit_note" || lastPayload.OriginalReceiptNumber == nil || *lastPayload.OriginalReceiptNumber != sentSale.Data()["receipt_number"] ||
			lastPayload.Reason == nil || *lastPayload.Reason != "customer returned it" || lastIdemKey != sentSaleId+":credit-note" || lastPayload.OriginalVerification == nil {
			t.Fatalf("the EFD got %+v with key %s", lastPayload, lastIdemKey)
		}
		afterSend := harness.Call(http.MethodGet, "/api/sales/"+sentSaleId, company.OwnerToken, nil).Data()
		if afterSend["credit_note"].(map[string]any)["status"] != "sent" {
			t.Fatalf("the credit note is %v", afterSend["credit_note"])
		}

		fake.isDown.Store(true)
		unsentSale := sell(harness, company.OwnerToken, "void-efd-unsent-1", []map[string]any{line(juiceId, 1)}, cash(2360))
		unsentSaleId := unsentSale.Data()["id"].(string)
		voidedUnsent := voidSale(harness, company.OwnerToken, unsentSaleId, "wrong item")
		if voidedUnsent.Status != http.StatusOK || voidedUnsent.Data()["fiscal"] != nil || voidedUnsent.Data()["credit_note"] != nil {
			t.Fatalf("voiding a sale the EFD never got returned %d fiscal %v credit note %v", voidedUnsent.Status, voidedUnsent.Data()["fiscal"], voidedUnsent.Data()["credit_note"])
		}
		fake.isDown.Store(false)
		if fake.timesReceived(unsentSaleId) != 0 {
			t.Fatal("a voided sale the EFD never got was sent")
		}
	})
}
