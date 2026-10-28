package invoices_test

import (
	"bytes"
	"fmt"
	"net/http"
	"testing"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/google/uuid"
)

type invoiceShop struct {
	harness    *apptest.Harness
	company    apptest.Company
	customerId string
	productId  string
}

func openInvoiceShop(t *testing.T, engineCase testkit.EngineCase, withBooks bool) invoiceShop {
	t.Helper()
	harness := apptest.Start(t, engineCase)
	company := harness.CreateCompany("Invoice Shop", "owner@invoices.test")
	switches := map[string]any{"customers_enabled": true, "invoices_enabled": true}
	if withBooks {
		switches["accounting_mode"] = "simple"
	}
	if switched := harness.Call(http.MethodPut, "/api/features", company.OwnerToken, switches); switched.Status != http.StatusOK {
		t.Fatalf("features returned %d %v", switched.Status, switched.Body)
	}
	if withBooks {
		if started := harness.Call(http.MethodPost, "/api/accounting/start", company.OwnerToken, map[string]any{"mode": "today"}); started.Status >= 300 {
			t.Fatalf("starting the books returned %d %v", started.Status, started.Body)
		}
	}
	customerId := harness.Call(http.MethodPost, "/api/customers", company.OwnerToken, map[string]any{"name": "Juma Traders", "phone": "0712000111"}).Data()["id"].(string)
	productId := harness.Call(http.MethodPost, "/api/products", company.OwnerToken, map[string]any{"sku": "RICE25", "name": "Rice 25kg", "price": 72000, "cost_price": 60000, "opening_quantity": 10}).Data()["id"].(string)
	return invoiceShop{harness: harness, company: company, customerId: customerId, productId: productId}
}

func (shop invoiceShop) call(t *testing.T, method string, path string, body any, wantStatus int) apptest.Response {
	t.Helper()
	result := shop.harness.Call(method, path, shop.company.OwnerToken, body)
	if result.Status != wantStatus {
		t.Fatalf("%s %s returned %d, want %d: %v", method, path, result.Status, wantStatus, result.Body)
	}
	return result
}

func (shop invoiceShop) stockLeft(t *testing.T) int64 {
	return shop.harness.QueryIntForCompany(shop.company.Id, `SELECT quantity FROM shop_stock WHERE company_id = $1 AND shop_id = $2 AND product_id = $3`, shop.company.Id, shop.company.ShopId, shop.productId)
}

func (shop invoiceShop) customerBalance(t *testing.T) float64 {
	return shop.call(t, http.MethodGet, "/api/customers/"+shop.customerId, nil, http.StatusOK).Data()["balance"].(float64)
}

func TestInvoicesNeedTheirSwitches(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Switch Shop", "owner@switch.test")
		if listed := harness.Call(http.MethodGet, "/api/invoices", company.OwnerToken, nil); listed.Status != http.StatusForbidden || listed.Code() != "feature_off" {
			t.Fatalf("invoices while off returned %d %v", listed.Status, listed.Body)
		}
		if refused := harness.Call(http.MethodPut, "/api/features", company.OwnerToken, map[string]any{"invoices_enabled": true}); refused.Status != http.StatusBadRequest {
			t.Fatalf("invoices without customers or suppliers returned %d", refused.Status)
		}
		harness.Call(http.MethodPut, "/api/features", company.OwnerToken, map[string]any{"suppliers_enabled": true, "invoices_enabled": true})
		customerInvoice := harness.Call(http.MethodPost, "/api/invoices", company.OwnerToken, map[string]any{
			"kind": "invoice", "customer_id": uuid.NewString(), "lines": []map[string]any{{"description": "Labour", "quantity": 1, "unit_price": 1000}},
		})
		if customerInvoice.Status != http.StatusForbidden || customerInvoice.Code() != "feature_off" {
			t.Fatalf("a customer invoice with customers off returned %d %v", customerInvoice.Status, customerInvoice.Body)
		}
	})
}

