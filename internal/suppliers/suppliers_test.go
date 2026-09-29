package suppliers_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/config"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/google/uuid"
)

func switchOn(t *testing.T, harness *apptest.Harness, ownerToken string, changes map[string]any) {
	t.Helper()
	saved := harness.Call(http.MethodPut, "/api/features", ownerToken, changes)
	if saved.Status != http.StatusOK {
		t.Fatalf("features %v returned %d %v", changes, saved.Status, saved.Body)
	}
}

func newProduct(t *testing.T, harness *apptest.Harness, sessionToken string, sku string, openingQuantity int, costPrice int) string {
	t.Helper()
	created := harness.Call(http.MethodPost, "/api/products", sessionToken, map[string]any{
		"sku": sku, "name": "Product " + sku, "price": 5000, "cost_price": costPrice, "opening_quantity": openingQuantity,
	})
	if created.Status != http.StatusCreated {
		t.Fatalf("create %s returned %d: %v", sku, created.Status, created.Body)
	}
	return created.Data()["id"].(string)
}

func newSupplier(t *testing.T, harness *apptest.Harness, sessionToken string, fields map[string]any) string {
	t.Helper()
	created := harness.Call(http.MethodPost, "/api/suppliers", sessionToken, fields)
	if created.Status != http.StatusCreated {
		t.Fatalf("create supplier %v returned %d: %v", fields, created.Status, created.Body)
	}
	return created.Data()["id"].(string)
}

func line(productId string, quantity int, unitCost int) map[string]any {
	return map[string]any{"product_id": productId, "quantity": quantity, "unit_cost": unitCost}
}

func arrival(clientRef string, supplierId any, amountPaid int, lines ...map[string]any) map[string]any {
	return map[string]any{"client_ref": clientRef, "supplier_id": supplierId, "amount_paid": amountPaid, "lines": lines}
}

func mustRecord(t *testing.T, harness *apptest.Harness, sessionToken string, body map[string]any) map[string]any {
	t.Helper()
	recorded := harness.Call(http.MethodPost, "/api/purchases", sessionToken, body)
	if recorded.Status != http.StatusCreated {
		t.Fatalf("stock arrived %v returned %d: %v", body, recorded.Status, recorded.Body)
	}
	return recorded.Data()
}

func expectError(t *testing.T, got apptest.Response, wantStatus int, wantCode string) {
	t.Helper()
	if got.Status != wantStatus || (wantCode != "" && got.Code() != wantCode) {
		t.Fatalf("got %d %s, want %d %s: %v", got.Status, got.Code(), wantStatus, wantCode, got.Body)
	}
}

func balanceOf(t *testing.T, harness *apptest.Harness, sessionToken string, supplierId string) float64 {
	t.Helper()
	found := harness.Call(http.MethodGet, "/api/suppliers/"+supplierId, sessionToken, nil)
	if found.Status != http.StatusOK {
		t.Fatalf("get supplier returned %d %v", found.Status, found.Body)
	}
	return found.Data()["balance"].(float64)
}

func stockIn(harness *apptest.Harness, companyId uuid.UUID, shopId string, productId string) int64 {
	return harness.QueryIntForCompany(companyId, `SELECT COALESCE(SUM(quantity), 0) FROM shop_stock WHERE shop_id = $1 AND product_id = $2`, shopId, productId)
}

func costOf(harness *apptest.Harness, companyId uuid.UUID, productId string) int64 {
	return harness.QueryIntForCompany(companyId, `SELECT cost_price FROM products WHERE id = $1`, productId)
}

func TestSuppliersStayHiddenUntilSwitchedOn(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Quiet Shop", "owner@quiet.test")
		riceId := newProduct(t, harness, company.OwnerToken, "RICE", 0, 500)

		expectError(t, harness.Call(http.MethodGet, "/api/suppliers", company.OwnerToken, nil), http.StatusForbidden, "feature_off")
		expectError(t, harness.Call(http.MethodGet, "/api/purchases", company.OwnerToken, nil), http.StatusForbidden, "feature_off")
		expectError(t, harness.Call(http.MethodPost, "/api/purchases", company.OwnerToken, arrival("offline-0001", nil, 1000, line(riceId, 2, 500))), http.StatusForbidden, "feature_off")
		if stockIn(harness, company.Id, company.ShopId.String(), riceId) != 0 {
			t.Fatal("a refused stock arrival still added stock")
		}

		switchOn(t, harness, company.OwnerToken, map[string]any{"accounting_mode": "simple"})
		withoutSupplier := mustRecord(t, harness, company.OwnerToken, arrival("offline-0002", nil, 1000, line(riceId, 2, 500)))
		if withoutSupplier["supplier_id"] != nil || withoutSupplier["payment_status"] != "paid" {
			t.Fatalf("stock arrived without a supplier returned %v", withoutSupplier)
		}
		someSupplier := uuid.Must(uuid.NewV7()).String()
		expectError(t, harness.Call(http.MethodPost, "/api/purchases", company.OwnerToken, arrival("offline-0003", someSupplier, 0, line(riceId, 1, 500))), http.StatusForbidden, "feature_off")
		expectError(t, harness.Call(http.MethodGet, "/api/suppliers", company.OwnerToken, nil), http.StatusForbidden, "feature_off")

		switchOn(t, harness, company.OwnerToken, map[string]any{"suppliers_enabled": true})
		orderBody := map[string]any{"supplier_id": someSupplier, "lines": []any{map[string]any{"product_id": riceId, "quantity": 1}}}
		expectError(t, harness.Call(http.MethodGet, "/api/purchase-orders", company.OwnerToken, nil), http.StatusForbidden, "feature_off")
		expectError(t, harness.Call(http.MethodPost, "/api/purchase-orders", company.OwnerToken, orderBody), http.StatusForbidden, "feature_off")

		viewerToken := harness.CreateStaff(company, "viewer@quiet.test", []string{"suppliers:view", "purchases:view"}, []uuid.UUID{company.ShopId})
		cashierToken := harness.CreateStaff(company, "cashier@quiet.test", []string{"sales:create"}, []uuid.UUID{company.ShopId})
		purchaseId := withoutSupplier["id"].(string)
		if harness.Call(http.MethodGet, "/api/purchases", viewerToken, nil).Status != http.StatusOK {
			t.Fatal("a viewer could not see stock arrived")
		}
		expectError(t, harness.Call(http.MethodPost, "/api/purchases", viewerToken, arrival("viewer-0001", nil, 500, line(riceId, 1, 500))), http.StatusForbidden, "forbidden")
		expectError(t, harness.Call(http.MethodPost, "/api/purchases/"+purchaseId+"/cancel", viewerToken, map[string]any{"reason": "no"}), http.StatusForbidden, "forbidden")
		expectError(t, harness.Call(http.MethodPost, "/api/suppliers", viewerToken, map[string]any{"name": "Sneaky"}), http.StatusForbidden, "forbidden")
		expectError(t, harness.Call(http.MethodGet, "/api/suppliers", cashierToken, nil), http.StatusForbidden, "forbidden")
		expectError(t, harness.Call(http.MethodGet, "/api/purchases", cashierToken, nil), http.StatusForbidden, "forbidden")

		switchOn(t, harness, company.OwnerToken, map[string]any{"suppliers_enabled": false, "accounting_mode": "off"})
		expectError(t, harness.Call(http.MethodGet, "/api/purchases", company.OwnerToken, nil), http.StatusForbidden, "feature_off")
		switchOn(t, harness, company.OwnerToken, map[string]any{"accounting_mode": "full"})
		keptPurchases := harness.Call(http.MethodGet, "/api/purchases", company.OwnerToken, nil)
		if keptPurchases.Status != http.StatusOK || keptPurchases.Data()["total"] != float64(1) {
			t.Fatalf("switching the feature off and on lost data: %v", keptPurchases.Body)
		}
	})
}

