package reports_test

import (
	"fmt"
	"net/http"
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

func sell(t *testing.T, harness *apptest.Harness, sessionToken string, clientRef string, productId string, quantity int, payments []map[string]any) string {
	t.Helper()
	saleResponse := harness.Call(http.MethodPost, "/api/sales", sessionToken, map[string]any{
		"client_ref": clientRef,
		"items":      []map[string]any{{"product_id": productId, "quantity": quantity}},
		"payments":   payments,
	})
	if saleResponse.Status != http.StatusCreated {
		t.Fatalf("sale %s returned %d: %v", clientRef, saleResponse.Status, saleResponse.Body)
	}
	return saleResponse.Data()["id"].(string)
}

func cash(amount int64) []map[string]any {
	return []map[string]any{{"method": "cash", "amount": amount}}
}

func number(t *testing.T, values map[string]any, key string) int64 {
	t.Helper()
	value, isNumber := values[key].(float64)
	if !isNumber {
		t.Fatalf("%s is %v, want a number", key, values[key])
	}
	return int64(value)
}

func today() string {
	darEsSalaam, _ := time.LoadLocation("Africa/Dar_es_Salaam")
	return time.Now().In(darEsSalaam).Format("2006-01-02")
}

func TestReportsGiveExactTotalsFromSnapshots(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Report Shop", "owner@reports.test")
		sodaId := newProduct(t, harness, company.OwnerToken, map[string]any{"sku": "SODA", "name": "Soda", "price": 1180, "cost_price": 600, "opening_quantity": 100})
		coffeeId := newProduct(t, harness, company.OwnerToken, map[string]any{"sku": "COFFEE", "name": "Coffee", "price": 2360, "cost_price": 900, "opening_quantity": 100})
		idleId := newProduct(t, harness, company.OwnerToken, map[string]any{"sku": "IDLE", "name": "Idle", "price": 500, "cost_price": 200, "opening_quantity": 7})

		sell(t, harness, company.OwnerToken, "report-sale-1", sodaId, 2, cash(5000))
		sell(t, harness, company.OwnerToken, "report-sale-2", coffeeId, 1, []map[string]any{{"method": "card", "amount": 2360}})
		sell(t, harness, company.OwnerToken, "report-sale-3", sodaId, 1, []map[string]any{{"method": "mobile", "amount": 1180}})

		rangeQuery := "?from=" + today() + "&to=" + today()
		summary := harness.Call(http.MethodGet, "/api/reports/summary"+rangeQuery, company.OwnerToken, nil)
		if summary.Status != http.StatusOK {
			t.Fatalf("summary returned %d %v", summary.Status, summary.Body)
		}
		summaryData := summary.Data()
		expected := map[string]int64{
			"sale_count": 3, "units_sold": 4, "total": 5900, "tax_total": 900, "net_sales": 5000,
			"cost_total": 3*600 + 900, "gross_profit": 5000 - 2700, "margin_basis_points": 4600, "average_sale": 1967,
		}
		for key, want := range expected {
			if got := number(t, summaryData, key); got != want {
				t.Fatalf("%s = %d, want %d (%v)", key, got, want, summaryData)
			}
		}
		payments := summaryData["payments"].(map[string]any)
		if number(t, payments, "cash") != 2360 || number(t, payments, "card") != 2360 || number(t, payments, "mobile") != 1180 {
			t.Fatalf("payments were %v; cash must be net of change", payments)
		}

		costChange := harness.Call(http.MethodPut, "/api/products/"+sodaId, company.OwnerToken, map[string]any{"sku": "SODA", "name": "Soda", "price": 1180, "cost_price": 5000})
		if costChange.Status != http.StatusOK || number(t, costChange.Data(), "cost_price") != 5000 {
			t.Fatalf("changing the cost returned %d %v", costChange.Status, costChange.Body)
		}
		afterCostChange := harness.Call(http.MethodGet, "/api/reports/summary"+rangeQuery, company.OwnerToken, nil).Data()
		if number(t, afterCostChange, "gross_profit") != 2300 {
			t.Fatalf("changing a product's cost changed past profit to %v", afterCostChange["gross_profit"])
		}

		productRows := harness.Call(http.MethodGet, "/api/reports/products"+rangeQuery+"&sort=quantity", company.OwnerToken, nil).Body["data"].([]any)
		firstProduct := productRows[0].(map[string]any)
		if len(productRows) != 2 || firstProduct["sku"] != "SODA" || number(t, firstProduct, "quantity") != 3 || number(t, firstProduct, "revenue") != 3540 || number(t, firstProduct, "cost_total") != 1800 || number(t, firstProduct, "sale_count") != 2 {
			t.Fatalf("products by quantity were %v", productRows)
		}
		if harness.Call(http.MethodGet, "/api/reports/products?sort=loudest", company.OwnerToken, nil).Status != http.StatusBadRequest {
			t.Fatal("an unknown sort was accepted")
		}

		cashiers := harness.Call(http.MethodGet, "/api/reports/cashiers"+rangeQuery, company.OwnerToken, nil).Body["data"].([]any)
		if len(cashiers) != 1 || number(t, cashiers[0].(map[string]any), "total") != 5900 || number(t, cashiers[0].(map[string]any), "sale_count") != 3 {
			t.Fatalf("cashiers were %v", cashiers)
		}

		daily := harness.Call(http.MethodGet, "/api/reports/daily"+rangeQuery, company.OwnerToken, nil).Body["data"].([]any)
		if len(daily) != 1 || daily[0].(map[string]any)["date"] != today() || number(t, daily[0].(map[string]any), "gross_profit") != 2300 {
			t.Fatalf("daily was %v", daily)
		}

		inventory := harness.Call(http.MethodGet, "/api/reports/inventory", company.OwnerToken, nil).Data()
		deadStock := inventory["dead_stock"].([]any)
		stockTotals := inventory["stock"].(map[string]any)
		if len(deadStock) != 1 || deadStock[0].(map[string]any)["product_id"] != idleId || deadStock[0].(map[string]any)["last_sold_at"] != nil || number(t, stockTotals, "units") != 97+99+7 {
			t.Fatalf("inventory was %v", inventory)
		}

		dashboard := harness.Call(http.MethodGet, "/api/dashboard", company.OwnerToken, nil)
		dashboardData := dashboard.Data()
		if dashboard.Status != http.StatusOK || number(t, dashboardData["today"].(map[string]any), "total") != 5900 || len(dashboardData["last_two_weeks"].([]any)) != 14 || len(dashboardData["recent_sales"].([]any)) != 3 || len(dashboardData["top_products"].([]any)) != 2 {
			t.Fatalf("dashboard returned %d %v", dashboard.Status, dashboardData)
		}
	})
}

