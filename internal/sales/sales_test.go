package sales_test

import (
	"fmt"
	"net/http"
	"sort"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/google/uuid"
)

func newProduct(t *testing.T, harness *apptest.Harness, sessionToken string, body map[string]any) string {
	t.Helper()
	createResponse := harness.Call(http.MethodPost, "/api/products", sessionToken, body)
	if createResponse.Status != http.StatusCreated {
		t.Fatalf("create %v returned %d: %v", body["sku"], createResponse.Status, createResponse.Body)
	}
	return createResponse.Data()["id"].(string)
}

func line(productId string, quantity int, addonIds ...string) map[string]any {
	return map[string]any{"product_id": productId, "quantity": quantity, "addon_ids": addonIds}
}

func cash(amount int64) []map[string]any {
	return []map[string]any{{"method": "cash", "amount": amount}}
}

func sell(harness *apptest.Harness, sessionToken string, clientRef string, items []map[string]any, payments []map[string]any) apptest.Response {
	return harness.Call(http.MethodPost, "/api/sales", sessionToken, map[string]any{"client_ref": clientRef, "items": items, "payments": payments})
}

func TestSalesAreIdempotentPricedByTheServerAndAtomic(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Till Shop", "owner@till.test")
		otherCompany := harness.CreateCompany("Other Till", "owner@othertill.test")
		now := time.Now().UTC()

		sodaId := newProduct(t, harness, company.OwnerToken, map[string]any{"sku": "SODA", "name": "Soda", "price": 1000, "cost_price": 600, "wholesale_price": 850, "wholesale_min": 10, "opening_quantity": 40})
		coffeeId := newProduct(t, harness, company.OwnerToken, map[string]any{"sku": "COFFEE", "name": "Coffee", "price": 3000, "cost_price": 900, "opening_quantity": 5})
		breadId := newProduct(t, harness, company.OwnerToken, map[string]any{"sku": "BREAD", "name": "Bread", "price": 2500, "opening_quantity": 1})
		foreignId := newProduct(t, harness, otherCompany.OwnerToken, map[string]any{"sku": "SODA", "name": "Soda", "price": 1, "opening_quantity": 9})
		shotId := harness.Call(http.MethodPost, "/api/products/"+coffeeId+"/addons", company.OwnerToken, map[string]any{"name": "Extra shot", "price": 500}).Data()["id"].(string)
		sodaAddonId := harness.Call(http.MethodPost, "/api/products/"+sodaId+"/addons", company.OwnerToken, map[string]any{"name": "Ice", "price": 100}).Data()["id"].(string)

		createDiscount := func(name string, value int64, startsAt time.Time, endsAt time.Time, productId any) string {
			return harness.Call(http.MethodPost, "/api/discounts", company.OwnerToken, map[string]any{
				"name": name, "kind": "percent", "value": value, "product_id": productId,
				"starts_at": startsAt.Format(time.RFC3339), "ends_at": endsAt.Format(time.RFC3339),
			}).Data()["id"].(string)
		}
		createDiscount("Coffee hour", 1000, now.Add(-time.Hour), now.Add(time.Hour), coffeeId)
		createDiscount("Yesterday", 5000, now.Add(-48*time.Hour), now.Add(-24*time.Hour), nil)
		stoppedId := createDiscount("Stopped", 9000, now.Add(-time.Hour), now.Add(time.Hour), nil)
		harness.Call(http.MethodDelete, "/api/discounts/"+stoppedId, company.OwnerToken, nil)

		quote := harness.Call(http.MethodPost, "/api/sales/quote", company.OwnerToken, map[string]any{
			"items": []any{line(sodaId, 10), line(coffeeId, 2, shotId)},
		})
		quoteData := quote.Data()
		quoteLines := quoteData["lines"].([]any)
		sodaQuote := quoteLines[0].(map[string]any)
		coffeeQuote := quoteLines[1].(map[string]any)
		if quote.Status != http.StatusOK || sodaQuote["is_wholesale"] != true || sodaQuote["line_total"] != float64(8500) || sodaQuote["in_stock"] != float64(40) ||
			coffeeQuote["discount_name"] != "Coffee hour" || coffeeQuote["line_total"] != float64(6400) || quoteData["total"] != float64(14900) || quoteData["tax_total"] != float64(2273) {
			t.Fatalf("quote returned %d %v", quote.Status, quoteData)
		}

		tamperedBody := map[string]any{
			"client_ref": "checkout-0001",
			"items":      []any{map[string]any{"product_id": sodaId, "quantity": 9, "unit_price": 1, "line_total": 9}},
			"payments":   cash(10000),
			"total":      9,
		}
		firstSale := harness.Call(http.MethodPost, "/api/sales", company.OwnerToken, tamperedBody)
		firstData := firstSale.Data()
		if firstSale.Status != http.StatusCreated || firstData["total"] != float64(9000) || firstData["change_given"] != float64(1000) || firstData["amount_paid"] != float64(10000) {
			t.Fatalf("first sale returned %d %v", firstSale.Status, firstSale.Body)
		}
		if !strings.HasPrefix(firstData["receipt_number"].(string), "SALE-") || !strings.HasSuffix(firstData["receipt_number"].(string), "-0001") {
			t.Fatalf("receipt number %v", firstData["receipt_number"])
		}

		replay := harness.Call(http.MethodPost, "/api/sales", company.OwnerToken, tamperedBody)
		if replay.Status != http.StatusCreated || replay.Data()["id"] != firstData["id"] || replay.Data()["receipt_number"] != firstData["receipt_number"] {
			t.Fatalf("replay returned %d %v", replay.Status, replay.Body)
		}
		if harness.QueryIntForCompany(company.Id, `SELECT quantity FROM shop_stock WHERE product_id = $1`, sodaId) != 31 {
			t.Fatal("the replay sold the stock a second time")
		}
		changedReplay := sell(harness, company.OwnerToken, "checkout-0001", []map[string]any{line(sodaId, 8)}, cash(10000))
		if changedReplay.Status != http.StatusConflict || changedReplay.Code() != "client_ref_reused" {
			t.Fatalf("reusing a reference for a different cart returned %d %s", changedReplay.Status, changedReplay.Code())
		}

		salesBefore := harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM sales`)
		movementsBefore := harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM stock_movements`)
		refused := []struct {
			name       string
			items      []map[string]any
			payments   []map[string]any
			wantStatus int
			wantCode   string
		}{
			{"short on the second line", []map[string]any{line(sodaId, 1), line(breadId, 2)}, cash(20000), http.StatusConflict, "insufficient_stock"},
			{"a foreign product", []map[string]any{line(sodaId, 1), line(foreignId, 1)}, cash(20000), http.StatusNotFound, "not_found"},
			{"another product's add-on", []map[string]any{line(coffeeId, 1, sodaAddonId)}, cash(20000), http.StatusNotFound, "not_found"},
			{"cash below the total", []map[string]any{line(sodaId, 1)}, cash(999), http.StatusBadRequest, "invalid_payment"},
			{"no payment", []map[string]any{line(sodaId, 1)}, nil, http.StatusBadRequest, "invalid_payment"},
			{"card above the total", []map[string]any{line(sodaId, 1)}, []map[string]any{{"method": "card", "amount": 1500}}, http.StatusBadRequest, "invalid_payment"},
			{"cash twice", []map[string]any{line(sodaId, 1)}, []map[string]any{{"method": "cash", "amount": 500}, {"method": "cash", "amount": 500}}, http.StatusBadRequest, "invalid_payment"},
			{"zero quantity", []map[string]any{line(sodaId, 0)}, cash(1000), http.StatusBadRequest, "validation_failed"},
		}
		for refusedIndex, refusedSale := range refused {
			refusedResponse := sell(harness, company.OwnerToken, fmt.Sprintf("refused-%04d", refusedIndex), refusedSale.items, refusedSale.payments)
			if refusedResponse.Status != refusedSale.wantStatus || refusedResponse.Code() != refusedSale.wantCode {
				t.Fatalf("%s returned %d %s, want %d %s", refusedSale.name, refusedResponse.Status, refusedResponse.Code(), refusedSale.wantStatus, refusedSale.wantCode)
			}
		}
		if harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM sales`) != salesBefore ||
			harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM stock_movements`) != movementsBefore {
			t.Fatal("a refused sale still wrote something")
		}

		split := sell(harness, company.OwnerToken, "checkout-0002", []map[string]any{line(coffeeId, 1, shotId), line(breadId, 1)},
			[]map[string]any{{"method": "mobile", "amount": 3000}, {"method": "cash", "amount": 5000}})
		splitData := split.Data()
		if split.Status != http.StatusCreated || splitData["total"] != float64(5700) || splitData["change_given"] != float64(2300) || !strings.HasSuffix(splitData["receipt_number"].(string), "-0002") {
			t.Fatalf("split payment sale returned %d %v", split.Status, split.Body)
		}
		splitItems := splitData["items"].([]any)
		coffeeLine := splitItems[0].(map[string]any)
		if len(coffeeLine["addons"].([]any)) != 1 || coffeeLine["discount_name"] != "Coffee hour" || len(splitData["payments"].([]any)) != 2 {
			t.Fatalf("split sale detail %v", splitData)
		}
		soldOut := harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM notifications WHERE product_id = $1 AND kind = 'out_of_stock'`, breadId)
		if soldOut != 1 {
			t.Fatalf("selling the last bread raised %d out-of-stock notices", soldOut)
		}

		cashierToken := harness.CreateStaff(company, "cashier@till.test", []string{"sales:create", "products:view"}, []uuid.UUID{company.ShopId})
		cashierSale := sell(harness, cashierToken, "checkout-cashier-1", []map[string]any{line(sodaId, 1)}, cash(1000))
		cashierReceipt := harness.Call(http.MethodGet, "/api/sales/"+cashierSale.Data()["id"].(string)+"/receipt", cashierToken, nil)
		cashierList := harness.Call(http.MethodGet, "/api/sales", cashierToken, nil)
		if cashierSale.Status != http.StatusCreated || cashierReceipt.Status != http.StatusOK || cashierList.Status != http.StatusForbidden {
			t.Fatalf("cashier sale %d, receipt %d, list %d", cashierSale.Status, cashierReceipt.Status, cashierList.Status)
		}
		receiptData := cashierReceipt.Data()
		if receiptData["company"].(map[string]any)["name"] != "Till Shop" || receiptData["shop"].(map[string]any)["name"] != "Main Shop" || receiptData["show_tax"] != true {
			t.Fatalf("receipt %v", receiptData)
		}

		listed := harness.Call(http.MethodGet, "/api/sales?limit=2", company.OwnerToken, nil)
		totals := harness.Call(http.MethodGet, "/api/sales/totals", company.OwnerToken, nil).Data()
		if listed.Data()["total"] != float64(3) || len(listed.Items()) != 2 || totals["total"] != float64(9000+5700+1000) || totals["sale_count"] != float64(3) {
			t.Fatalf("list %v, totals %v", listed.Data(), totals)
		}
		searched := harness.Call(http.MethodGet, "/api/sales?q=0002", company.OwnerToken, nil)
		if searched.Data()["total"] != float64(1) {
			t.Fatalf("receipt search found %v", searched.Data()["total"])
		}
		saleId := firstData["id"].(string)
		if harness.Call(http.MethodGet, "/api/sales/"+saleId, otherCompany.OwnerToken, nil).Status != http.StatusNotFound ||
			harness.Call(http.MethodGet, "/api/sales/"+saleId+"/receipt", otherCompany.OwnerToken, nil).Status != http.StatusNotFound {
			t.Fatal("another company can read this sale")
		}
		if harness.Call(http.MethodGet, "/api/sales", otherCompany.OwnerToken, nil).Data()["total"] != float64(0) {
			t.Fatal("another company's sale list is not empty")
		}

		mismatches := harness.QueryIntForCompany(company.Id, `
			SELECT COUNT(*) FROM shop_stock ss
			WHERE ss.quantity <> (SELECT COALESCE(SUM(m.change), 0) FROM stock_movements m
			                      WHERE m.company_id = ss.company_id AND m.shop_id = ss.shop_id AND m.product_id = ss.product_id)
		`)
		if mismatches != 0 {
			t.Fatalf("%d stock rows disagree with their movements after sales", mismatches)
		}
	})
}

func TestConcurrentSalesGetUniqueGapFreeReceiptNumbers(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Busy Till", "owner@busytill.test")
		waterId := newProduct(t, harness, company.OwnerToken, map[string]any{"sku": "WATER", "name": "Water", "price": 500, "opening_quantity": 25})

		saleCount := 20
		receiptNumbers := make(chan string, saleCount)
		failures := make(chan string, saleCount)
		var waitGroup sync.WaitGroup
		for saleIndex := 0; saleIndex < saleCount; saleIndex++ {
			waitGroup.Add(1)
			go func(saleIndex int) {
				defer waitGroup.Done()
				saleResponse := sell(harness, company.OwnerToken, fmt.Sprintf("parallel-%04d", saleIndex), []map[string]any{line(waterId, 1)}, cash(500))
				if saleResponse.Status != http.StatusCreated {
					failures <- fmt.Sprintf("%d %v", saleResponse.Status, saleResponse.Body)
					return
				}
				receiptNumbers <- saleResponse.Data()["receipt_number"].(string)
			}(saleIndex)
		}
		waitGroup.Wait()
		close(receiptNumbers)
		close(failures)

		for failure := range failures {
			t.Fatalf("a parallel sale failed: %s", failure)
		}
		counters := []string{}
		for receiptNumber := range receiptNumbers {
			counters = append(counters, receiptNumber[len(receiptNumber)-4:])
		}
		sort.Strings(counters)
		for counterIndex, counter := range counters {
			if counter != fmt.Sprintf("%04d", counterIndex+1) {
				t.Fatalf("receipt counters are not 0001..0020 without gaps: %v", counters)
			}
		}
		if harness.QueryIntForCompany(company.Id, `SELECT quantity FROM shop_stock WHERE product_id = $1`, waterId) != 5 {
			t.Fatal("parallel sales left the wrong stock")
		}
	})
}

func TestCurrencyLocksAfterTheFirstSale(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Currency Shop", "owner@currency.test")
		teaId := newProduct(t, harness, company.OwnerToken, map[string]any{"sku": "TEA", "name": "Tea", "price": 500, "opening_quantity": 3})

		beforeSale := harness.Call(http.MethodPut, "/api/settings", company.OwnerToken, map[string]any{"currency_code": "KES", "currency_decimals": 2})
		if beforeSale.Status != http.StatusOK {
			t.Fatalf("changing currency before any sale returned %d", beforeSale.Status)
		}
		badFormat := harness.Call(http.MethodPut, "/api/settings", company.OwnerToken, map[string]any{"receipt_number_format": "{SHOP}-{DATE}"})
		if badFormat.Status != http.StatusBadRequest {
			t.Fatalf("a receipt format without {COUNTER} returned %d", badFormat.Status)
		}

		firstSale := sell(harness, company.OwnerToken, "currency-sale-1", []map[string]any{line(teaId, 1)}, cash(500))
		if firstSale.Status != http.StatusCreated || firstSale.Data()["currency_code"] != "KES" || firstSale.Data()["currency_decimals"] != float64(2) {
			t.Fatalf("first sale returned %d %v", firstSale.Status, firstSale.Body)
		}

		afterSale := harness.Call(http.MethodPut, "/api/settings", company.OwnerToken, map[string]any{"currency_code": "TZS", "currency_decimals": 0})
		decimalsOnly := harness.Call(http.MethodPut, "/api/settings", company.OwnerToken, map[string]any{"currency_decimals": 0})
		sameCurrency := harness.Call(http.MethodPut, "/api/settings", company.OwnerToken, map[string]any{"currency_code": "KES", "tax_rate": 16})
		if afterSale.Status != http.StatusConflict || afterSale.Code() != "currency_locked" || decimalsOnly.Status != http.StatusConflict || sameCurrency.Status != http.StatusOK {
			t.Fatalf("after the first sale: switch %d, decimals %d, same currency %d", afterSale.Status, decimalsOnly.Status, sameCurrency.Status)
		}
	})
}

func TestTillOptionsFollowSettingsAndReachCashiers(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Options Shop", "owner@options.test")
		cashierToken := harness.CreateStaff(company, "cashier@options.test", []string{"sales:create"}, []uuid.UUID{company.ShopId})
		viewerToken := harness.CreateStaff(company, "viewer@options.test", []string{"sales:view"}, []uuid.UUID{company.ShopId})

		defaults := harness.Call(http.MethodGet, "/api/sales/till", cashierToken, nil)
		if defaults.Status != http.StatusOK || defaults.Data()["numpad_enabled"] != false || defaults.Data()["customer_display_enabled"] != false || defaults.Data()["efd_enabled"] != false {
			t.Fatalf("default till options returned %d %v", defaults.Status, defaults.Body)
		}

		turnOn := harness.Call(http.MethodPut, "/api/settings", company.OwnerToken, map[string]any{"till_numpad_enabled": true, "customer_display_enabled": true})
		if turnOn.Status != http.StatusOK || turnOn.Data()["till_numpad_enabled"] != true || turnOn.Data()["customer_display_enabled"] != true {
			t.Fatalf("turning the till options on returned %d %v", turnOn.Status, turnOn.Body)
		}

		cashierView := harness.Call(http.MethodGet, "/api/sales/till", cashierToken, nil)
		if cashierView.Data()["numpad_enabled"] != true || cashierView.Data()["customer_display_enabled"] != true {
			t.Fatalf("the cashier saw %v", cashierView.Body)
		}
		if harness.Call(http.MethodGet, "/api/sales/till", viewerToken, nil).Status != http.StatusForbidden {
			t.Fatal("someone who cannot sell read the till options")
		}

		turnOff := harness.Call(http.MethodPut, "/api/settings", company.OwnerToken, map[string]any{"customer_display_enabled": false})
		if turnOff.Data()["customer_display_enabled"] != false || turnOff.Data()["till_numpad_enabled"] != true {
			t.Fatalf("turning one option off changed the other: %v", turnOff.Body)
		}
	})
}