func TestSupplierDetailsNamesAndDeactivation(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Named Shop", "owner@named.test")
		otherCompany := harness.CreateCompany("Other Named", "owner@othernamed.test")
		switchOn(t, harness, company.OwnerToken, map[string]any{"suppliers_enabled": true})
		switchOn(t, harness, otherCompany.OwnerToken, map[string]any{"suppliers_enabled": true})
		flourId := newProduct(t, harness, company.OwnerToken, "FLOUR", 0, 0)

		created := harness.Call(http.MethodPost, "/api/suppliers", company.OwnerToken, map[string]any{
			"name": "  Mohamed   Enterprises ", "phone": "0712 345 678", "email": "Sales@Mo.co.tz",
			"payment_terms_days": 30, "opening_balance": 50000, "tin": " 123-456-789 ",
		})
		createdData := created.Data()
		if created.Status != http.StatusCreated || createdData["name"] != "Mohamed Enterprises" || createdData["phone"] != "+255712345678" ||
			createdData["email"] != "sales@mo.co.tz" || createdData["balance"] != float64(50000) || createdData["tin"] != "123-456-789" {
			t.Fatalf("create supplier returned %d %v", created.Status, created.Body)
		}
		supplierId := createdData["id"].(string)

		expectError(t, harness.Call(http.MethodPost, "/api/suppliers", company.OwnerToken, map[string]any{"name": "MOHAMED enterprises"}), http.StatusConflict, "supplier_name_taken")
		expectError(t, harness.Call(http.MethodPost, "/api/suppliers", company.OwnerToken, map[string]any{"name": "Bad Phone", "phone": "12345"}), http.StatusBadRequest, "invalid_phone")
		expectError(t, harness.Call(http.MethodPost, "/api/suppliers", company.OwnerToken, map[string]any{"name": "Bad Mail", "email": "not-an-email"}), http.StatusBadRequest, "invalid_email")
		expectError(t, harness.Call(http.MethodPost, "/api/suppliers", company.OwnerToken, map[string]any{"name": "   "}), http.StatusBadRequest, "supplier_name_required")
		expectError(t, harness.Call(http.MethodPost, "/api/suppliers", company.OwnerToken, map[string]any{"name": "Negative", "opening_balance": -1}), http.StatusBadRequest, "")
		expectError(t, harness.Call(http.MethodPost, "/api/suppliers", company.OwnerToken, map[string]any{"name": "Long Terms", "payment_terms_days": 400}), http.StatusBadRequest, "")

		newSupplier(t, harness, otherCompany.OwnerToken, map[string]any{"name": "Mohamed Enterprises"})
		expectError(t, harness.Call(http.MethodGet, "/api/suppliers/"+supplierId, otherCompany.OwnerToken, nil), http.StatusNotFound, "supplier_not_found")
		expectError(t, harness.Call(http.MethodPut, "/api/suppliers/"+supplierId, otherCompany.OwnerToken, map[string]any{"name": "Taken Over"}), http.StatusNotFound, "supplier_not_found")

		edited := harness.Call(http.MethodPut, "/api/suppliers/"+supplierId, company.OwnerToken, map[string]any{
			"name": "Mohamed Enterprises", "contact_person": "Mzee Mohamed", "payment_terms_days": 14, "opening_balance": 50000,
		})
		if edited.Status != http.StatusOK || edited.Data()["contact_person"] != "Mzee Mohamed" || edited.Data()["phone"] != nil {
			t.Fatalf("edit supplier returned %d %v", edited.Status, edited.Body)
		}

		mustRecord(t, harness, company.OwnerToken, arrival("named-0001", supplierId, 0, line(flourId, 2, 1000)))
		if harness.Call(http.MethodDelete, "/api/suppliers/"+supplierId, company.OwnerToken, nil).Status != http.StatusOK {
			t.Fatal("deactivating the supplier failed")
		}
		activeList := harness.Call(http.MethodGet, "/api/suppliers", company.OwnerToken, nil)
		everyone := harness.Call(http.MethodGet, "/api/suppliers?include_inactive=true", company.OwnerToken, nil)
		if activeList.Data()["total"] != float64(0) || everyone.Data()["total"] != float64(1) {
			t.Fatalf("lists after deactivating: active %v, all %v", activeList.Body, everyone.Body)
		}
		kept := harness.Call(http.MethodGet, "/api/suppliers/"+supplierId, company.OwnerToken, nil).Data()
		if kept["is_active"] != false || kept["balance"] != float64(52000) {
			t.Fatalf("a deactivated supplier lost its history: %v", kept)
		}
		history := harness.Call(http.MethodGet, "/api/purchases?supplier_id="+supplierId, company.OwnerToken, nil).Items()
		if len(history) != 1 || history[0].(map[string]any)["supplier_name"] != "Mohamed Enterprises" {
			t.Fatalf("stock arrived from a deactivated supplier disappeared: %v", history)
		}
		expectError(t, harness.Call(http.MethodPost, "/api/purchases", company.OwnerToken, arrival("named-0002", supplierId, 0, line(flourId, 1, 1000))), http.StatusBadRequest, "supplier_inactive")

		newSupplier(t, harness, company.OwnerToken, map[string]any{"name": "Mohamed Enterprises"})
		expectError(t, harness.Call(http.MethodPut, "/api/suppliers/"+supplierId, company.OwnerToken, map[string]any{"name": "Mohamed Enterprises", "is_active": true}), http.StatusConflict, "supplier_name_taken")
		searched := harness.Call(http.MethodGet, "/api/suppliers?q=moham", company.OwnerToken, nil)
		if searched.Data()["total"] != float64(1) {
			t.Fatalf("search returned %v", searched.Body)
		}
	})
}

