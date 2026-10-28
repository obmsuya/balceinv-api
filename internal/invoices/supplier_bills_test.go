package invoices_test

import (
	"bytes"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
)

func supplierOwed(t *testing.T, harness *apptest.Harness, ownerToken string, supplierId string) float64 {
	t.Helper()
	supplier := harness.Call(http.MethodGet, "/api/suppliers/"+supplierId, ownerToken, nil)
	if supplier.Status != http.StatusOK {
		t.Fatalf("reading the supplier returned %d %v", supplier.Status, supplier.Body)
	}
	return supplier.Data()["balance"].(float64)
}

func TestSupplierInvoicesLowerWhatWeOweOrBringMoneyIn(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Supplier Bill Shop", "owner@supplierbills.test")
		switched := harness.Call(http.MethodPut, "/api/features", company.OwnerToken, map[string]any{"suppliers_enabled": true, "invoices_enabled": true, "accounting_mode": "simple"})
		if switched.Status != http.StatusOK {
			t.Fatalf("features returned %d %v", switched.Status, switched.Body)
		}
		harness.Call(http.MethodPost, "/api/accounting/start", company.OwnerToken, map[string]any{"mode": "today"})
		supplierId := harness.Call(http.MethodPost, "/api/suppliers", company.OwnerToken, map[string]any{"name": "Bakhresa Wholesale", "phone": "0713111222", "opening_balance": 500000}).Data()["id"].(string)
		productId := harness.Call(http.MethodPost, "/api/products", company.OwnerToken, map[string]any{"sku": "OIL20", "name": "Cooking oil 20L", "price": 98000, "cost_price": 80000, "opening_quantity": 6}).Data()["id"].(string)

		call := func(method string, path string, body any, wantStatus int) apptest.Response {
			t.Helper()
			result := harness.Call(method, path, company.OwnerToken, body)
			if result.Status != wantStatus {
				t.Fatalf("%s %s returned %d, want %d: %v", method, path, result.Status, wantStatus, result.Body)
			}
			return result
		}

		charged := call(http.MethodPost, "/api/invoices", map[string]any{
			"kind": "supplier_invoice", "supplier_id": supplierId,
			"lines": []map[string]any{{"description": "Damaged cartons charged back", "quantity": 1, "unit_price": 50000}, {"product_id": productId, "quantity": 2}},
		}, http.StatusCreated).Data()
		if charged["number"] != fmt.Sprintf("SINV-%d-0001", time.Now().Year()) || charged["total"] != float64(50000+2*98000) || charged["party"].(map[string]any)["type"] != "supplier" {
			t.Fatalf("the supplier invoice was %v", charged)
		}
		chargedId := charged["id"].(string)
		call(http.MethodPost, "/api/invoices/"+chargedId+"/issue", nil, http.StatusOK)
		call(http.MethodPost, "/api/invoices/"+chargedId+"/deliver", nil, http.StatusOK)
		if owed := supplierOwed(t, harness, company.OwnerToken, supplierId); owed != 500000-246000 {
			t.Fatalf("after billing the supplier we owe %v", owed)
		}
		stockLeft := harness.QueryIntForCompany(company.Id, `SELECT quantity FROM shop_stock WHERE company_id = $1 AND shop_id = $2 AND product_id = $3`, company.Id, company.ShopId, productId)
		if stockLeft != 4 {
			t.Fatalf("the products billed to the supplier left %d in stock", stockLeft)
		}

		if wrongMethod := harness.Call(http.MethodPost, "/api/invoices/"+chargedId+"/payments", company.OwnerToken, map[string]any{"method": "card", "amount": 1000}); wrongMethod.Code() != "invalid_payment" {
			t.Fatalf("a card payment from a supplier returned %d %v", wrongMethod.Status, wrongMethod.Body)
		}
		offset := call(http.MethodPost, "/api/invoices/"+chargedId+"/payments", map[string]any{"method": "offset", "amount": 46000}, http.StatusOK).Data()
		if offset["payment_status"] != "part_paid" || supplierOwed(t, harness, company.OwnerToken, supplierId) != 254000 {
			t.Fatalf("an offset changed what we owe: %v", offset)
		}
		cashIn := call(http.MethodPost, "/api/invoices/"+chargedId+"/payments", map[string]any{"method": "bank", "amount": 200000, "reference": "CRDB-889"}, http.StatusOK).Data()
		if cashIn["payment_status"] != "paid" || supplierOwed(t, harness, company.OwnerToken, supplierId) != 454000 {
			t.Fatalf("money from the supplier left us owing %v: %v", supplierOwed(t, harness, company.OwnerToken, supplierId), cashIn)
		}
		receipts := harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM journal_entries WHERE company_id = $1 AND source_type = 'supplier_receipt'`, company.Id)
		if receipts != 1 {
			t.Fatalf("the books have %d supplier receipts", receipts)
		}

		var bankPaymentId string
		for _, payment := range cashIn["payments"].([]any) {
			if payment.(map[string]any)["method"] == "bank" {
				bankPaymentId = payment.(map[string]any)["id"].(string)
			}
		}
		voided := call(http.MethodPost, "/api/invoices/"+chargedId+"/payments/"+bankPaymentId+"/void", map[string]any{"reason": "Cheque bounced"}, http.StatusOK).Data()
		if voided["payment_status"] != "part_paid" || supplierOwed(t, harness, company.OwnerToken, supplierId) != 254000 {
			t.Fatalf("cancelling the supplier's money left %v: %v", supplierOwed(t, harness, company.OwnerToken, supplierId), voided["payment_status"])
		}
		receiptVoids := harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM journal_entries WHERE company_id = $1 AND source_type = 'supplier_receipt_void'`, company.Id)
		if receiptVoids != 1 {
			t.Fatalf("the books have %d cancelled supplier receipts", receiptVoids)
		}

		statement := call(http.MethodGet, "/api/suppliers/"+supplierId+"/statement", nil, http.StatusOK).Data()
		sawInvoice := false
		for _, entry := range statement["lines"].([]any) {
			if entry.(map[string]any)["kind"] == "invoice" {
				sawInvoice = true
			}
		}
		if !sawInvoice {
			t.Fatalf("the supplier statement did not show the invoice: %v", statement["lines"])
		}

		pdf := call(http.MethodGet, "/api/invoices/"+chargedId+"/document", nil, http.StatusOK)
		if !bytes.HasPrefix(pdf.Raw, []byte("%PDF")) || !containsUtf16(pdf.Raw, "Bakhresa Wholesale") {
			t.Fatal("the supplier invoice PDF is missing the supplier")
		}
	})
}