func TestInvoiceFromDraftToPaidMovesStockDebtAndBooksOnce(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		shop := openInvoiceShop(t, engineCase, true)
		year := time.Now().Year()

		draft := shop.call(t, http.MethodPost, "/api/invoices", map[string]any{
			"kind":        "invoice",
			"customer_id": shop.customerId,
			"reference":   "LPO-4471",
			"lines": []map[string]any{
				{"product_id": shop.productId, "quantity": 4, "discount_amount": 8000},
				{"description": "Transport to Mbezi", "unit": "trip", "quantity": 1, "unit_price": 25000},
			},
		}, http.StatusCreated).Data()
		invoiceId := draft["id"].(string)
		if draft["number"] != fmt.Sprintf("INV-%d-0001", year) || draft["status"] != "draft" || draft["total"] != float64(4*72000-8000+25000) || draft["subtotal"] != float64(4*72000+25000) {
			t.Fatalf("the draft was %v", draft)
		}
		lines := draft["lines"].([]any)
		if lines[0].(map[string]any)["description"] != "Rice 25kg" || lines[0].(map[string]any)["unit"] != "pcs" || lines[1].(map[string]any)["unit_price"] != float64(25000) {
			t.Fatalf("the lines were %v", lines)
		}
		if draft["due_date"] == nil || draft["payment_status"] != "unpaid" {
			t.Fatalf("the draft had no due date or status: %v", draft)
		}

		edited := shop.call(t, http.MethodPut, "/api/invoices/"+invoiceId, map[string]any{
			"kind": "invoice", "customer_id": shop.customerId,
			"lines": []map[string]any{{"product_id": shop.productId, "quantity": 5, "unit_price": 70000}},
		}, http.StatusOK).Data()
		if edited["total"] != float64(350000) || edited["number"] != draft["number"] {
			t.Fatalf("the edit gave %v", edited)
		}

		if early := shop.harness.Call(http.MethodPost, "/api/invoices/"+invoiceId+"/payments", shop.company.OwnerToken, map[string]any{"method": "cash", "amount": 1000}); early.Status != http.StatusConflict {
			t.Fatalf("paying a draft returned %d", early.Status)
		}
		shop.call(t, http.MethodPost, "/api/invoices/"+invoiceId+"/issue", nil, http.StatusOK)
		if locked := shop.harness.Call(http.MethodPut, "/api/invoices/"+invoiceId, shop.company.OwnerToken, map[string]any{"kind": "invoice", "customer_id": shop.customerId, "lines": []map[string]any{{"description": "x", "quantity": 1, "unit_price": 1}}}); locked.Code() != "not_draft" {
			t.Fatalf("editing an issued invoice returned %d %v", locked.Status, locked.Body)
		}

		partPaid := shop.call(t, http.MethodPost, "/api/invoices/"+invoiceId+"/payments", map[string]any{"method": "mobile", "amount": 100000, "reference": "MPESA123"}, http.StatusOK).Data()
		if partPaid["payment_status"] != "part_paid" || partPaid["balance_due"] != float64(250000) {
			t.Fatalf("after a part payment: %v", partPaid)
		}
		if shop.customerBalance(t) != -100000 || shop.stockLeft(t) != 10 {
			t.Fatalf("paying before delivery moved debt %v or stock %d", shop.customerBalance(t), shop.stockLeft(t))
		}

		delivered := shop.call(t, http.MethodPost, "/api/invoices/"+invoiceId+"/deliver", nil, http.StatusOK).Data()
		if delivered["delivered_at"] == nil || shop.stockLeft(t) != 5 || shop.customerBalance(t) != 250000 {
			t.Fatalf("delivery left stock %d and balance %v: %v", shop.stockLeft(t), shop.customerBalance(t), delivered)
		}
		if again := shop.harness.Call(http.MethodPost, "/api/invoices/"+invoiceId+"/deliver", shop.company.OwnerToken, nil); again.Code() != "already_delivered" {
			t.Fatalf("a second delivery returned %d %v", again.Status, again.Body)
		}
		if tooMuch := shop.harness.Call(http.MethodPost, "/api/invoices/"+invoiceId+"/payments", shop.company.OwnerToken, map[string]any{"method": "cash", "amount": 250001}); tooMuch.Code() != "payment_too_high" {
			t.Fatalf("an overpayment returned %d %v", tooMuch.Status, tooMuch.Body)
		}

		paid := shop.call(t, http.MethodPost, "/api/invoices/"+invoiceId+"/payments", map[string]any{"method": "cash", "amount": 250000}, http.StatusOK).Data()
		if paid["payment_status"] != "paid" || paid["balance_due"] != float64(0) || shop.customerBalance(t) != 0 {
			t.Fatalf("after paying in full: %v, balance %v", paid, shop.customerBalance(t))
		}

		invoiceEntries := shop.harness.QueryIntForCompany(shop.company.Id, `SELECT COUNT(*) FROM journal_entries WHERE company_id = $1 AND source_type = 'invoice' AND source_id = $2`, shop.company.Id, invoiceId)
		unbalanced := shop.harness.QueryIntForCompany(shop.company.Id, `
			SELECT COUNT(*) FROM (
				SELECT e.id FROM journal_entries e JOIN journal_lines l ON l.company_id = e.company_id AND l.entry_id = e.id
				WHERE e.company_id = $1 GROUP BY e.id HAVING SUM(l.debit) <> SUM(l.credit)
			) unbalanced_entries`, shop.company.Id)
		cogs := shop.harness.QueryIntForCompany(shop.company.Id, `
			SELECT COALESCE(SUM(l.debit), 0) FROM journal_lines l
			JOIN journal_entries e ON e.company_id = l.company_id AND e.id = l.entry_id
			JOIN accounts a ON a.company_id = l.company_id AND a.id = l.account_id
			WHERE l.company_id = $1 AND e.source_type = 'invoice' AND a.system_key = 'cogs'`, shop.company.Id)
		if invoiceEntries != 1 || unbalanced != 0 || cogs != 5*60000 {
			t.Fatalf("the books have %d invoice entries, %d unbalanced entries and cost %d", invoiceEntries, unbalanced, cogs)
		}

		statement := shop.call(t, http.MethodGet, "/api/customers/"+shop.customerId+"/statement", nil, http.StatusOK).Data()
		sawInvoice := false
		for _, entry := range statement["entries"].([]any) {
			entryFields := entry.(map[string]any)
			if entryFields["kind"] == "invoice" && entryFields["invoice_id"] == invoiceId && entryFields["debit"] == float64(350000) {
				sawInvoice = true
			}
		}
		if !sawInvoice {
			t.Fatalf("the statement did not show the invoice: %v", statement["entries"])
		}

		pdf := shop.call(t, http.MethodGet, "/api/invoices/"+invoiceId+"/document?lang=en", nil, http.StatusOK)
		if !bytes.HasPrefix(pdf.Raw, []byte("%PDF")) || !containsUtf16(pdf.Raw, "Invoice") || !containsUtf16(pdf.Raw, "Juma Traders") {
			t.Fatal("the invoice PDF is missing its title or customer")
		}
	})
}