func TestStockArrivedAddsStockAveragesCostAndIsIdempotent(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Arrival Shop", "owner@arrival.test")
		otherCompany := harness.CreateCompany("Arrival Other", "owner@arrivalother.test")
		switchOn(t, harness, company.OwnerToken, map[string]any{"suppliers_enabled": true})
		mainShopId := company.ShopId.String()
		supplierId := newSupplier(t, harness, company.OwnerToken, map[string]any{"name": "Kariakoo Wholesalers"})
		secondSupplierId := newSupplier(t, harness, company.OwnerToken, map[string]any{"name": "Mwanza Traders"})
		sugarId := newProduct(t, harness, company.OwnerToken, "SUGAR", 10, 1000)
		flourId := newProduct(t, harness, company.OwnerToken, "FLOUR", 0, 500)
		foreignProductId := newProduct(t, harness, otherCompany.OwnerToken, "FOREIGN", 0, 0)

		firstBody := arrival("arrival-0001", supplierId, 0, line(sugarId, 5, 1300))
		firstBody["supplier_invoice_number"] = " INV-77 "
		firstBody["invoice_date"] = "2026-09-01"
		first := mustRecord(t, harness, company.OwnerToken, firstBody)
		if first["purchase_number"] != "PUR-000001" || first["total"] != float64(6500) || first["payment_status"] != "unpaid" ||
			first["amount_due"] != float64(6500) || first["supplier_invoice_number"] != "INV-77" || first["invoice_date"] != "2026-09-01" ||
			first["supplier_name"] != "Kariakoo Wholesalers" || len(first["lines"].([]any)) != 1 {
			t.Fatalf("first stock arrival returned %v", first)
		}
		if costOf(harness, company.Id, sugarId) != 1100 || stockIn(harness, company.Id, mainShopId, sugarId) != 15 {
			t.Fatalf("after the first arrival cost is %d and stock %d, want 1100 and 15", costOf(harness, company.Id, sugarId), stockIn(harness, company.Id, mainShopId, sugarId))
		}
		purchaseMovements := harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM stock_movements WHERE reason = 'purchase' AND reference = 'PUR-000001' AND change = 5`)
		if purchaseMovements != 1 {
			t.Fatalf("found %d stock movements for PUR-000001, want 1", purchaseMovements)
		}

		retried := mustRecord(t, harness, company.OwnerToken, firstBody)
		if retried["id"] != first["id"] || stockIn(harness, company.Id, mainShopId, sugarId) != 15 || harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM purchases`) != 1 {
			t.Fatalf("retrying with the same client_ref recorded twice: %v", retried)
		}

		secondBody := arrival("arrival-0002", secondSupplierId, 3003, line(sugarId, 3, 1001))
		secondBody["payment_method"] = "mobile"
		second := mustRecord(t, harness, company.OwnerToken, secondBody)
		secondPayments := second["payments"].([]any)
		if second["purchase_number"] != "PUR-000002" || second["payment_status"] != "paid" || len(secondPayments) != 1 ||
			secondPayments[0].(map[string]any)["method"] != "mobile" || costOf(harness, company.Id, sugarId) != 1084 {
			t.Fatalf("second arrival returned %v with cost %d", second, costOf(harness, company.Id, sugarId))
		}

		mustRecord(t, harness, company.OwnerToken, arrival("arrival-0003", nil, 3108, line(flourId, 4, 777)))
		if costOf(harness, company.Id, flourId) != 777 {
			t.Fatalf("empty stock took cost %d, want 777", costOf(harness, company.Id, flourId))
		}

		sugar := harness.Call(http.MethodGet, "/api/products/"+sugarId, company.OwnerToken, nil).Data()
		if sugar["preferred_supplier_id"] != supplierId {
			t.Fatalf("the usual supplier is %v, want the first supplier", sugar["preferred_supplier_id"])
		}
		cleared := harness.Call(http.MethodPut, "/api/products/"+sugarId, company.OwnerToken, map[string]any{
			"sku": "SUGAR", "name": "Product SUGAR", "price": 5000, "cost_price": 1084, "preferred_supplier_id": "",
		})
		if cleared.Status != http.StatusOK || cleared.Data()["preferred_supplier_id"] != nil {
			t.Fatalf("clearing the usual supplier returned %d %v", cleared.Status, cleared.Body)
		}
		chosen := harness.Call(http.MethodPut, "/api/products/"+sugarId, company.OwnerToken, map[string]any{
			"sku": "SUGAR", "name": "Product SUGAR", "price": 5000, "cost_price": 1084, "preferred_supplier_id": secondSupplierId,
		})
		kept := harness.Call(http.MethodPut, "/api/products/"+sugarId, company.OwnerToken, map[string]any{
			"sku": "SUGAR", "name": "Product SUGAR", "price": 5000, "cost_price": 1084,
		})
		if chosen.Data()["preferred_supplier_id"] != secondSupplierId || kept.Data()["preferred_supplier_id"] != secondSupplierId {
			t.Fatalf("choosing then keeping the usual supplier gave %v then %v", chosen.Data()["preferred_supplier_id"], kept.Data()["preferred_supplier_id"])
		}
		expectError(t, harness.Call(http.MethodPut, "/api/products/"+sugarId, company.OwnerToken, map[string]any{
			"sku": "SUGAR", "name": "Product SUGAR", "price": 5000, "preferred_supplier_id": uuid.Must(uuid.NewV7()).String(),
		}), http.StatusBadRequest, "supplier_not_found")

		purchasesBefore := harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM purchases`)
		futureBody := arrival("arrival-0009", supplierId, 0, line(flourId, 1, 100))
		futureBody["received_at"] = time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339)
		badDateBody := arrival("arrival-0010", supplierId, 0, line(flourId, 1, 100))
		badDateBody["invoice_date"] = "01/09/2026"
		refusals := []struct {
			body       map[string]any
			wantStatus int
			wantCode   string
		}{
			{arrival("arrival-0004", nil, 500, line(flourId, 2, 500)), http.StatusBadRequest, "must_pay_in_full"},
			{arrival("arrival-0005", supplierId, 1001, line(flourId, 2, 500)), http.StatusBadRequest, "paid_more_than_total"},
			{arrival("arrival-0006", supplierId, 0, line(flourId, 1, 500), line(flourId, 1, 500)), http.StatusBadRequest, "duplicate_product"},
			{arrival("arrival-0007", supplierId, 0, line(foreignProductId, 1, 500)), http.StatusNotFound, "product_not_found"},
			{arrival("arrival-0008", supplierId, 0, line(flourId, 0, 500)), http.StatusBadRequest, "validation_failed"},
			{futureBody, http.StatusBadRequest, "date_in_future"},
			{badDateBody, http.StatusBadRequest, "invalid_date"},
			{arrival("short", supplierId, 0, line(flourId, 1, 100)), http.StatusBadRequest, "validation_failed"},
		}
		for _, refusal := range refusals {
			expectError(t, harness.Call(http.MethodPost, "/api/purchases", company.OwnerToken, refusal.body), refusal.wantStatus, refusal.wantCode)
		}
		if harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM purchases`) != purchasesBefore || costOf(harness, company.Id, flourId) != 777 {
			t.Fatal("a refused stock arrival still saved something")
		}

		pastBody := arrival("arrival-0011", supplierId, 0, line(flourId, 1, 777))
		pastBody["received_at"] = "2026-01-15T09:00:00Z"
		past := mustRecord(t, harness, company.OwnerToken, pastBody)
		pastReceivedAt, _ := time.Parse(time.RFC3339, past["received_at"].(string))
		if past["purchase_number"] != "PUR-000004" || !pastReceivedAt.Equal(time.Date(2026, 1, 15, 9, 0, 0, 0, time.UTC)) {
			t.Fatalf("a back-dated arrival returned %v", past)
		}

		lastCost := harness.Call(http.MethodGet, "/api/purchases/last-cost?product_id="+sugarId+"&supplier_id="+supplierId, company.OwnerToken, nil).Data()
		if lastCost["unit_cost"] != float64(1300) {
			t.Fatalf("last cost from the first supplier is %v, want 1300", lastCost)
		}
		newestCost := harness.Call(http.MethodGet, "/api/purchases/last-cost?product_id="+sugarId, company.OwnerToken, nil).Data()
		if newestCost["unit_cost"] != float64(1001) {
			t.Fatalf("newest last cost is %v, want 1001", newestCost)
		}
	})
}

