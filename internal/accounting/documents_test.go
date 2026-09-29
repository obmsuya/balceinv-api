package accounting_test

import (
	"net/http"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
)

var everyBooksFeature = map[string]any{
	"suppliers_enabled": true, "customers_enabled": true, "credit_sales_enabled": true, "customer_orders_enabled": true,
	"accounting_mode": "simple", "vat_registered": true, "vat_number": "40-000111-A",
}

var comparedKeys = []string{
	"cash", "mobile_money", "bank", "card_clearing", "receivable", "inventory", "vat_input", "payable", "vat_output",
	"customer_deposits", "owner_capital", "sales", "stock_gains", "cogs", "stock_losses",
}

type tradingDay struct {
	riceId             string
	supplierId         string
	customerId         string
	firstPurchaseId    string
	cashPurchaseId     string
	creditSaleId       string
	collectionSaleId   string
	openOrderId        string
	voidedSupplierPay  string
	voidedCustomerPay  string
	firstPurchasePayId string
}

func mustCall(t *testing.T, harness *apptest.Harness, method string, path string, sessionToken string, body any, wantStatus int) map[string]any {
	t.Helper()
	called := harness.Call(method, path, sessionToken, body)
	if called.Status != wantStatus {
		t.Fatalf("%s %s returned %d: %v", method, path, called.Status, called.Body)
	}
	return called.Data()
}