func TestCancellingADeliveredInvoicePutsEverythingBack(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		shop := openInvoiceShop(t, engineCase, true)
		invoiceId := shop.call(t, http.MethodPost, "/api/invoices", map[string]any{
			"kind": "invoice", "customer_id": shop.customerId,
			"lines": []map[string]any{{"product_id": shop.productId, "quantity": 3}},
		}, http.StatusCreated).Data()["id"].(string)
		shop.call(t, http.MethodPost, "/api/invoices/"+invoiceId+"/issue", nil, http.StatusOK)
		shop.call(t, http.MethodPost, "/api/invoices/"+invoiceId+"/deliver", nil, http.StatusOK)
		paidInvoice := shop.call(t, http.MethodPost, "/api/invoices/"+invoiceId+"/payments", map[string]any{"method": "cash", "amount": 10000}, http.StatusOK).Data()

		if refused := shop.harness.Call(http.MethodPost, "/api/invoices/"+invoiceId+"/cancel", shop.company.OwnerToken, map[string]any{"reason": "Wrong customer"}); refused.Code() != "has_payments" {
			t.Fatalf("cancelling with a payment returned %d %v", refused.Status, refused.Body)
		}
		paymentId := paidInvoice["payments"].([]any)[0].(map[string]any)["id"].(string)
		voided := shop.call(t, http.MethodPost, "/api/invoices/"+invoiceId+"/payments/"+paymentId+"/void", map[string]any{"reason": "Entered twice"}, http.StatusOK).Data()
		if voided["paid_total"] != float64(0) || !voided["payments"].([]any)[0].(map[string]any)["is_voided"].(bool) {
			t.Fatalf("the voided payment still counts: %v", voided)
		}

		cancelled := shop.call(t, http.MethodPost, "/api/invoices/"+invoiceId+"/cancel", map[string]any{"reason": "Wrong customer"}, http.StatusOK).Data()
		if cancelled["status"] != "cancelled" || shop.stockLeft(t) != 10 || shop.customerBalance(t) != 0 {
			t.Fatalf("after cancelling: %v, stock %d, balance %v", cancelled["status"], shop.stockLeft(t), shop.customerBalance(t))
		}
		reversals := shop.harness.QueryIntForCompany(shop.company.Id, `SELECT COUNT(*) FROM journal_entries WHERE company_id = $1 AND source_type = 'invoice_cancel' AND reverses_entry_id IS NOT NULL`, shop.company.Id)
		if reversals != 1 {
			t.Fatalf("the books have %d invoice reversals", reversals)
		}
		if again := shop.harness.Call(http.MethodPost, "/api/invoices/"+invoiceId+"/cancel", shop.company.OwnerToken, map[string]any{"reason": "again"}); again.Code() != "invalid_status" {
			t.Fatalf("cancelling twice returned %d", again.Status)
		}
	})
}