func TestVatIsWorkedOutFromTheInvoice(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Vat Shop", "owner@vat.test")
		switchOn(t, harness, company.OwnerToken, map[string]any{"suppliers_enabled": true})
		sodaId := newProduct(t, harness, company.OwnerToken, "SODA", 0, 0)
		juiceId := newProduct(t, harness, company.OwnerToken, "JUICE", 0, 0)
		supplierId := newSupplier(t, harness, company.OwnerToken, map[string]any{"name": "Vat Supplier"})

		beforeVat := arrival("vat-00001", supplierId, 0, line(sodaId, 2, 1180))
		beforeVat["invoice_has_vat"] = true
		beforeVat["prices_include_vat"] = true
		ignored := mustRecord(t, harness, company.OwnerToken, beforeVat)
		if ignored["vat_total"] != float64(0) || ignored["total"] != float64(2360) || ignored["prices_include_vat"] != false {
			t.Fatalf("a shop without VAT still split VAT: %v", ignored)
		}

		switchOn(t, harness, company.OwnerToken, map[string]any{"vat_registered": true, "vat_number": "40-123456-A"})
		inclusiveBody := arrival("vat-00002", supplierId, 0, line(juiceId, 2, 1180))
		inclusiveBody["invoice_has_vat"] = true
		inclusiveBody["prices_include_vat"] = true
		inclusive := mustRecord(t, harness, company.OwnerToken, inclusiveBody)
		inclusiveLine := inclusive["lines"].([]any)[0].(map[string]any)
		if inclusive["subtotal"] != float64(2000) || inclusive["vat_total"] != float64(360) || inclusive["total"] != float64(2360) ||
			inclusiveLine["unit_cost"] != float64(1000) || costOf(harness, company.Id, juiceId) != 1000 {
			t.Fatalf("VAT-inclusive invoice returned %v", inclusive)
		}

		exclusiveBody := arrival("vat-00003", supplierId, 0, line(juiceId, 3, 1000))
		exclusiveBody["invoice_has_vat"] = true
		exclusive := mustRecord(t, harness, company.OwnerToken, exclusiveBody)
		if exclusive["subtotal"] != float64(3000) || exclusive["vat_total"] != float64(540) || exclusive["total"] != float64(3540) || costOf(harness, company.Id, juiceId) != 1000 {
			t.Fatalf("VAT-exclusive invoice returned %v", exclusive)
		}
		noVatBody := arrival("vat-00004", supplierId, 0, line(sodaId, 1, 1180))
		noVatBody["prices_include_vat"] = true
		noVat := mustRecord(t, harness, company.OwnerToken, noVatBody)
		if noVat["vat_total"] != float64(0) || noVat["total"] != float64(1180) || noVat["prices_include_vat"] != false {
			t.Fatalf("an invoice without VAT still split VAT: %v", noVat)
		}
		if balanceOf(t, harness, company.OwnerToken, supplierId) != 2360+2360+3540+1180 {
			t.Fatal("the supplier balance does not include VAT")
		}
	})
}

