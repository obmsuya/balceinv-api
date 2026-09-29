package stock_test

import (
	"fmt"
	"net/http"
	"net/url"
	"testing"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/google/uuid"
)

func newProduct(t *testing.T, harness *apptest.Harness, sessionToken string, sku string, openingQuantity int, minimumStock int) string {
	t.Helper()
	createResponse := harness.Call(http.MethodPost, "/api/products", sessionToken, map[string]any{
		"sku": sku, "name": "Product " + sku, "price": 1000, "cost_price": 600,
		"opening_quantity": openingQuantity, "min_stock": minimumStock,
	})
	if createResponse.Status != http.StatusCreated {
		t.Fatalf("create %s returned %d: %v", sku, createResponse.Status, createResponse.Body)
	}
	return createResponse.Data()["id"].(string)
}

func adjust(harness *apptest.Harness, sessionToken string, productId string, reason string, change int) apptest.Response {
	return harness.Call(http.MethodPost, "/api/stock-movements", sessionToken, map[string]any{
		"product_id": productId, "reason": reason, "change": change, "reference": " Delivery note 7 ",
	})
}

func TestAdjustmentsNotifyOncePerCrossing(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Stock Keeper", "owner@stockkeeper.test")
		otherCompany := harness.CreateCompany("Other Keeper", "owner@otherkeeper.test")
		sodaId := newProduct(t, harness, company.OwnerToken, "SODA", 10, 5)
		foreignId := newProduct(t, harness, otherCompany.OwnerToken, "FOREIGN", 3, 1)

		steps := []struct {
			reason       string
			change       int
			wantQuantity float64
		}{
			{"damage", -3, 7},
			{"damage", -2, 5},
			{"adjustment", -1, 4},
			{"purchase", 10, 14},
			{"damage", -14, 0},
		}
		for _, step := range steps {
			stepResponse := adjust(harness, company.OwnerToken, sodaId, step.reason, step.change)
			if stepResponse.Status != http.StatusCreated || stepResponse.Data()["quantity_after"] != step.wantQuantity {
				t.Fatalf("%s %d returned %d %v", step.reason, step.change, stepResponse.Status, stepResponse.Body)
			}
		}
		lastMovement := adjust(harness, company.OwnerToken, sodaId, "return", 1).Data()
		if lastMovement["reference"] != "Delivery note 7" || lastMovement["user_name"] == nil || lastMovement["product_name"] != "Product SODA" {
			t.Fatalf("movement view is missing details: %v", lastMovement)
		}

		movementsBefore := harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM stock_movements`)
		notificationsBefore := harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM notifications`)
		belowZero := adjust(harness, company.OwnerToken, sodaId, "damage", -2)
		if belowZero.Status != http.StatusConflict || belowZero.Code() != "insufficient_stock" {
			t.Fatalf("going below zero returned %d %s, want 409", belowZero.Status, belowZero.Code())
		}
		if harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM stock_movements`) != movementsBefore ||
			harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM notifications`) != notificationsBefore {
			t.Fatal("a refused adjustment still wrote a movement or a notification")
		}

		rejected := []struct {
			body       map[string]any
			wantStatus int
		}{
			{map[string]any{"product_id": sodaId, "reason": "purchase", "change": -5}, http.StatusBadRequest},
			{map[string]any{"product_id": sodaId, "reason": "damage", "change": 5}, http.StatusBadRequest},
			{map[string]any{"product_id": sodaId, "reason": "adjustment", "change": 0}, http.StatusBadRequest},
			{map[string]any{"product_id": sodaId, "reason": "sale", "change": -1}, http.StatusBadRequest},
			{map[string]any{"product_id": sodaId, "reason": "purchase", "change": 2000000}, http.StatusBadRequest},
			{map[string]any{"product_id": foreignId, "reason": "purchase", "change": 1}, http.StatusNotFound},
			{map[string]any{"product_id": uuid.Must(uuid.NewV7()).String(), "reason": "purchase", "change": 1}, http.StatusNotFound},
		}
		for _, rejectedCase := range rejected {
			rejectedResponse := harness.Call(http.MethodPost, "/api/stock-movements", company.OwnerToken, rejectedCase.body)
			if rejectedResponse.Status != rejectedCase.wantStatus {
				t.Fatalf("%v returned %d, want %d", rejectedCase.body, rejectedResponse.Status, rejectedCase.wantStatus)
			}
		}

		unread := harness.Call(http.MethodGet, "/api/notifications?unread=true", company.OwnerToken, nil)
		unreadItems := unread.Items()
		if len(unreadItems) != 2 {
			t.Fatalf("got %d notifications, want exactly one low and one out: %v", len(unreadItems), unreadItems)
		}
		newest := unreadItems[0].(map[string]any)
		oldest := unreadItems[1].(map[string]any)
		if newest["kind"] != "out_of_stock" || oldest["kind"] != "low_stock" || oldest["quantity"] != float64(5) || newest["current_quantity"] != float64(1) {
			t.Fatalf("notifications are %v and %v", newest, oldest)
		}
		if harness.Call(http.MethodGet, "/api/notifications/unread-count", company.OwnerToken, nil).Data()["count"] != float64(2) {
			t.Fatal("unread count is not 2")
		}

		oldestId := oldest["id"].(string)
		markOne := harness.Call(http.MethodPost, "/api/notifications/"+oldestId+"/read", company.OwnerToken, nil)
		markAgain := harness.Call(http.MethodPost, "/api/notifications/"+oldestId+"/read", company.OwnerToken, nil)
		foreignMark := harness.Call(http.MethodPost, "/api/notifications/"+oldestId+"/read", otherCompany.OwnerToken, nil)
		if markOne.Data()["changed"] != float64(1) || markAgain.Data()["changed"] != float64(0) || foreignMark.Status != http.StatusNotFound {
			t.Fatalf("mark read returned %v, %v, foreign %d", markOne.Data(), markAgain.Data(), foreignMark.Status)
		}
		if harness.Call(http.MethodGet, "/api/notifications/unread-count", company.OwnerToken, nil).Data()["count"] != float64(1) {
			t.Fatal("unread count did not drop to 1")
		}
		readAll := harness.Call(http.MethodPost, "/api/notifications/read-all", company.OwnerToken, nil)
		clearRead := harness.Call(http.MethodDelete, "/api/notifications/read", company.OwnerToken, nil)
		if readAll.Data()["changed"] != float64(1) || clearRead.Data()["changed"] != float64(2) {
			t.Fatalf("read-all %v, clear %v", readAll.Data(), clearRead.Data())
		}
		if harness.Call(http.MethodGet, "/api/notifications", company.OwnerToken, nil).Data()["total"] != float64(0) {
			t.Fatal("cleared notifications are still listed")
		}
		if harness.Call(http.MethodGet, "/api/notifications", otherCompany.OwnerToken, nil).Data()["total"] != float64(0) {
			t.Fatal("another company sees this company's notifications")
		}

		cashierToken := harness.CreateStaff(company, "cashier@stockkeeper.test", []string{"products:view"}, []uuid.UUID{company.ShopId})
		cashierCalls := []apptest.Response{
			harness.Call(http.MethodGet, "/api/stock", cashierToken, nil),
			harness.Call(http.MethodGet, "/api/stock-movements", cashierToken, nil),
			adjust(harness, cashierToken, sodaId, "purchase", 1),
			harness.Call(http.MethodGet, "/api/notifications", cashierToken, nil),
		}
		for callIndex, cashierCall := range cashierCalls {
			if cashierCall.Status != http.StatusForbidden {
				t.Fatalf("cashier call %d returned %d, want 403", callIndex, cashierCall.Status)
			}
		}
	})
}

