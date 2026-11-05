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