func TestPurchaseOrdersPrintForTheSupplier(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Order Paper Shop", "owner@orderpaper.test")
		harness.Call(http.MethodPut, "/api/features", company.OwnerToken, map[string]any{"suppliers_enabled": true, "purchase_orders_enabled": true})
		supplierId := harness.Call(http.MethodPost, "/api/suppliers", company.OwnerToken, map[string]any{"name": "Azam Mills", "email": "orders@azam.test"}).Data()["id"].(string)
		productId := harness.Call(http.MethodPost, "/api/products", company.OwnerToken, map[string]any{"sku": "FLOUR", "name": "Wheat flour 25kg", "price": 45000, "cost_price": 38000}).Data()["id"].(string)
		order := harness.Call(http.MethodPost, "/api/purchase-orders", company.OwnerToken, map[string]any{
			"supplier_id": supplierId, "lines": []map[string]any{{"product_id": productId, "quantity": 20, "expected_unit_cost": 37500}},
		})
		if order.Status != http.StatusCreated {
			t.Fatalf("creating the order returned %d %v", order.Status, order.Body)
		}
		orderId := order.Data()["id"].(string)

		pdf := harness.Call(http.MethodGet, "/api/purchase-orders/"+orderId+"/document?lang=en", company.OwnerToken, nil)
		if pdf.Status != http.StatusOK || !bytes.HasPrefix(pdf.Raw, []byte("%PDF")) || !containsUtf16(pdf.Raw, "Purchase order") || !containsUtf16(pdf.Raw, "Azam Mills") {
			t.Fatalf("the purchase order PDF returned %d", pdf.Status)
		}
		swahili := harness.Call(http.MethodGet, "/api/purchase-orders/"+orderId+"/document?lang=sw", company.OwnerToken, nil)
		if !containsUtf16(swahili.Raw, "Oda kwa msambazaji") {
			t.Fatal("the Swahili purchase order has the wrong title")
		}
		if refused := harness.Call(http.MethodGet, "/api/purchase-orders/"+orderId+"/document?format=xlsx", company.OwnerToken, nil); refused.Status != http.StatusBadRequest {
			t.Fatalf("an Excel purchase order returned %d", refused.Status)
		}
		otherCompany := harness.CreateCompany("Other Paper Shop", "owner@otherpaper.test")
		harness.Call(http.MethodPut, "/api/features", otherCompany.OwnerToken, map[string]any{"suppliers_enabled": true, "purchase_orders_enabled": true})
		if foreign := harness.Call(http.MethodGet, "/api/purchase-orders/"+orderId+"/document", otherCompany.OwnerToken, nil); foreign.Status != http.StatusNotFound {
			t.Fatalf("another company's owner got %d", foreign.Status)
		}
	})
}