func TestStockLevelsHistoryAndInvariant(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Level Shop", "owner@levels.test")

		productIds := []string{}
		for productNumber := 1; productNumber <= 30; productNumber++ {
			productIds = append(productIds, newProduct(t, harness, company.OwnerToken, fmt.Sprintf("ITEM-%02d", productNumber), productNumber%7, 3))
		}
		for productIndex, productId := range productIds {
			if productIndex%3 == 0 {
				adjust(harness, company.OwnerToken, productId, "purchase", 12)
			}
			if productIndex%4 == 0 && productIndex%7 != 0 {
				adjust(harness, company.OwnerToken, productId, "damage", -1)
			}
		}

		mismatches := harness.QueryIntForCompany(company.Id, `
			SELECT COUNT(*) FROM shop_stock ss
			WHERE ss.quantity <> (SELECT COALESCE(SUM(m.change), 0) FROM stock_movements m
			                      WHERE m.company_id = ss.company_id AND m.shop_id = ss.shop_id AND m.product_id = ss.product_id)
		`)
		if mismatches != 0 {
			t.Fatalf("%d products have stock that does not equal the sum of their movements", mismatches)
		}

		var levelPage apptest.Response
		levelQueries := harness.CountQueries(func() {
			levelPage = harness.Call(http.MethodGet, "/api/stock?limit=25", company.OwnerToken, nil)
		})
		if levelPage.Data()["total"] != float64(30) || len(levelPage.Items()) != 25 || levelQueries > 2 {
			t.Fatalf("levels returned %v of %v in %d queries", len(levelPage.Items()), levelPage.Data()["total"], levelQueries)
		}

		summary := harness.Call(http.MethodGet, "/api/stock/summary", company.OwnerToken, nil).Data()
		totalUnits := harness.QueryIntForCompany(company.Id, `SELECT COALESCE(SUM(quantity), 0) FROM shop_stock`)
		outCount := harness.Call(http.MethodGet, "/api/stock?status=out", company.OwnerToken, nil).Data()["total"]
		lowOrOutCount := harness.Call(http.MethodGet, "/api/stock?status=low", company.OwnerToken, nil).Data()["total"].(float64)
		if summary["product_count"] != float64(30) || summary["total_units"] != float64(totalUnits) ||
			summary["value_at_cost"] != float64(totalUnits*600) || summary["value_at_price"] != float64(totalUnits*1000) {
			t.Fatalf("summary %v does not match %d units", summary, totalUnits)
		}
		if summary["out_count"] != outCount || summary["low_count"].(float64)+summary["out_count"].(float64) != lowOrOutCount {
			t.Fatalf("summary %v disagrees with filters out=%v low=%v", summary, outCount, lowOrOutCount)
		}
		searched := harness.Call(http.MethodGet, "/api/stock?q=item-07", company.OwnerToken, nil)
		if searched.Data()["total"] != float64(1) {
			t.Fatalf("search found %v items", searched.Data()["total"])
		}

		firstProductHistory := harness.Call(http.MethodGet, "/api/stock-movements?product_id="+productIds[0], company.OwnerToken, nil)
		if firstProductHistory.Data()["total"] != float64(2) {
			t.Fatalf("first product has %v movements, want opening + purchase", firstProductHistory.Data()["total"])
		}
		var historyPage apptest.Response
		historyQueries := harness.CountQueries(func() {
			historyPage = harness.Call(http.MethodGet, "/api/stock-movements?reason=purchase&limit=5", company.OwnerToken, nil)
		})
		if historyPage.Data()["total"] != float64(10) || len(historyPage.Items()) != 5 || historyQueries > 2 {
			t.Fatalf("purchase history %v of %v in %d queries", len(historyPage.Items()), historyPage.Data()["total"], historyQueries)
		}
		future := url.QueryEscape(time.Now().Add(time.Hour).UTC().Format(time.RFC3339))
		past := url.QueryEscape(time.Now().Add(-time.Hour).UTC().Format(time.RFC3339))
		if harness.Call(http.MethodGet, "/api/stock-movements?from="+future, company.OwnerToken, nil).Data()["total"] != float64(0) {
			t.Fatal("a from date in the future still returned movements")
		}
		inWindow := harness.Call(http.MethodGet, "/api/stock-movements?from="+past+"&to="+future, company.OwnerToken, nil).Data()["total"]
		allMovements := harness.Call(http.MethodGet, "/api/stock-movements", company.OwnerToken, nil).Data()["total"]
		if inWindow != allMovements {
			t.Fatalf("the last-hour window found %v of %v movements", inWindow, allMovements)
		}
		badFilters := []string{"?status=soon", "?reason=gift", "?product_id=abc", "?from=yesterday"}
		for _, badFilter := range badFilters {
			path := "/api/stock-movements" + badFilter
			if badFilter == "?status=soon" {
				path = "/api/stock" + badFilter
			}
			if harness.Call(http.MethodGet, path, company.OwnerToken, nil).Status != http.StatusBadRequest {
				t.Fatalf("%s was not rejected", path)
			}
		}

		branch := harness.Call(http.MethodPost, "/api/shops", company.OwnerToken, map[string]any{"name": "Branch"})
		switched := harness.Call(http.MethodPost, "/api/auth/switch-shop", company.OwnerToken, map[string]any{"shop_id": branch.Data()["id"]})
		if switched.Status != http.StatusOK {
			t.Fatalf("switch shop returned %d", switched.Status)
		}
		branchSummary := harness.Call(http.MethodGet, "/api/stock/summary", company.OwnerToken, nil).Data()
		branchHistory := harness.Call(http.MethodGet, "/api/stock-movements", company.OwnerToken, nil).Data()
		if branchSummary["total_units"] != float64(0) || branchSummary["out_count"] != float64(30) || branchHistory["total"] != float64(0) {
			t.Fatalf("the new branch shows another shop's stock: %v %v", branchSummary, branchHistory["total"])
		}
	})
}