func TestQuotationsBecomeInvoicesAndInvoicesGiveDeliveryNotes(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		shop := openInvoiceShop(t, engineCase, false)
		year := time.Now().Year()
		quotation := shop.call(t, http.MethodPost, "/api/invoices", map[string]any{
			"kind": "quotation", "customer_id": shop.customerId, "terms": "Valid while stock lasts",
			"lines": []map[string]any{{"product_id": shop.productId, "quantity": 2, "unit_price": 70000}, {"description": "Installation", "quantity": 1, "unit_price": 15000}},
		}, http.StatusCreated).Data()
		quotationId := quotation["id"].(string)
		if quotation["number"] != fmt.Sprintf("QUO-%d-0001", year) || quotation["payment_status"] != nil {
			t.Fatalf("the quotation was %v", quotation)
		}
		if notYet := shop.harness.Call(http.MethodPost, "/api/invoices/"+quotationId+"/convert", shop.company.OwnerToken, map[string]any{"kind": "invoice"}); notYet.Code() != "cannot_convert" {
			t.Fatalf("converting a draft quotation returned %d", notYet.Status)
		}
		if notABill := shop.harness.Call(http.MethodPost, "/api/invoices/"+quotationId+"/deliver", shop.company.OwnerToken, nil); notABill.Code() != "not_a_bill" {
			t.Fatalf("delivering a quotation returned %d", notABill.Status)
		}
		shop.call(t, http.MethodPost, "/api/invoices/"+quotationId+"/issue", nil, http.StatusOK)

		invoice := shop.call(t, http.MethodPost, "/api/invoices/"+quotationId+"/convert", map[string]any{"kind": "invoice"}, http.StatusCreated).Data()
		invoiceId := invoice["id"].(string)
		if invoice["kind"] != "invoice" || invoice["number"] != fmt.Sprintf("INV-%d-0001", year) || invoice["total"] != quotation["total"] || invoice["source"].(map[string]any)["number"] != quotation["number"] {
			t.Fatalf("the converted invoice was %v", invoice)
		}
		acceptedQuotation := shop.call(t, http.MethodGet, "/api/invoices/"+quotationId, nil, http.StatusOK).Data()
		if acceptedQuotation["status"] != "accepted" || len(acceptedQuotation["derived"].([]any)) != 1 {
			t.Fatalf("the quotation after converting: %v", acceptedQuotation)
		}

		shop.call(t, http.MethodPost, "/api/invoices/"+invoiceId+"/issue", nil, http.StatusOK)
		deliveryNote := shop.call(t, http.MethodPost, "/api/invoices/"+invoiceId+"/convert", map[string]any{"kind": "delivery_note"}, http.StatusCreated).Data()
		if deliveryNote["kind"] != "delivery_note" || deliveryNote["due_date"] != nil || deliveryNote["terms"] != nil || len(deliveryNote["lines"].([]any)) != 2 {
			t.Fatalf("the delivery note was %v", deliveryNote)
		}
		notePdf := shop.call(t, http.MethodGet, "/api/invoices/"+deliveryNote["id"].(string)+"/document?lang=sw", nil, http.StatusOK)
		if !containsUtf16(notePdf.Raw, "Hati ya kupokelea") || !containsUtf16(notePdf.Raw, "Juma Traders") {
			t.Fatal("the Swahili delivery note PDF is missing its title or customer")
		}

		listed := shop.call(t, http.MethodGet, "/api/invoices?kind=invoice", nil, http.StatusOK)
		if len(listed.Items()) != 1 {
			t.Fatalf("listing invoices gave %d", len(listed.Items()))
		}
		searched := shop.call(t, http.MethodGet, "/api/invoices?q=juma", nil, http.StatusOK)
		if len(searched.Items()) != 3 {
			t.Fatalf("searching by customer gave %d", len(searched.Items()))
		}
		unpaid := shop.call(t, http.MethodGet, "/api/invoices?payment=unpaid", nil, http.StatusOK)
		if len(unpaid.Items()) != 1 {
			t.Fatalf("unpaid filter gave %d", len(unpaid.Items()))
		}
		summary := shop.call(t, http.MethodGet, "/api/invoices/summary", nil, http.StatusOK).Data()
		if summary["owed_to_you"] != quotation["total"] || summary["awaiting_delivery_count"] != float64(1) {
			t.Fatalf("the summary was %v", summary)
		}
		if refused := shop.harness.Call(http.MethodGet, "/api/invoices?payment=someday", shop.company.OwnerToken, nil); refused.Code() != "invalid_filter" {
			t.Fatalf("a bad filter returned %d", refused.Status)
		}
	})
}