func TestEmptyRangesAndLocalDays(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Night Shop", "owner@night.test")
		teaId := newProduct(t, harness, company.OwnerToken, map[string]any{"sku": "TEA", "name": "Tea", "price": 1180, "cost_price": 500, "opening_quantity": 10})

		empty := harness.Call(http.MethodGet, "/api/reports/summary?from=2020-01-01&to=2020-01-31", company.OwnerToken, nil)
		for _, key := range []string{"sale_count", "total", "tax_total", "gross_profit", "margin_basis_points", "average_sale", "units_sold"} {
			if number(t, empty.Data(), key) != 0 {
				t.Fatalf("an empty range gave %s = %v", key, empty.Data()[key])
			}
		}
		emptyDays := harness.Call(http.MethodGet, "/api/reports/daily?from=2020-01-01&to=2020-01-31", company.OwnerToken, nil).Body["data"].([]any)
		emptyLists := [][]any{
			harness.Call(http.MethodGet, "/api/reports/products?from=2020-01-01&to=2020-01-31", company.OwnerToken, nil).Body["data"].([]any),
			harness.Call(http.MethodGet, "/api/reports/cashiers?from=2020-01-01&to=2020-01-31", company.OwnerToken, nil).Body["data"].([]any),
			harness.Call(http.MethodGet, "/api/reports/shops?from=2020-01-01&to=2020-01-31", company.OwnerToken, nil).Body["data"].([]any),
		}
		if len(emptyDays) != 31 || number(t, emptyDays[30].(map[string]any), "total") != 0 {
			t.Fatalf("an empty month gave %d days", len(emptyDays))
		}
		for _, emptyList := range emptyLists {
			if emptyList == nil || len(emptyList) != 0 {
				t.Fatalf("an empty range gave %v instead of an empty list", emptyList)
			}
		}

		lateSaleId := sell(t, harness, company.OwnerToken, "night-sale-1", teaId, 1, cash(1180))
		harness.ExecForCompany(company.Id, `UPDATE sales SET created_at = $1 WHERE id = $2`, time.Date(2026, 3, 10, 23, 30, 0, 0, time.UTC), lateSaleId)
		edgeSaleId := sell(t, harness, company.OwnerToken, "night-sale-2", teaId, 1, cash(1180))
		harness.ExecForCompany(company.Id, `UPDATE sales SET created_at = $1 WHERE id = $2`, time.Date(2026, 3, 10, 21, 0, 0, 0, time.UTC), edgeSaleId)

		nightDays := harness.Call(http.MethodGet, "/api/reports/daily?from=2026-03-10&to=2026-03-11", company.OwnerToken, nil).Body["data"].([]any)
		firstDay := nightDays[0].(map[string]any)
		secondDay := nightDays[1].(map[string]any)
		if firstDay["date"] != "2026-03-10" || number(t, firstDay, "sale_count") != 0 || secondDay["date"] != "2026-03-11" || number(t, secondDay, "sale_count") != 2 {
			t.Fatalf("23:30 and 21:00 UTC must count on the next day in Dar es Salaam: %v", nightDays)
		}
		onlyTenth := harness.Call(http.MethodGet, "/api/reports/summary?from=2026-03-10&to=2026-03-10", company.OwnerToken, nil).Data()
		if number(t, onlyTenth, "sale_count") != 0 {
			t.Fatalf("the 10th counted %v sales", onlyTenth["sale_count"])
		}

		for _, badRange := range []string{"from=2026-03-11&to=2026-03-10", "from=2025-01-01&to=2026-03-10", "from=10-03-2026", "to=yesterday"} {
			if harness.Call(http.MethodGet, "/api/reports/summary?"+badRange, company.OwnerToken, nil).Status != http.StatusBadRequest {
				t.Fatalf("%s was accepted", badRange)
			}
		}
		if harness.Call(http.MethodGet, "/api/reports/daily?from=2025-03-11&to=2026-03-11", company.OwnerToken, nil).Status != http.StatusOK {
			t.Fatal("a full year was refused")
		}
	})
}

