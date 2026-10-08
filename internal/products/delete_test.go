package products_test

import (
	"net/http"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/google/uuid"
)

func TestDeletingProductsRemovesUnusedOnesAndSkipsSoldOnes(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Delete Shop", "owner@delete.test")
		otherCompany := harness.CreateCompany("Other Delete", "owner@otherdelete.test")
		harness.Call(http.MethodPut, "/api/features", company.OwnerToken, map[string]any{"accounting_mode": "simple"})
		booksStarted := harness.Call(http.MethodPost, "/api/accounting/start", company.OwnerToken, map[string]any{"mode": "today"})
		if booksStarted.Status != http.StatusCreated {
			t.Fatalf("starting the books returned %d %v", booksStarted.Status, booksStarted.Body)
		}
		cashierToken := harness.CreateStaff(company, "cashier@delete.test", []string{"sales:create", "products:view"}, []uuid.UUID{company.ShopId})

		typoId := createProduct(t, harness, company.OwnerToken, map[string]any{"sku": "TYPO", "name": "Sugr", "price": 3000, "cost_price": 2000, "opening_quantity": 10})["id"].(string)
		soldId := createProduct(t, harness, company.OwnerToken, map[string]any{"sku": "SOLD", "name": "Rice", "price": 5000, "opening_quantity": 10})["id"].(string)
		sodaId := createProduct(t, harness, company.OwnerToken, map[string]any{"sku": "SODA", "name": "Soda", "price": 1000})["id"].(string)
		largeSodaId := createProduct(t, harness, company.OwnerToken, map[string]any{"sku": "SODA-1L", "name": "Soda", "price": 2000, "parent_id": sodaId, "variant_label": "1 litre", "opening_quantity": 5})["id"].(string)
		foreignId := createProduct(t, harness, otherCompany.OwnerToken, map[string]any{"sku": "FOREIGN", "name": "Foreign", "price": 100})["id"].(string)

		sold := harness.Call(http.MethodPost, "/api/sales", company.OwnerToken, map[string]any{
			"client_ref": "delete-sale-0001", "items": []any{map[string]any{"product_id": soldId, "quantity": 1}},
			"payments": []any{map[string]any{"method": "cash", "amount": 5000}},
		})
		if sold.Status != http.StatusCreated {
			t.Fatalf("selling returned %d %v", sold.Status, sold.Body)
		}

		deleteBody := map[string]any{"product_ids": []string{typoId, soldId, sodaId, foreignId}}
		if harness.Call(http.MethodPost, "/api/products/delete", cashierToken, deleteBody).Status != http.StatusForbidden {
			t.Fatal("a cashier deleted products")
		}

		deleted := harness.Call(http.MethodPost, "/api/products/delete", company.OwnerToken, deleteBody)
		deletedIds, _ := deleted.Data()["deleted"].([]any)
		skipped, _ := deleted.Data()["skipped"].([]any)
		if deleted.Status != http.StatusOK || len(deletedIds) != 3 || len(skipped) != 1 {
			t.Fatalf("deleting returned %d %v", deleted.Status, deleted.Body)
		}
		skippedProduct := skipped[0].(map[string]any)
		if skippedProduct["id"] != soldId || skippedProduct["reason"] != "sold" || skippedProduct["name"] != "Rice" {
			t.Fatalf("the skipped product is %v", skippedProduct)
		}

		for _, goneId := range []string{typoId, sodaId, largeSodaId} {
			if harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM products WHERE id = $1`, goneId) != 0 {
				t.Fatalf("product %s is still there", goneId)
			}
			if harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM stock_movements WHERE product_id = $1`, goneId) != 0 {
				t.Fatalf("product %s still has stock movements", goneId)
			}
		}
		if harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM products WHERE id = $1`, soldId) != 1 {
			t.Fatal("the sold product was deleted")
		}
		if harness.QueryIntForCompany(otherCompany.Id, `SELECT COUNT(*) FROM products WHERE id = $1`, foreignId) != 1 {
			t.Fatal("another business's product was deleted")
		}
		openingEntries := harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM journal_entries WHERE source_type = 'stock_adjustment'`)
		reversals := harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM journal_entries WHERE source_type = 'reversal' AND reverses_entry_id IS NOT NULL`)
		if openingEntries != 1 || reversals != 1 {
			t.Fatalf("the books have %d opening entries and %d reversals, want 1 and 1 (only the typo had a cost)", openingEntries, reversals)
		}
		inventoryLeft := harness.QueryIntForCompany(company.Id, `
			SELECT CAST(COALESCE(SUM(l.debit - l.credit), 0) AS BIGINT)
			FROM journal_lines l JOIN accounts a ON a.company_id = l.company_id AND a.id = l.account_id
			WHERE a.system_key = 'inventory'`)
		if inventoryLeft != 0 {
			t.Fatalf("the books still hold %d of stock value; only the sold rice (no cost) is left", inventoryLeft)
		}
	})
}