func TestInvoiceLinesAreChecked(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		shop := openInvoiceShop(t, engineCase, false)
		cases := map[string]map[string]any{
			"party_required":            {"kind": "invoice", "lines": []map[string]any{{"description": "Labour", "quantity": 1, "unit_price": 1000}}},
			"line_description_required": {"kind": "invoice", "customer_id": shop.customerId, "lines": []map[string]any{{"quantity": 1, "unit_price": 1000}}},
			"line_price_required":       {"kind": "invoice", "customer_id": shop.customerId, "lines": []map[string]any{{"description": "Labour", "quantity": 1}}},
			"discount_too_high":         {"kind": "invoice", "customer_id": shop.customerId, "lines": []map[string]any{{"description": "Labour", "quantity": 1, "unit_price": 1000, "discount_amount": 1001}}},
			"due_before_issue":          {"kind": "invoice", "customer_id": shop.customerId, "issue_date": "2026-10-10", "due_date": "2026-10-01", "lines": []map[string]any{{"description": "Labour", "quantity": 1, "unit_price": 1000}}},
			"invalid_date":              {"kind": "invoice", "customer_id": shop.customerId, "issue_date": "2026-13-40", "lines": []map[string]any{{"description": "Labour", "quantity": 1, "unit_price": 1000}}},
			"not_found":                 {"kind": "invoice", "customer_id": shop.customerId, "lines": []map[string]any{{"product_id": uuid.NewString(), "quantity": 1}}},
		}
		for wantCode, body := range cases {
			refused := shop.harness.Call(http.MethodPost, "/api/invoices", shop.company.OwnerToken, body)
			if refused.Code() != wantCode {
				t.Fatalf("%v returned %d %v, want %s", body, refused.Status, refused.Body, wantCode)
			}
		}

		tooMany := shop.call(t, http.MethodPost, "/api/invoices", map[string]any{
			"kind": "invoice", "customer_id": shop.customerId,
			"lines": []map[string]any{{"product_id": shop.productId, "quantity": 11}},
		}, http.StatusCreated).Data()["id"].(string)
		shop.call(t, http.MethodPost, "/api/invoices/"+tooMany+"/issue", nil, http.StatusOK)
		if short := shop.harness.Call(http.MethodPost, "/api/invoices/"+tooMany+"/deliver", shop.company.OwnerToken, nil); short.Status != http.StatusConflict && short.Status != http.StatusBadRequest {
			t.Fatalf("delivering more than the shop has returned %d %v", short.Status, short.Body)
		}
		if shop.stockLeft(t) != 10 {
			t.Fatalf("a refused delivery still moved stock: %d", shop.stockLeft(t))
		}
	})
}

