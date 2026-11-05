package invoices_test

import (
	"net/http"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/google/uuid"
)

func TestInvoiceLinesTakeExtraDiscountEachAndBrk(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		shop := openInvoiceShop(t, engineCase, true)
		shop.call(t, http.MethodPut, "/api/features", map[string]any{"winga_enabled": true}, http.StatusOK)

		draft := shop.call(t, http.MethodPost, "/api/invoices", map[string]any{
			"kind": "invoice", "customer_id": shop.customerId,
			"lines": []map[string]any{
				{"product_id": shop.productId, "quantity": 3, "extra": map[string]any{"kind": "amount", "value": 1000}, "discount": map[string]any{"kind": "percent", "value": 1000}},
				{"description": "Tiles from Kariakoo", "quantity": 2, "unit_price": 30000, "is_winga": true, "winga_share": 8000},
			},
		}, http.StatusCreated).Data()
		lines := draft["lines"].([]any)
		riceLine := lines[0].(map[string]any)
		brkLine := lines[1].(map[string]any)
		if riceLine["unit_price"] != float64(73000) || riceLine["extra_unit_amount"] != float64(1000) || riceLine["discount_amount"] != float64(3*7300) || riceLine["line_total"] != float64(3*73000-3*7300) {
			t.Fatalf("the rice line with an extra and 10%% off each was %v", riceLine)
		}
		if brkLine["is_winga"] != true || brkLine["winga_share"] != float64(8000) || brkLine["line_total"] != float64(60000) || draft["total"] != float64(197100+60000) {
			t.Fatalf("the Brk line was %v, total %v", brkLine, draft["total"])
		}

		invoiceId := draft["id"].(string)
		shop.call(t, http.MethodPost, "/api/invoices/"+invoiceId+"/issue", nil, http.StatusOK)
		converted := shop.call(t, http.MethodPost, "/api/invoices/"+invoiceId+"/convert", map[string]any{"kind": "delivery_note"}, http.StatusCreated).Data()
		copiedBrkLine := converted["lines"].([]any)[1].(map[string]any)
		if copiedBrkLine["is_winga"] != true || copiedBrkLine["winga_share"] != float64(8000) {
			t.Fatalf("the delivery note lost the Brk line: %v", copiedBrkLine)
		}

		shop.call(t, http.MethodPost, "/api/invoices/"+invoiceId+"/deliver", nil, http.StatusOK)
		if shop.stockLeft(t) != 7 {
			t.Fatalf("delivery left %d bags; the Brk line must not touch stock", shop.stockLeft(t))
		}
		cogs := shop.harness.QueryIntForCompany(shop.company.Id, `
			SELECT COALESCE(SUM(l.debit), 0) FROM journal_lines l
			JOIN journal_entries e ON e.company_id = l.company_id AND e.id = l.entry_id
			JOIN accounts a ON a.company_id = l.company_id AND a.id = l.account_id
			WHERE l.company_id = $1 AND e.source_type = 'invoice' AND a.system_key = 'cogs'`, shop.company.Id)
		if cogs != 3*60000+8000 {
			t.Fatalf("the invoice booked cost %d, want the rice cost plus the Brk share", cogs)
		}

		noDiscountsToken := shop.harness.CreateStaff(shop.company, "clerk@invoices.test", []string{"invoices:view", "invoices:create"}, []uuid.UUID{shop.company.ShopId})
		refused := shop.harness.Call(http.MethodPost, "/api/invoices", noDiscountsToken, map[string]any{
			"kind": "invoice", "customer_id": shop.customerId,
			"lines": []map[string]any{{"product_id": shop.productId, "quantity": 1, "discount": map[string]any{"kind": "amount", "value": 500}}},
		})
		if refused.Code() != "till_discount_not_allowed" {
			t.Fatalf("a discount without the permission returned %d %v", refused.Status, refused.Body)
		}
	})
}

func TestOwnerChoosesWhichInvoicePartsToHide(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		shop := openInvoiceShop(t, engineCase, false)
		if refused := shop.harness.Call(http.MethodPut, "/api/settings", shop.company.OwnerToken, map[string]any{"invoice_hidden_parts": []string{"logo", "watermark"}}); refused.Status != http.StatusBadRequest {
			t.Fatalf("an unknown part returned %d %v", refused.Status, refused.Body)
		}
		saved := shop.call(t, http.MethodPut, "/api/settings", map[string]any{"invoice_hidden_parts": []string{"logo", "terms", "logo", "signatures"}}, http.StatusOK).Data()
		hiddenParts := saved["company"].(map[string]any)["invoice_hidden_parts"].([]any)
		if len(hiddenParts) != 3 || hiddenParts[0] != "logo" || hiddenParts[2] != "signatures" {
			t.Fatalf("the saved parts were %v", hiddenParts)
		}

		invoiceId := shop.call(t, http.MethodPost, "/api/invoices", map[string]any{
			"kind": "invoice", "customer_id": shop.customerId, "terms": "Goods once sold are not taken back",
			"lines": []map[string]any{{"product_id": shop.productId, "quantity": 1}},
		}, http.StatusCreated).Data()["id"].(string)
		pdf := shop.call(t, http.MethodGet, "/api/invoices/"+invoiceId+"/document?lang=en", nil, http.StatusOK)
		if !containsUtf16(pdf.Raw, "Juma Traders") || containsUtf16(pdf.Raw, "Goods once sold") || containsUtf16(pdf.Raw, "Received by") {
			t.Fatal("the PDF still shows the hidden terms or signatures")
		}

		cleared := shop.call(t, http.MethodPut, "/api/settings", map[string]any{"invoice_hidden_parts": []string{}}, http.StatusOK).Data()
		if len(cleared["company"].(map[string]any)["invoice_hidden_parts"].([]any)) != 0 {
			t.Fatalf("clearing the parts left %v", cleared["company"])
		}
	})
}

func TestInvoiceProductLinesTakeTheWholesalePriceLikeTheTill(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		shop := openInvoiceShop(t, engineCase, false)
		cementId := shop.call(t, http.MethodPost, "/api/products", map[string]any{"sku": "CEM50", "name": "Cement 50kg", "price": 20000, "wholesale_price": 18500, "wholesale_min": 10, "opening_quantity": 100}, http.StatusCreated).Data()["id"].(string)
		draft := shop.call(t, http.MethodPost, "/api/invoices", map[string]any{
			"kind": "invoice", "customer_id": shop.customerId,
			"lines": []map[string]any{{"product_id": cementId, "quantity": 9}, {"product_id": cementId, "quantity": 10}},
		}, http.StatusCreated).Data()
		lines := draft["lines"].([]any)
		if lines[0].(map[string]any)["unit_price"] != float64(20000) || lines[1].(map[string]any)["unit_price"] != float64(18500) {
			t.Fatalf("9 and 10 bags were priced %v and %v", lines[0].(map[string]any)["unit_price"], lines[1].(map[string]any)["unit_price"])
		}
	})
}
