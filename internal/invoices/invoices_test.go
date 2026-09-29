package invoices_test

import (
	"bytes"
	"net/http"
	"strings"
	"testing"
	"unicode/utf16"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/google/uuid"
)

func containsUtf16(pdfBytes []byte, wanted string) bool {
	encoded := []byte{}
	for _, unit := range utf16.Encode([]rune(wanted)) {
		encoded = append(encoded, byte(unit>>8), byte(unit))
	}
	return bytes.Contains(pdfBytes, encoded)
}

func TestSaleDocumentIsAnA4InvoiceOnlyForItsCompany(t *testing.T) {
	for modeName, startHarness := range map[string]func(*testing.T, testkit.EngineCase) *apptest.Harness{"cloud": apptest.Start, "desktop": apptest.StartDesktop} {
		t.Run(modeName, func(t *testing.T) {
			testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
				checkSaleDocument(t, startHarness(t, engineCase))
			})
		})
	}
}

func checkSaleDocument(t *testing.T, harness *apptest.Harness) {
	company := harness.CreateCompany("Invoice Shop", "owner@invoice.test")
	otherCompany := harness.CreateCompany("Other Invoice Shop", "owner@otherinvoice.test")

	settingsSaved := harness.Call(http.MethodPut, "/api/settings", company.OwnerToken, map[string]any{
		"receipt_header": "Karibu sana\nOpen every day",
		"receipt_footer": "Goods sold are not returned",
		"business_tin":   "123-456-789",
	})
	if settingsSaved.Status != http.StatusOK {
		t.Fatalf("settings returned %d %v", settingsSaved.Status, settingsSaved.Body)
	}

	createdProduct := harness.Call(http.MethodPost, "/api/products", company.OwnerToken, map[string]any{"sku": "SODA", "name": "Soda — baridi", "price": 1180, "cost_price": 600, "opening_quantity": 50})
	if createdProduct.Status != http.StatusCreated {
		t.Fatalf("product returned %d %v", createdProduct.Status, createdProduct.Body)
	}
	createdSale := harness.Call(http.MethodPost, "/api/sales", company.OwnerToken, map[string]any{
		"client_ref": "invoice-sale-1",
		"items":      []map[string]any{{"product_id": createdProduct.Data()["id"], "quantity": 3}},
		"payments":   []map[string]any{{"method": "cash", "amount": 5000}},
	})
	if createdSale.Status != http.StatusCreated {
		t.Fatalf("sale returned %d %v", createdSale.Status, createdSale.Body)
	}
	saleId := createdSale.Data()["id"].(string)
	receiptNumber := createdSale.Data()["receipt_number"].(string)
	harness.ExecForCompany(company.Id, `INSERT INTO fiscal_receipts (company_id, sale_id, status, attempts, verification_code, verification_url) VALUES ($1, $2, 'sent', 1, 'ABC123', 'https://verify.tra.go.tz/ABC123')`, company.Id, saleId)

	invoice := harness.Call(http.MethodGet, "/api/sales/"+saleId+"/document?format=pdf", company.OwnerToken, nil)
	if invoice.Status != http.StatusOK || !bytes.HasPrefix(invoice.Raw, []byte("%PDF")) || invoice.Headers.Get("Content-Type") != "application/pdf" {
		t.Fatalf("the invoice returned %d %v", invoice.Status, invoice.Body)
	}
	if invoice.Headers.Get("Content-Disposition") != `attachment; filename="invoice-`+receiptNumber+`.pdf"` {
		t.Fatalf("the invoice disposition was %q", invoice.Headers.Get("Content-Disposition"))
	}
	if !containsUtf16(invoice.Raw, "Invoice") {
		t.Fatal("the invoice title is missing")
	}

	receiptCopy := harness.Call(http.MethodGet, "/api/sales/"+saleId+"/document?kind=receipt", company.OwnerToken, nil)
	if receiptCopy.Status != http.StatusOK || !strings.Contains(receiptCopy.Headers.Get("Content-Disposition"), "receipt-"+receiptNumber+".pdf") {
		t.Fatalf("the receipt copy returned %d %q", receiptCopy.Status, receiptCopy.Headers.Get("Content-Disposition"))
	}

	swahiliSaved := harness.Call(http.MethodPut, "/api/settings", company.OwnerToken, map[string]any{"receipt_language": "sw"})
	if swahiliSaved.Status != http.StatusOK {
		t.Fatalf("receipt language returned %d", swahiliSaved.Status)
	}
	swahiliInvoice := harness.Call(http.MethodGet, "/api/sales/"+saleId+"/document", company.OwnerToken, nil)
	if swahiliInvoice.Status != http.StatusOK || !containsUtf16(swahiliInvoice.Raw, "Ankara") {
		t.Fatal("the invoice did not follow the Swahili receipt language")
	}
	englishOverride := harness.Call(http.MethodGet, "/api/sales/"+saleId+"/document?lang=en&kind=receipt", company.OwnerToken, nil)
	if !containsUtf16(englishOverride.Raw, "Receipt") {
		t.Fatal("lang=en did not override the receipt language")
	}

	if refused := harness.Call(http.MethodGet, "/api/sales/"+saleId+"/document?format=xlsx", company.OwnerToken, nil); refused.Status != http.StatusBadRequest || refused.Code() != "invalid_format" {
		t.Fatalf("an Excel invoice returned %d %v", refused.Status, refused.Body)
	}
	if refused := harness.Call(http.MethodGet, "/api/sales/"+saleId+"/document?kind=quote", company.OwnerToken, nil); refused.Status != http.StatusBadRequest {
		t.Fatalf("an unknown kind returned %d", refused.Status)
	}
	if foreign := harness.Call(http.MethodGet, "/api/sales/"+saleId+"/document", otherCompany.OwnerToken, nil); foreign.Status != http.StatusNotFound || foreign.Code() != "not_found" {
		t.Fatalf("another company's owner got %d %v", foreign.Status, foreign.Body)
	}
	if missing := harness.Call(http.MethodGet, "/api/sales/"+uuid.NewString()+"/document", company.OwnerToken, nil); missing.Status != http.StatusNotFound {
		t.Fatalf("a missing sale returned %d", missing.Status)
	}
	if malformed := harness.Call(http.MethodGet, "/api/sales/not-a-sale/document", company.OwnerToken, nil); malformed.Status != http.StatusNotFound {
		t.Fatalf("a malformed id returned %d", malformed.Status)
	}

	cashierToken := harness.CreateStaff(company, "cashier@invoice.test", []string{"sales:create"}, []uuid.UUID{company.ShopId})
	if cashierCopy := harness.Call(http.MethodGet, "/api/sales/"+saleId+"/document", cashierToken, nil); cashierCopy.Status != http.StatusOK {
		t.Fatalf("a cashier who sells got %d", cashierCopy.Status)
	}
	stockKeeperToken := harness.CreateStaff(company, "stock@invoice.test", []string{"stock_movements:view"}, []uuid.UUID{company.ShopId})
	if refused := harness.Call(http.MethodGet, "/api/sales/"+saleId+"/document", stockKeeperToken, nil); refused.Status != http.StatusForbidden {
		t.Fatalf("staff without sales permissions got %d", refused.Status)
	}
}