func runTradingDay(t *testing.T, harness *apptest.Harness, ownerToken string) tradingDay {
	t.Helper()
	day := tradingDay{}
	day.supplierId = mustCall(t, harness, http.MethodPost, "/api/suppliers", ownerToken, map[string]any{"name": "Azam Wholesale", "opening_balance": 50000}, http.StatusCreated)["id"].(string)
	mustCall(t, harness, http.MethodPut, "/api/suppliers/"+day.supplierId, ownerToken, map[string]any{"name": "Azam Wholesale", "opening_balance": 70000}, http.StatusOK)
	day.riceId = newProduct(t, harness, ownerToken, map[string]any{"sku": "RICE", "name": "Rice", "price": 1180, "cost_price": 1000, "opening_quantity": 10})

	firstPurchase := mustCall(t, harness, http.MethodPost, "/api/purchases", ownerToken, map[string]any{
		"client_ref": "purchase-0001", "supplier_id": day.supplierId, "invoice_has_vat": true, "prices_include_vat": true,
		"amount_paid": 10000, "payment_method": "cash", "lines": []map[string]any{{"product_id": day.riceId, "quantity": 20, "unit_cost": 1180}},
	}, http.StatusCreated)
	day.firstPurchaseId = firstPurchase["id"].(string)
	day.firstPurchasePayId = firstPurchase["payments"].([]any)[0].(map[string]any)["id"].(string)

	day.voidedSupplierPay = mustCall(t, harness, http.MethodPost, "/api/supplier-payments", ownerToken, map[string]any{"supplier_id": day.supplierId, "amount": 5000, "method": "bank"}, http.StatusCreated)["id"].(string)
	mustCall(t, harness, http.MethodPost, "/api/supplier-payments/"+day.voidedSupplierPay+"/void", ownerToken, map[string]any{"reason": "Bank bounced it"}, http.StatusOK)
	mustCall(t, harness, http.MethodPost, "/api/supplier-returns", ownerToken, map[string]any{
		"supplier_id": day.supplierId, "lines": []map[string]any{{"product_id": day.riceId, "quantity": 2, "unit_cost": 1000}},
	}, http.StatusCreated)

	day.cashPurchaseId = mustCall(t, harness, http.MethodPost, "/api/purchases", ownerToken, map[string]any{
		"client_ref": "purchase-0002", "amount_paid": 5000, "payment_method": "mobile",
		"lines": []map[string]any{{"product_id": day.riceId, "quantity": 5, "unit_cost": 1000}},
	}, http.StatusCreated)["id"].(string)
	mustCall(t, harness, http.MethodPost, "/api/purchases/"+day.cashPurchaseId+"/cancel", ownerToken, map[string]any{"reason": "Wrong delivery"}, http.StatusOK)

	day.customerId = mustCall(t, harness, http.MethodPost, "/api/customers", ownerToken, map[string]any{"name": "Juma", "opening_balance": 5000}, http.StatusCreated)["id"].(string)
	day.creditSaleId = mustCall(t, harness, http.MethodPost, "/api/sales", ownerToken, map[string]any{
		"client_ref": "credit-sale-001", "customer_id": day.customerId,
		"items": []map[string]any{{"product_id": day.riceId, "quantity": 2}}, "payments": []map[string]any{{"method": "credit", "amount": 2360}},
	}, http.StatusCreated)["id"].(string)
	day.voidedCustomerPay = mustCall(t, harness, http.MethodPost, "/api/customers/"+day.customerId+"/payments", ownerToken, map[string]any{"amount": 3000, "method": "mobile"}, http.StatusCreated)["id"].(string)
	mustCall(t, harness, http.MethodPost, "/api/customers/"+day.customerId+"/payments/"+day.voidedCustomerPay+"/void", ownerToken, map[string]any{"reason": "Money never arrived"}, http.StatusOK)
	mustCall(t, harness, http.MethodPost, "/api/customers/"+day.customerId+"/payments", ownerToken, map[string]any{"amount": 1000, "method": "cash"}, http.StatusCreated)

	collectedOrder := mustCall(t, harness, http.MethodPost, "/api/orders", ownerToken, map[string]any{
		"customer_id": day.customerId, "items": []map[string]any{{"product_id": day.riceId, "quantity": 3}}, "deposit": map[string]any{"method": "cash", "amount": 1000},
	}, http.StatusCreated)
	collectedOrderId := collectedOrder["id"].(string)
	mustCall(t, harness, http.MethodPost, "/api/orders/"+collectedOrderId+"/deposits", ownerToken, map[string]any{"method": "mobile", "amount": 500}, http.StatusOK)
	collected := mustCall(t, harness, http.MethodPost, "/api/orders/"+collectedOrderId+"/collect", ownerToken, map[string]any{
		"payments": []map[string]any{{"method": "cash", "amount": 3000}},
	}, http.StatusOK)
	day.collectionSaleId = collected["sale_id"].(string)

	cancelledOrderId := mustCall(t, harness, http.MethodPost, "/api/orders", ownerToken, map[string]any{
		"customer_id": day.customerId, "items": []map[string]any{{"product_id": day.riceId, "quantity": 1}}, "deposit": map[string]any{"method": "cash", "amount": 700},
	}, http.StatusCreated)["id"].(string)
	mustCall(t, harness, http.MethodPost, "/api/orders/"+cancelledOrderId+"/cancel", ownerToken, map[string]any{"reason": "Changed mind", "refund_method": "cash"}, http.StatusOK)

	day.openOrderId = mustCall(t, harness, http.MethodPost, "/api/orders", ownerToken, map[string]any{
		"customer_id": day.customerId, "items": []map[string]any{{"product_id": day.riceId, "quantity": 4}},
	}, http.StatusCreated)["id"].(string)
	return day
}