func TestStaffNeedInvoicePermissionsAndTheirShop(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		shop := openInvoiceShop(t, engineCase, false)
		invoiceId := shop.call(t, http.MethodPost, "/api/invoices", map[string]any{
			"kind": "invoice", "customer_id": shop.customerId, "lines": []map[string]any{{"description": "Labour", "quantity": 1, "unit_price": 1000}},
		}, http.StatusCreated).Data()["id"].(string)

		cashierToken := shop.harness.CreateStaff(shop.company, "cashier@invoices.test", []string{"sales:create"}, []uuid.UUID{shop.company.ShopId})
		if refused := shop.harness.Call(http.MethodGet, "/api/invoices/"+invoiceId, cashierToken, nil); refused.Status != http.StatusForbidden {
			t.Fatalf("a cashier without invoices:view got %d", refused.Status)
		}
		viewerToken := shop.harness.CreateStaff(shop.company, "viewer@invoices.test", []string{"invoices:view"}, []uuid.UUID{shop.company.ShopId})
		if seen := shop.harness.Call(http.MethodGet, "/api/invoices/"+invoiceId, viewerToken, nil); seen.Status != http.StatusOK {
			t.Fatalf("a viewer got %d", seen.Status)
		}
		if refused := shop.harness.Call(http.MethodPost, "/api/invoices/"+invoiceId+"/cancel", viewerToken, map[string]any{"reason": "x"}); refused.Status != http.StatusForbidden {
			t.Fatalf("a viewer could cancel: %d", refused.Status)
		}

		otherCompany := shop.harness.CreateCompany("Other Invoice Shop", "owner@otherinvoices.test")
		shop.harness.Call(http.MethodPut, "/api/features", otherCompany.OwnerToken, map[string]any{"customers_enabled": true, "invoices_enabled": true})
		if foreign := shop.harness.Call(http.MethodGet, "/api/invoices/"+invoiceId, otherCompany.OwnerToken, nil); foreign.Status != http.StatusNotFound {
			t.Fatalf("another company's owner got %d", foreign.Status)
		}
	})
}