func TestCancellingStockArrivedReversesStockAndPayments(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Cancel Shop", "owner@cancel.test")
		switchOn(t, harness, company.OwnerToken, map[string]any{"suppliers_enabled": true})
		mainShopId := company.ShopId.String()
		supplierId := newSupplier(t, harness, company.OwnerToken, map[string]any{"name": "Cancel Supplier"})
		beansId := newProduct(t, harness, company.OwnerToken, "BEANS", 0, 0)

		recorded := mustRecord(t, harness, company.OwnerToken, arrival("cancel-0001", supplierId, 500, line(beansId, 10, 200)))
		purchaseId := recorded["id"].(string)
		if recorded["payment_status"] != "part_paid" || recorded["amount_due"] != float64(1500) || balanceOf(t, harness, company.OwnerToken, supplierId) != 1500 {
			t.Fatalf("part-paid arrival returned %v", recorded)
		}

		expectError(t, harness.Call(http.MethodPost, "/api/purchases/"+purchaseId+"/cancel", company.OwnerToken, map[string]any{"reason": "  "}), http.StatusBadRequest, "reason_required")
		cancelled := harness.Call(http.MethodPost, "/api/purchases/"+purchaseId+"/cancel", company.OwnerToken, map[string]any{"reason": "Wrong delivery"})
		cancelledData := cancelled.Data()
		cancelledPayment := cancelledData["payments"].([]any)[0].(map[string]any)
		if cancelled.Status != http.StatusOK || cancelledData["status"] != "cancelled" || cancelledData["cancel_reason"] != "Wrong delivery" ||
			cancelledData["cancelled_by_name"] == nil || cancelledPayment["is_voided"] != true {
			t.Fatalf("cancel returned %d %v", cancelled.Status, cancelled.Body)
		}
		reversals := harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM stock_movements WHERE reference = 'PUR-000001' AND reason = 'purchase' AND change = -10`)
		if stockIn(harness, company.Id, mainShopId, beansId) != 0 || reversals != 1 || balanceOf(t, harness, company.OwnerToken, supplierId) != 0 {
			t.Fatal("cancelling did not reverse the stock and the balance")
		}
		expectError(t, harness.Call(http.MethodPost, "/api/purchases/"+purchaseId+"/cancel", company.OwnerToken, map[string]any{"reason": "Again"}), http.StatusConflict, "already_cancelled")
		expectError(t, harness.Call(http.MethodPost, "/api/supplier-payments", company.OwnerToken, map[string]any{
			"supplier_id": supplierId, "purchase_id": purchaseId, "amount": 1, "method": "cash",
		}), http.StatusConflict, "already_cancelled")

		sold := mustRecord(t, harness, company.OwnerToken, arrival("cancel-0002", supplierId, 0, line(beansId, 5, 100)))
		damaged := harness.Call(http.MethodPost, "/api/stock-movements", company.OwnerToken, map[string]any{"product_id": beansId, "reason": "damage", "change": -2})
		if damaged.Status != http.StatusCreated {
			t.Fatalf("damaging stock returned %d %v", damaged.Status, damaged.Body)
		}
		expectError(t, harness.Call(http.MethodPost, "/api/purchases/"+sold["id"].(string)+"/cancel", company.OwnerToken, map[string]any{"reason": "Too late"}), http.StatusConflict, "stock_already_used")
		stillReceived := harness.Call(http.MethodGet, "/api/purchases/"+sold["id"].(string), company.OwnerToken, nil).Data()
		if stockIn(harness, company.Id, mainShopId, beansId) != 3 || stillReceived["status"] != "received" {
			t.Fatal("a refused cancel still changed the stock or the status")
		}

		cashArrival := mustRecord(t, harness, company.OwnerToken, arrival("cancel-0003", nil, 300, line(beansId, 3, 100)))
		cashPaymentId := cashArrival["payments"].([]any)[0].(map[string]any)["id"].(string)
		expectError(t, harness.Call(http.MethodPost, "/api/supplier-payments/"+cashPaymentId+"/void", company.OwnerToken, map[string]any{"reason": "Oops"}), http.StatusConflict, "payment_locked")
		cancelledCash := harness.Call(http.MethodPost, "/api/purchases/"+cashArrival["id"].(string)+"/cancel", company.OwnerToken, map[string]any{"reason": "Typed twice"})
		if cancelledCash.Status != http.StatusOK || cancelledCash.Data()["payments"].([]any)[0].(map[string]any)["is_voided"] != true {
			t.Fatalf("cancelling a cash arrival returned %d %v", cancelledCash.Status, cancelledCash.Body)
		}
	})
}

func TestSupplierPaymentsReturnsAndStatements(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Paying Shop", "owner@paying.test")
		switchOn(t, harness, company.OwnerToken, map[string]any{"suppliers_enabled": true})
		mainShopId := company.ShopId.String()
		supplierId := newSupplier(t, harness, company.OwnerToken, map[string]any{"name": "Owed Supplier", "opening_balance": 1000})
		otherSupplierId := newSupplier(t, harness, company.OwnerToken, map[string]any{"name": "Other Supplier"})
		oilId := newProduct(t, harness, company.OwnerToken, "OIL", 0, 0)

		mustRecord(t, harness, company.OwnerToken, arrival("paying-0001", supplierId, 0, line(oilId, 10, 300)))
		otherPurchase := mustRecord(t, harness, company.OwnerToken, arrival("paying-0002", otherSupplierId, 0, line(oilId, 1, 300)))
		if balanceOf(t, harness, company.OwnerToken, supplierId) != 4000 {
			t.Fatal("balance is not opening balance plus stock arrived")
		}

		payment := func(amount int) apptest.Response {
			return harness.Call(http.MethodPost, "/api/supplier-payments", company.OwnerToken, map[string]any{"supplier_id": supplierId, "amount": amount, "method": "cash", "reference": " Receipt 12 "})
		}
		expectError(t, payment(4001), http.StatusBadRequest, "overpayment")
		expectError(t, payment(0), http.StatusBadRequest, "validation_failed")
		expectError(t, harness.Call(http.MethodPost, "/api/supplier-payments", company.OwnerToken, map[string]any{
			"supplier_id": supplierId, "purchase_id": otherPurchase["id"], "amount": 100, "method": "cash",
		}), http.StatusNotFound, "purchase_not_found")
		expectError(t, harness.Call(http.MethodPost, "/api/supplier-payments", company.OwnerToken, map[string]any{
			"supplier_id": supplierId, "amount": 100, "method": "cheque",
		}), http.StatusBadRequest, "validation_failed")

		paid := payment(1500)
		paidData := paid.Data()
		if paid.Status != http.StatusCreated || paidData["payment_number"] != "PAY-000001" || paidData["reference"] != "Receipt 12" || balanceOf(t, harness, company.OwnerToken, supplierId) != 2500 {
			t.Fatalf("paying returned %d %v", paid.Status, paid.Body)
		}
		listed := harness.Call(http.MethodGet, "/api/supplier-payments?supplier_id="+supplierId, company.OwnerToken, nil)
		if listed.Data()["total"] != float64(1) {
			t.Fatalf("payments list returned %v", listed.Body)
		}

		paymentId := paidData["id"].(string)
		expectError(t, harness.Call(http.MethodPost, "/api/supplier-payments/"+paymentId+"/void", company.OwnerToken, map[string]any{"reason": " "}), http.StatusBadRequest, "reason_required")
		voided := harness.Call(http.MethodPost, "/api/supplier-payments/"+paymentId+"/void", company.OwnerToken, map[string]any{"reason": "Paid to the wrong supplier"})
		if voided.Status != http.StatusOK || voided.Data()["is_voided"] != true || voided.Data()["void_reason"] != "Paid to the wrong supplier" || balanceOf(t, harness, company.OwnerToken, supplierId) != 4000 {
			t.Fatalf("voiding returned %d %v", voided.Status, voided.Body)
		}
		expectError(t, harness.Call(http.MethodPost, "/api/supplier-payments/"+paymentId+"/void", company.OwnerToken, map[string]any{"reason": "Again"}), http.StatusConflict, "payment_already_voided")

		returned := harness.Call(http.MethodPost, "/api/supplier-returns", company.OwnerToken, map[string]any{
			"supplier_id": supplierId, "note": "Leaking bottles", "lines": []any{line(oilId, 4, 300)},
		})
		returnedData := returned.Data()
		if returned.Status != http.StatusCreated || returnedData["return_number"] != "RET-000001" || returnedData["total"] != float64(1200) || len(returnedData["lines"].([]any)) != 1 {
			t.Fatalf("returning stock returned %d %v", returned.Status, returned.Body)
		}
		returnMovements := harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM stock_movements WHERE reason = 'return' AND reference = 'RET-000001' AND change = -4`)
		if stockIn(harness, company.Id, mainShopId, oilId) != 7 || returnMovements != 1 || balanceOf(t, harness, company.OwnerToken, supplierId) != 2800 {
			t.Fatal("the return did not take stock out and lower the balance")
		}
		expectError(t, harness.Call(http.MethodPost, "/api/supplier-returns", company.OwnerToken, map[string]any{
			"supplier_id": supplierId, "lines": []any{line(oilId, 100, 300)},
		}), http.StatusConflict, "insufficient_stock")
		if harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM supplier_returns`) != 1 {
			t.Fatal("a refused return was saved")
		}

		if payment(2800).Status != http.StatusCreated || balanceOf(t, harness, company.OwnerToken, supplierId) != 0 {
			t.Fatal("paying the full balance did not clear it")
		}
		creditReturn := harness.Call(http.MethodPost, "/api/supplier-returns", company.OwnerToken, map[string]any{
			"supplier_id": supplierId, "lines": []any{line(oilId, 1, 300)},
		})
		if creditReturn.Status != http.StatusCreated || balanceOf(t, harness, company.OwnerToken, supplierId) != -300 {
			t.Fatalf("a return after paying in full should leave a credit of 300: %v", creditReturn.Body)
		}
		expectError(t, payment(1), http.StatusBadRequest, "overpayment")

		statement := harness.Call(http.MethodGet, "/api/suppliers/"+supplierId+"/statement", company.OwnerToken, nil)
		statementData := statement.Data()
		statementLines := statementData["lines"].([]any)
		wantKinds := []string{"opening_balance", "purchase", "return", "payment", "return"}
		if statement.Status != http.StatusOK || statementData["opening_balance"] != float64(0) || statementData["closing_balance"] != float64(-300) || len(statementLines) != len(wantKinds) {
			t.Fatalf("statement returned %d %v", statement.Status, statement.Body)
		}
		for lineIndex, wantKind := range wantKinds {
			if statementLines[lineIndex].(map[string]any)["kind"] != wantKind {
				t.Fatalf("statement line %d is %v, want %s", lineIndex, statementLines[lineIndex], wantKind)
			}
		}
		if statementLines[1].(map[string]any)["balance"] != float64(4000) {
			t.Fatalf("running balance after the purchase is %v", statementLines[1])
		}
		expectError(t, harness.Call(http.MethodGet, "/api/suppliers/"+supplierId+"/statement?from=2026-09-10&to=2026-09-01", company.OwnerToken, nil), http.StatusBadRequest, "invalid_range")
		expectError(t, harness.Call(http.MethodGet, "/api/suppliers/"+supplierId+"/statement?from=2024-01-01&to=2026-01-01", company.OwnerToken, nil), http.StatusBadRequest, "invalid_range")
	})
}

func TestAgingSortsWhatIsOwedByHowLate(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Aging Shop", "owner@aging.test")
		switchOn(t, harness, company.OwnerToken, map[string]any{"suppliers_enabled": true})
		supplierId := newSupplier(t, harness, company.OwnerToken, map[string]any{"name": "Slow Supplier", "payment_terms_days": 30})
		paidUpId := newSupplier(t, harness, company.OwnerToken, map[string]any{"name": "Paid Up Supplier"})
		teaId := newProduct(t, harness, company.OwnerToken, "TEA", 0, 0)

		asOf := time.Now().UTC().AddDate(0, 0, -10)
		asOf = time.Date(asOf.Year(), asOf.Month(), asOf.Day(), 9, 0, 0, 0, time.UTC)
		receivedDaysBefore := func(clientRef string, daysBefore int, amount int) {
			body := arrival(clientRef, supplierId, 0, line(teaId, 1, amount))
			body["received_at"] = asOf.AddDate(0, 0, -daysBefore).Format(time.RFC3339)
			mustRecord(t, harness, company.OwnerToken, body)
		}
		receivedDaysBefore("aging-0001", 171, 1000)
		receivedDaysBefore("aging-0002", 71, 2000)
		receivedDaysBefore("aging-0003", 15, 3000)
		receivedDaysBefore("aging-0004", -5, 9000)
		mustRecord(t, harness, company.OwnerToken, arrival("aging-0005", paidUpId, 400, line(teaId, 1, 400)))
		paid := harness.Call(http.MethodPost, "/api/supplier-payments", company.OwnerToken, map[string]any{
			"supplier_id": supplierId, "amount": 700, "method": "bank", "paid_at": asOf.AddDate(0, 0, -1).Format(time.RFC3339),
		})
		if paid.Status != http.StatusCreated {
			t.Fatalf("paying returned %d %v", paid.Status, paid.Body)
		}

		aging := harness.Call(http.MethodGet, "/api/suppliers/aging?as_of="+asOf.Format("2006-01-02"), company.OwnerToken, nil)
		agingData := aging.Data()
		agedSuppliers := agingData["suppliers"].([]any)
		if aging.Status != http.StatusOK || len(agedSuppliers) != 1 || agingData["total_balance"] != float64(5300) {
			t.Fatalf("aging returned %d %v", aging.Status, aging.Body)
		}
		buckets := agedSuppliers[0].(map[string]any)["aging"].(map[string]any)
		wantBuckets := map[string]float64{"current": 3000, "days_1_30": 0, "days_31_60": 2000, "days_61_90": 0, "days_over_90": 300}
		for bucketName, wantAmount := range wantBuckets {
			if buckets[bucketName] != wantAmount {
				t.Fatalf("bucket %s is %v, want %v (all %v)", bucketName, buckets[bucketName], wantAmount, buckets)
			}
		}

		today := harness.Call(http.MethodGet, "/api/suppliers/"+supplierId, company.OwnerToken, nil).Data()
		if today["balance"] != float64(14300) || today["overdue_amount"] != float64(2300) {
			t.Fatalf("today the supplier shows %v", today)
		}
		expectError(t, harness.Call(http.MethodGet, "/api/suppliers/aging?as_of=yesterday", company.OwnerToken, nil), http.StatusBadRequest, "invalid_date")
	})
}

func TestPurchaseOrdersTrackWhatArrived(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Order Shop", "owner@order.test")
		switchOn(t, harness, company.OwnerToken, map[string]any{"suppliers_enabled": true, "purchase_orders_enabled": true})
		supplierId := newSupplier(t, harness, company.OwnerToken, map[string]any{"name": "Order Supplier"})
		otherSupplierId := newSupplier(t, harness, company.OwnerToken, map[string]any{"name": "Another Supplier"})
		soapId := newProduct(t, harness, company.OwnerToken, "SOAP", 0, 0)
		saltId := newProduct(t, harness, company.OwnerToken, "SALT", 0, 0)

		created := harness.Call(http.MethodPost, "/api/purchase-orders", company.OwnerToken, map[string]any{
			"supplier_id": supplierId, "expected_date": "2026-10-05", "note": "Before month end",
			"lines": []any{
				map[string]any{"product_id": soapId, "quantity": 10, "expected_unit_cost": 100},
				map[string]any{"product_id": saltId, "quantity": 5, "expected_unit_cost": 200},
			},
		})
		createdData := created.Data()
		if created.Status != http.StatusCreated || createdData["order_number"] != "PO-000001" || createdData["status"] != "draft" || createdData["expected_total"] != float64(2000) {
			t.Fatalf("create order returned %d %v", created.Status, created.Body)
		}
		orderId := createdData["id"].(string)
		orderStatus := func() (string, []any) {
			foundOrder := harness.Call(http.MethodGet, "/api/purchase-orders/"+orderId, company.OwnerToken, nil).Data()
			return foundOrder["status"].(string), foundOrder["lines"].([]any)
		}

		if harness.Call(http.MethodPost, "/api/purchase-orders/"+orderId+"/send", company.OwnerToken, nil).Data()["status"] != "sent" {
			t.Fatal("marking the order as sent failed")
		}
		expectError(t, harness.Call(http.MethodPost, "/api/purchase-orders/"+orderId+"/send", company.OwnerToken, nil), http.StatusConflict, "order_not_draft")

		partBody := arrival("order-00001", nil, 0, line(soapId, 4, 100))
		partBody["purchase_order_id"] = orderId
		part := mustRecord(t, harness, company.OwnerToken, partBody)
		status, orderLines := orderStatus()
		if part["supplier_id"] != supplierId || part["purchase_order_number"] != "PO-000001" || status != "partly_received" ||
			orderLines[0].(map[string]any)["quantity_remaining"] != float64(6) {
			t.Fatalf("after a part delivery the order is %s with %v", status, orderLines)
		}

		mismatchBody := arrival("order-00002", otherSupplierId, 0, line(soapId, 1, 100))
		mismatchBody["purchase_order_id"] = orderId
		expectError(t, harness.Call(http.MethodPost, "/api/purchases", company.OwnerToken, mismatchBody), http.StatusBadRequest, "order_supplier_mismatch")

		restBody := arrival("order-00003", supplierId, 0, line(soapId, 6, 100), line(saltId, 5, 200))
		restBody["purchase_order_id"] = orderId
		rest := mustRecord(t, harness, company.OwnerToken, restBody)
		if status, _ = orderStatus(); status != "received" {
			t.Fatalf("after the full delivery the order is %s", status)
		}
		moreBody := arrival("order-00004", supplierId, 0, line(soapId, 1, 100))
		moreBody["purchase_order_id"] = orderId
		expectError(t, harness.Call(http.MethodPost, "/api/purchases", company.OwnerToken, moreBody), http.StatusConflict, "order_closed")

		linked := harness.Call(http.MethodGet, "/api/purchases?purchase_order_id="+orderId, company.OwnerToken, nil)
		if linked.Data()["total"] != float64(2) {
			t.Fatalf("stock arrived for the order: %v", linked.Body)
		}

		if harness.Call(http.MethodPost, "/api/purchases/"+rest["id"].(string)+"/cancel", company.OwnerToken, map[string]any{"reason": "Returned the lorry"}).Status != http.StatusOK {
			t.Fatal("cancelling the second delivery failed")
		}
		status, orderLines = orderStatus()
		if status != "partly_received" || orderLines[0].(map[string]any)["quantity_received"] != float64(4) || orderLines[1].(map[string]any)["quantity_received"] != float64(0) {
			t.Fatalf("after cancelling a delivery the order is %s with %v", status, orderLines)
		}

		cancelled := harness.Call(http.MethodPost, "/api/purchase-orders/"+orderId+"/cancel", company.OwnerToken, map[string]any{"reason": "Found a cheaper supplier"})
		if cancelled.Status != http.StatusOK || cancelled.Data()["status"] != "cancelled" {
			t.Fatalf("cancelling the order returned %d %v", cancelled.Status, cancelled.Body)
		}
		expectError(t, harness.Call(http.MethodPost, "/api/purchase-orders/"+orderId+"/cancel", company.OwnerToken, map[string]any{"reason": "Again"}), http.StatusConflict, "order_closed")
		if harness.Call(http.MethodGet, "/api/purchase-orders?status=cancelled", company.OwnerToken, nil).Data()["total"] != float64(1) {
			t.Fatal("filtering orders by status failed")
		}
		expectError(t, harness.Call(http.MethodGet, "/api/purchase-orders?status=lost", company.OwnerToken, nil), http.StatusBadRequest, "invalid_filter")

		switchOn(t, harness, company.OwnerToken, map[string]any{"purchase_orders_enabled": false})
		expectError(t, harness.Call(http.MethodGet, "/api/purchase-orders/"+orderId, company.OwnerToken, nil), http.StatusForbidden, "feature_off")
		if harness.Call(http.MethodGet, "/api/purchases/"+part["id"].(string), company.OwnerToken, nil).Status != http.StatusOK {
			t.Fatal("turning orders off hid stock arrived")
		}
	})
}

func TestOtherCompaniesNeverSeeSuppliers(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Private Shop", "owner@private.test")
		otherCompany := harness.CreateCompany("Nosy Shop", "owner@nosy.test")
		for _, ownerToken := range []string{company.OwnerToken, otherCompany.OwnerToken} {
			switchOn(t, harness, ownerToken, map[string]any{"suppliers_enabled": true, "purchase_orders_enabled": true})
		}
		supplierId := newSupplier(t, harness, company.OwnerToken, map[string]any{"name": "Private Supplier", "opening_balance": 5000})
		riceId := newProduct(t, harness, company.OwnerToken, "RICE", 5, 100)
		nosyRiceId := newProduct(t, harness, otherCompany.OwnerToken, "RICE", 5, 100)
		purchase := mustRecord(t, harness, company.OwnerToken, arrival("private-0001", supplierId, 100, line(riceId, 2, 100)))
		paymentId := purchase["payments"].([]any)[0].(map[string]any)["id"].(string)
		returned := harness.Call(http.MethodPost, "/api/supplier-returns", company.OwnerToken, map[string]any{"supplier_id": supplierId, "lines": []any{line(riceId, 1, 100)}}).Data()
		order := harness.Call(http.MethodPost, "/api/purchase-orders", company.OwnerToken, map[string]any{
			"supplier_id": supplierId, "lines": []any{map[string]any{"product_id": riceId, "quantity": 1}},
		}).Data()

		crossRequests := []struct {
			method   string
			path     string
			body     map[string]any
			wantCode string
		}{
			{http.MethodGet, "/api/suppliers/" + supplierId, nil, "supplier_not_found"},
			{http.MethodGet, "/api/suppliers/" + supplierId + "/statement", nil, "supplier_not_found"},
			{http.MethodDelete, "/api/suppliers/" + supplierId, nil, "supplier_not_found"},
			{http.MethodGet, "/api/purchases/" + purchase["id"].(string), nil, "purchase_not_found"},
			{http.MethodPost, "/api/purchases/" + purchase["id"].(string) + "/cancel", map[string]any{"reason": "x"}, "purchase_not_found"},
			{http.MethodPost, "/api/purchases", arrival("nosy-000001", supplierId, 0, line(nosyRiceId, 1, 100)), "supplier_not_found"},
			{http.MethodPost, "/api/supplier-payments", map[string]any{"supplier_id": supplierId, "amount": 1, "method": "cash"}, "supplier_not_found"},
			{http.MethodPost, "/api/supplier-payments/" + paymentId + "/void", map[string]any{"reason": "x"}, "payment_not_found"},
			{http.MethodGet, "/api/supplier-returns/" + returned["id"].(string), nil, "return_not_found"},
			{http.MethodGet, "/api/purchase-orders/" + order["id"].(string), nil, "order_not_found"},
		}
		for _, crossRequest := range crossRequests {
			expectError(t, harness.Call(crossRequest.method, crossRequest.path, otherCompany.OwnerToken, crossRequest.body), http.StatusNotFound, crossRequest.wantCode)
		}
		for _, listPath := range []string{"/api/suppliers?include_inactive=true", "/api/purchases", "/api/supplier-payments", "/api/supplier-returns", "/api/purchase-orders"} {
			listed := harness.Call(http.MethodGet, listPath, otherCompany.OwnerToken, nil)
			if listed.Status != http.StatusOK || listed.Data()["total"] != float64(0) {
				t.Fatalf("%s showed another company's rows: %v", listPath, listed.Body)
			}
		}
		aging := harness.Call(http.MethodGet, "/api/suppliers/aging", otherCompany.OwnerToken, nil).Data()
		if len(aging["suppliers"].([]any)) != 0 {
			t.Fatalf("aging leaked another company's suppliers: %v", aging)
		}
		if balanceOf(t, harness, company.OwnerToken, supplierId) != 5000 {
			t.Fatal("the other company changed this supplier")
		}

		if engineCase.Engine == config.EnginePostgres {
			for _, tableName := range []string{"suppliers", "supplier_document_counters", "purchase_orders", "purchase_order_lines", "purchases", "purchase_lines", "supplier_payments", "supplier_returns", "supplier_return_lines"} {
				if rowsWithoutTenant(t, harness, tableName) != 0 {
					t.Fatalf("%s is visible with no tenant set", tableName)
				}
			}
		}
	})
}

func rowsWithoutTenant(t *testing.T, harness *apptest.Harness, tableName string) int64 {
	t.Helper()
	readTransaction, beginError := harness.Database.Reader.BeginTx(context.Background(), nil)
	if beginError != nil {
		t.Fatalf("begin: %v", beginError)
	}
	defer readTransaction.Rollback()

	rowCount := int64(0)
	scanError := readTransaction.QueryRowContext(context.Background(), "SELECT COUNT(*) FROM "+tableName).Scan(&rowCount)
	if scanError != nil {
		t.Fatalf("count %s: %v", tableName, scanError)
	}
	return rowCount
}