func TestSuppliersCustomersAndOrdersPostToTheBooks(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		liveCompany := harness.CreateCompany("Live Books", "owner@livebooks.test")
		historyCompany := harness.CreateCompany("History Books", "owner@historybooks.test")
		turnOn(t, harness, liveCompany.OwnerToken, everyBooksFeature)
		turnOn(t, harness, historyCompany.OwnerToken, everyBooksFeature)
		startBooks(t, harness, liveCompany.OwnerToken, map[string]any{"mode": "today"})

		day := runTradingDay(t, harness, liveCompany.OwnerToken)
		companyId := liveCompany.Id

		if accountBalance(harness, companyId, "payable") != -(70000 + 23600 - 10000 - 2000) {
			t.Fatalf("what we owe suppliers is %d", accountBalance(harness, companyId, "payable"))
		}
		assertLines(t, "purchase with VAT", sourceLines(t, harness, companyId, "purchase", day.firstPurchaseId), map[string]int64{
			"inventory": 20000, "vat_input": 3600, "payable": -23600,
		})
		assertLines(t, "paid at the delivery", sourceLines(t, harness, companyId, "supplier_payment", day.firstPurchasePayId), map[string]int64{"payable": 10000, "cash": -10000})
		assertLines(t, "voided supplier payment", sourceLines(t, harness, companyId, "supplier_payment_void", day.voidedSupplierPay), map[string]int64{"payable": -5000, "bank": 5000})
		assertLines(t, "cancelled purchase", sourceLines(t, harness, companyId, "purchase_cancel", day.cashPurchaseId), map[string]int64{"inventory": -5000, "payable": 5000})
		purchaseMovementsPosted := harness.QueryIntForCompany(companyId, `
			SELECT COUNT(*) FROM journal_entries e JOIN stock_movements m ON m.company_id = e.company_id AND m.id = e.source_id
			WHERE e.source_type = 'stock_adjustment' AND m.reason IN ('purchase', 'return', 'order_reserved', 'order_cancelled')`)
		if purchaseMovementsPosted != 0 {
			t.Fatalf("%d purchase, return or order movements were posted twice as stock changes", purchaseMovementsPosted)
		}

		assertLines(t, "credit sale", sourceLines(t, harness, companyId, "sale", day.creditSaleId), map[string]int64{
			"receivable": 2360, "sales": -2000, "vat_output": -360, "cogs": 2000, "inventory": -2000,
		})
		assertLines(t, "collected order", sourceLines(t, harness, companyId, "sale", day.collectionSaleId), map[string]int64{
			"customer_deposits": 1500, "cash": 2040, "sales": -3000, "vat_output": -540, "cogs": 3000, "inventory": -3000,
		})
		if accountBalance(harness, companyId, "customer_deposits") != 0 {
			t.Fatalf("customer deposits left %d after collecting and refunding", accountBalance(harness, companyId, "customer_deposits"))
		}
		if accountBalance(harness, companyId, "receivable") != 5000+2360-1000 {
			t.Fatalf("customers owe %d", accountBalance(harness, companyId, "receivable"))
		}
		customerParty := harness.QueryIntForCompany(companyId, `SELECT COUNT(*) FROM journal_entries WHERE party_type = 'customer' AND party_id = $1`, day.customerId)
		if customerParty < 5 {
			t.Fatalf("only %d entries remember the customer", customerParty)
		}

		integrity := mustCall(t, harness, http.MethodGet, "/api/accounting/overview", liveCompany.OwnerToken, nil, http.StatusOK)
		if number(t, integrity, "what_i_owe") != 81600+accountBalance(harness, companyId, "vat_output")*-1 {
			t.Fatalf("what I owe was %v", integrity["what_i_owe"])
		}
		turnOn(t, harness, liveCompany.OwnerToken, map[string]any{"accounting_mode": "full"})
		check := mustCall(t, harness, http.MethodGet, "/api/accounting/integrity", liveCompany.OwnerToken, nil, http.StatusOK)
		if check["is_balanced"] != true || number(t, check, "inventory_difference") != 0 || number(t, check, "unposted_count") != 0 || number(t, check, "sales_difference") != 0 {
			t.Fatalf("books check with an open order %v", check)
		}
		assertEveryEntryBalances(t, harness, companyId)

		runTradingDay(t, harness, historyCompany.OwnerToken)
		if entryCount(harness, historyCompany.Id) != 0 {
			t.Fatal("entries were posted before the history books started")
		}
		historyStatus := startBooks(t, harness, historyCompany.OwnerToken, map[string]any{"mode": "history"})
		if number(t, historyStatus, "unposted_count") != 0 {
			t.Fatalf("history start left %v records out", historyStatus["unposted_count"])
		}
		for _, systemKey := range comparedKeys {
			liveBalance := accountBalance(harness, companyId, systemKey)
			historyBalance := accountBalance(harness, historyCompany.Id, systemKey)
			if liveBalance != historyBalance {
				t.Fatalf("%s is %d when posted live but %d when rebuilt from history", systemKey, liveBalance, historyBalance)
			}
		}
		assertEveryEntryBalances(t, harness, historyCompany.Id)
	})
}