func TestReportQueriesDoNotGrowWithSales(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Busy Shop", "owner@busy.test")
		waterId := newProduct(t, harness, company.OwnerToken, map[string]any{"sku": "WATER", "name": "Water", "price": 500, "cost_price": 200, "opening_quantity": 1000})

		countReportQueries := func() int64 {
			return harness.CountQueries(func() {
				for _, path := range []string{"/api/reports/summary", "/api/reports/daily", "/api/reports/products", "/api/reports/cashiers", "/api/reports/shops", "/api/reports/inventory", "/api/dashboard"} {
					if harness.Call(http.MethodGet, path, company.OwnerToken, nil).Status != http.StatusOK {
						t.Fatalf("%s failed", path)
					}
				}
			})
		}

		for saleIndex := 0; saleIndex < 10; saleIndex++ {
			sell(t, harness, company.OwnerToken, fmt.Sprintf("busy-%03d", saleIndex), waterId, 1, cash(500))
		}
		fewSalesQueries := countReportQueries()
		for saleIndex := 10; saleIndex < 150; saleIndex++ {
			sell(t, harness, company.OwnerToken, fmt.Sprintf("busy-%03d", saleIndex), waterId, 1+saleIndex%3, cash(1500))
		}
		manySalesQueries := countReportQueries()
		if fewSalesQueries != manySalesQueries {
			t.Fatalf("reports ran %d queries with 10 sales and %d with 150", fewSalesQueries, manySalesQueries)
		}
	})
}

func TestReportsFollowShopsAndTenants(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Two Shops", "owner@twoshops.test")
		otherCompany := harness.CreateCompany("Rival", "owner@rival.test")
		breadId := newProduct(t, harness, company.OwnerToken, map[string]any{"sku": "BREAD", "name": "Bread", "price": 1000, "cost_price": 400, "opening_quantity": 50})
		sell(t, harness, company.OwnerToken, "main-sale-1", breadId, 3, cash(3000))

		branch := harness.Call(http.MethodPost, "/api/shops", company.OwnerToken, map[string]any{"name": "Branch"})
		branchId := branch.Data()["id"].(string)
		harness.Call(http.MethodPost, "/api/auth/switch-shop", company.OwnerToken, map[string]any{"shop_id": branchId})
		harness.Call(http.MethodPost, "/api/stock-movements", company.OwnerToken, map[string]any{"product_id": breadId, "reason": "purchase", "change": 10})
		sell(t, harness, company.OwnerToken, "branch-sale-1", breadId, 2, cash(2000))
		sell(t, harness, company.OwnerToken, "branch-sale-2", breadId, 1, cash(1000))

		totalFor := func(sessionToken string, shop string) (int, int64) {
			summaryResponse := harness.Call(http.MethodGet, "/api/reports/summary?shop="+shop, sessionToken, nil)
			if summaryResponse.Status != http.StatusOK {
				return summaryResponse.Status, 0
			}
			return summaryResponse.Status, number(t, summaryResponse.Data(), "total")
		}

		if _, activeTotal := totalFor(company.OwnerToken, ""); activeTotal != 3000 {
			t.Fatalf("the active branch totalled %d", activeTotal)
		}
		if _, mainTotal := totalFor(company.OwnerToken, company.ShopId.String()); mainTotal != 3000 {
			t.Fatalf("the main shop totalled %d", mainTotal)
		}
		if _, allTotal := totalFor(company.OwnerToken, "all"); allTotal != 6000 {
			t.Fatalf("all shops totalled %d", allTotal)
		}
		shopRows := harness.Call(http.MethodGet, "/api/reports/shops?shop=all", company.OwnerToken, nil).Body["data"].([]any)
		if len(shopRows) != 2 || number(t, shopRows[0].(map[string]any), "total")+number(t, shopRows[1].(map[string]any), "total") != 6000 {
			t.Fatalf("shop rows were %v", shopRows)
		}

		managerToken := harness.CreateStaff(company, "manager@twoshops.test", []string{"reports:view"}, []uuid.UUID{company.ShopId})
		if status, _ := totalFor(managerToken, branchId); status != http.StatusForbidden {
			t.Fatalf("a manager of the main shop read the branch: %d", status)
		}
		if _, managerAll := totalFor(managerToken, "all"); managerAll != 3000 {
			t.Fatalf("all shops for the manager totalled %d, want only their shop", managerAll)
		}
		cashierToken := harness.CreateStaff(company, "cashier@twoshops.test", []string{"sales:create"}, []uuid.UUID{company.ShopId})
		if harness.Call(http.MethodGet, "/api/dashboard", cashierToken, nil).Status != http.StatusForbidden {
			t.Fatal("a cashier without reports:view read the dashboard")
		}

		if status, _ := totalFor(otherCompany.OwnerToken, company.ShopId.String()); status != http.StatusNotFound {
			t.Fatalf("another company asked for this shop and got %d", status)
		}
		if _, rivalAll := totalFor(otherCompany.OwnerToken, "all"); rivalAll != 0 {
			t.Fatalf("another company saw %d in sales", rivalAll)
		}
		if harness.Call(http.MethodGet, "/api/reports/summary?shop=not-a-shop", company.OwnerToken, nil).Status != http.StatusNotFound {
			t.Fatal("a malformed shop id was accepted")
		}
	})
}
