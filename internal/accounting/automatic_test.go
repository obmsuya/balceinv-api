package accounting_test

import (
	"net/http"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
)

func TestSalesAndStockPostBalancedEntries(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Books Shop", "owner@books.test")
		turnOn(t, harness, company.OwnerToken, simpleBooks(true))

		started := startBooks(t, harness, company.OwnerToken, map[string]any{"cash_in_drawer": 50000})
		if started["started"] != true || started["start_mode"] != "today" {
			t.Fatalf("an empty shop started as %v", started)
		}
		if accountBalance(harness, company.Id, "cash") != 50000 || accountBalance(harness, company.Id, "owner_capital") != -50000 {
			t.Fatal("the cash in the drawer did not open the books")
		}

		sodaId := newProduct(t, harness, company.OwnerToken, map[string]any{"sku": "SODA", "name": "Soda", "price": sodaPrice, "cost_price": sodaCost, "opening_quantity": 40})
		openingMovementId := harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM stock_movements WHERE reason = 'opening'`)
		if openingMovementId != 1 || accountBalance(harness, company.Id, "inventory") != 40*sodaCost {
			t.Fatalf("opening stock of a new product booked %d to inventory", accountBalance(harness, company.Id, "inventory"))
		}

		mixedSale := sell(t, harness, company.OwnerToken, "books-sale-0001", sodaId, 10, []map[string]any{
			{"method": "cash", "amount": 5000}, {"method": "mobile", "amount": 4000}, {"method": "card", "amount": 3000},
		})
		if number(t, mixedSale, "change_given") != 200 || number(t, mixedSale, "tax_total") != 1800 {
			t.Fatalf("the sale was %v", mixedSale)
		}
		assertLines(t, "mixed sale", sourceLines(t, harness, company.Id, "sale", mixedSale["id"].(string)), map[string]int64{
			"cash": 4800, "mobile_money": 4000, "card_clearing": 3000, "sales": -10000, "vat_output": -1800, "cogs": 6000, "inventory": -6000,
		})

		entriesBefore := entryCount(harness, company.Id)
		replayed := harness.Call(http.MethodPost, "/api/sales", company.OwnerToken, map[string]any{
			"client_ref": "books-sale-0001", "items": []map[string]any{{"product_id": sodaId, "quantity": 10}},
			"payments": []map[string]any{{"method": "cash", "amount": 5000}, {"method": "mobile", "amount": 4000}, {"method": "card", "amount": 3000}},
		})
		if replayed.Status != http.StatusCreated || entryCount(harness, company.Id) != entriesBefore {
			t.Fatalf("a retried sale posted again: %d", replayed.Status)
		}

		damagedId := adjust(t, harness, company.OwnerToken, sodaId, "damage", -2)
		assertLines(t, "damage", sourceLines(t, harness, company.Id, "stock_adjustment", damagedId), map[string]int64{"stock_losses": 1200, "inventory": -1200})
		countedUpId := adjust(t, harness, company.OwnerToken, sodaId, "adjustment", 3)
		assertLines(t, "count up", sourceLines(t, harness, company.Id, "stock_adjustment", countedUpId), map[string]int64{"inventory": 1800, "stock_gains": -1800})
		countedDownId := adjust(t, harness, company.OwnerToken, sodaId, "adjustment", -1)
		assertLines(t, "count down", sourceLines(t, harness, company.Id, "stock_adjustment", countedDownId), map[string]int64{"stock_losses": 600, "inventory": -600})
		returnedId := adjust(t, harness, company.OwnerToken, sodaId, "return", 1)
		assertLines(t, "customer return", sourceLines(t, harness, company.Id, "stock_adjustment", returnedId), map[string]int64{"inventory": 600, "stock_gains": -600})
		receivedId := adjust(t, harness, company.OwnerToken, sodaId, "purchase", 5)
		assertLines(t, "stock received", sourceLines(t, harness, company.Id, "stock_adjustment", receivedId), map[string]int64{"inventory": 3000, "owner_capital": -3000})

		freeId := newProduct(t, harness, company.OwnerToken, map[string]any{"sku": "FREE", "name": "Free sample", "price": 0, "opening_quantity": 5})
		entriesBeforeFree := entryCount(harness, company.Id)
		adjust(t, harness, company.OwnerToken, freeId, "damage", -1)
		if entryCount(harness, company.Id) != entriesBeforeFree {
			t.Fatal("stock with no cost price posted an empty entry")
		}

		branchId := harness.Call(http.MethodPost, "/api/shops", company.OwnerToken, map[string]any{"name": "Branch"}).Data()["id"].(string)
		mainShopId := company.ShopId.String()
		mainBefore := shopInventory(harness, company.Id, mainShopId)
		sent := harness.Call(http.MethodPost, "/api/stock-transfers", company.OwnerToken, map[string]any{
			"to_shop_id": branchId, "items": []map[string]any{{"product_id": sodaId, "quantity": 6}},
		})
		if sent.Status != http.StatusCreated {
			t.Fatalf("transfer returned %d %v", sent.Status, sent.Body)
		}
		if shopInventory(harness, company.Id, branchId) != 6*sodaCost || shopInventory(harness, company.Id, mainShopId) != mainBefore-6*sodaCost {
			t.Fatalf("the transfer left the branch at %d and main at %d", shopInventory(harness, company.Id, branchId), shopInventory(harness, company.Id, mainShopId))
		}

		if accountBalance(harness, company.Id, "inventory") != liveStockValue(harness, company.Id) {
			t.Fatalf("inventory account %d, live stock %d", accountBalance(harness, company.Id, "inventory"), liveStockValue(harness, company.Id))
		}
		assertEveryEntryBalances(t, harness, company.Id)

		turnOn(t, harness, company.OwnerToken, map[string]any{"vat_registered": false})
		plainSale := sell(t, harness, company.OwnerToken, "books-sale-0002", sodaId, 1, cash(sodaPrice))
		assertLines(t, "sale without VAT", sourceLines(t, harness, company.Id, "sale", plainSale["id"].(string)), map[string]int64{
			"cash": sodaPrice, "sales": -sodaPrice, "cogs": sodaCost, "inventory": -sodaCost,
		})

		for _, forbiddenChange := range []string{
			`UPDATE journal_lines SET debit = debit + 1 WHERE debit > 0`,
			`UPDATE journal_entries SET memo = 'changed'`,
			`DELETE FROM journal_lines`,
			`DELETE FROM journal_entries`,
		} {
			if execErrorForCompany(t, harness, company.Id, forbiddenChange) == nil {
				t.Fatalf("the database allowed %q", forbiddenChange)
			}
		}
		assertEveryEntryBalances(t, harness, company.Id)
	})
}

func TestNothingPostsWhileBooksAreOffOrNotStarted(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Quiet Shop", "owner@quiet.test")
		sodaId := newProduct(t, harness, company.OwnerToken, map[string]any{"sku": "SODA", "name": "Soda", "price": sodaPrice, "cost_price": sodaCost, "opening_quantity": 20})
		sell(t, harness, company.OwnerToken, "quiet-sale-0001", sodaId, 1, cash(sodaPrice))

		offStatus := harness.Call(http.MethodGet, "/api/accounting/status", company.OwnerToken, nil)
		if offStatus.Status != http.StatusForbidden || offStatus.Code() != "feature_off" {
			t.Fatalf("status with accounting off returned %d %s", offStatus.Status, offStatus.Code())
		}
		if harness.Call(http.MethodPost, "/api/accounting/start", company.OwnerToken, map[string]any{}).Code() != "feature_off" {
			t.Fatal("the books started with accounting off")
		}

		turnOn(t, harness, company.OwnerToken, simpleBooks(false))
		sell(t, harness, company.OwnerToken, "quiet-sale-0002", sodaId, 1, cash(sodaPrice))
		adjust(t, harness, company.OwnerToken, sodaId, "damage", -1)
		if harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM journal_entries`) != 0 {
			t.Fatal("entries were posted before the books started")
		}
		notStarted := harness.Call(http.MethodPost, "/api/accounting/money", company.OwnerToken, map[string]any{
			"client_ref": "quiet-money-01", "kind": "owner_in", "amount": 1000, "money_account": "cash",
		})
		if notStarted.Status != http.StatusConflict || notStarted.Code() != "books_not_started" {
			t.Fatalf("money before the start returned %d %s", notStarted.Status, notStarted.Code())
		}

		status := harness.Call(http.MethodGet, "/api/accounting/status", company.OwnerToken, nil).Data()
		if status["started"] != false || status["suggested_start_mode"] != "history" || status["first_record_date"] == nil {
			t.Fatalf("status before start was %v", status)
		}

		startBooks(t, harness, company.OwnerToken, map[string]any{"mode": "today"})
		assertEveryEntryBalances(t, harness, company.Id)
		if harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM journal_entries WHERE source_type = 'sale'`) != 0 {
			t.Fatal("starting from today posted old sales")
		}
		if accountBalance(harness, company.Id, "inventory") != liveStockValue(harness, company.Id) || accountBalance(harness, company.Id, "inventory") != 17*sodaCost {
			t.Fatalf("starting today valued stock at %d", accountBalance(harness, company.Id, "inventory"))
		}
		again := harness.Call(http.MethodPost, "/api/accounting/start", company.OwnerToken, map[string]any{})
		if again.Status != http.StatusConflict || again.Code() != "books_already_started" {
			t.Fatalf("starting twice returned %d %s", again.Status, again.Code())
		}

		turnOn(t, harness, company.OwnerToken, map[string]any{"accounting_mode": "off"})
		missedSale := sell(t, harness, company.OwnerToken, "quiet-sale-0003", sodaId, 2, cash(2*sodaPrice))
		if harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM journal_entries WHERE source_type = 'sale'`) != 0 {
			t.Fatal("a sale posted while accounting was off")
		}
		if harness.Call(http.MethodGet, "/api/accounting/overview", company.OwnerToken, nil).Code() != "feature_off" {
			t.Fatal("the books were visible while accounting was off")
		}

		turnOn(t, harness, company.OwnerToken, map[string]any{"accounting_mode": "simple"})
		backOn := harness.Call(http.MethodGet, "/api/accounting/status", company.OwnerToken, nil).Data()
		if backOn["started"] != true || number(t, backOn, "unposted_count") != 1 {
			t.Fatalf("after turning back on the status was %v", backOn)
		}
		caughtUp := harness.Call(http.MethodPost, "/api/accounting/catch-up", company.OwnerToken, nil)
		if caughtUp.Status != http.StatusOK || number(t, caughtUp.Data(), "posted") != 1 {
			t.Fatalf("catch up returned %d %v", caughtUp.Status, caughtUp.Body)
		}
		if len(sourceLines(t, harness, company.Id, "sale", missedSale["id"].(string))) == 0 {
			t.Fatal("the missed sale is not in the books")
		}
		if number(t, harness.Call(http.MethodPost, "/api/accounting/catch-up", company.OwnerToken, nil).Data(), "posted") != 0 {
			t.Fatal("catching up twice posted the same sale again")
		}
		assertEveryEntryBalances(t, harness, company.Id)
	})
}

func TestStartingFromHistoryRebuildsTheBooks(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("History Shop", "owner@history.test")
		mainShopId := company.ShopId.String()
		branchId := harness.Call(http.MethodPost, "/api/shops", company.OwnerToken, map[string]any{"name": "Branch"}).Data()["id"].(string)

		sodaId := newProduct(t, harness, company.OwnerToken, map[string]any{"sku": "SODA", "name": "Soda", "price": sodaPrice, "cost_price": sodaCost, "opening_quantity": 50})
		riceId := newProduct(t, harness, company.OwnerToken, map[string]any{"sku": "RICE", "name": "Rice", "price": 3540, "cost_price": 2000, "opening_quantity": 10})
		harness.ExecForCompany(company.Id, `UPDATE stock_movements SET created_at = $1`, localDaysAgo(6))

		firstSale := sell(t, harness, company.OwnerToken, "history-sale-01", sodaId, 5, cash(10000))
		harness.ExecForCompany(company.Id, `UPDATE sales SET created_at = $1 WHERE id = $2`, localDaysAgo(5), firstSale["id"])
		harness.ExecForCompany(company.Id, `UPDATE stock_movements SET created_at = $1 WHERE reason = 'sale'`, localDaysAgo(5))

		damageId := adjust(t, harness, company.OwnerToken, riceId, "damage", -1)
		gainId := adjust(t, harness, company.OwnerToken, sodaId, "adjustment", 2)
		harness.ExecForCompany(company.Id, `UPDATE stock_movements SET created_at = $1 WHERE id IN ($2, $3)`, localDaysAgo(4), damageId, gainId)

		sent := harness.Call(http.MethodPost, "/api/stock-transfers", company.OwnerToken, map[string]any{
			"to_shop_id": branchId, "items": []map[string]any{{"product_id": riceId, "quantity": 4}, {"product_id": sodaId, "quantity": 7}},
		}).Data()
		harness.ExecForCompany(company.Id, `UPDATE stock_transfers SET created_at = $1 WHERE id = $2`, localDaysAgo(3), sent["id"])
		harness.ExecForCompany(company.Id, `UPDATE stock_movements SET created_at = $1 WHERE reference = $2`, localDaysAgo(3), sent["id"])

		secondSale := sell(t, harness, company.OwnerToken, "history-sale-02", riceId, 2, []map[string]any{{"method": "mobile", "amount": 7080}})
		harness.ExecForCompany(company.Id, `UPDATE sales SET created_at = $1 WHERE id = $2`, localDaysAgo(1), secondSale["id"])
		harness.ExecForCompany(company.Id, `UPDATE stock_movements SET created_at = $1 WHERE reference = $2`, localDaysAgo(1), secondSale["receipt_number"])

		turnOn(t, harness, company.OwnerToken, simpleBooks(true))
		status := harness.Call(http.MethodGet, "/api/accounting/status", company.OwnerToken, nil).Data()
		if status["suggested_start_mode"] != "history" || status["first_record_date"] != localDate(6) {
			t.Fatalf("status before start was %v", status)
		}

		started := startBooks(t, harness, company.OwnerToken, map[string]any{"mobile_money": 25000})
		if started["started_on"] != localDate(6) || started["start_mode"] != "history" || number(t, started, "unposted_count") != 0 {
			t.Fatalf("history start returned %v", started)
		}
		assertEveryEntryBalances(t, harness, company.Id)

		postedEvents := harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM journal_entries WHERE source_type IN ('sale', 'stock_adjustment', 'stock_transfer')`)
		if postedEvents != 2+2+2+1 {
			t.Fatalf("history posted %d events, want 7", postedEvents)
		}
		outOfOrder := harness.QueryIntForCompany(company.Id, `
			SELECT COUNT(*) FROM journal_entries earlier JOIN journal_entries later
			ON later.company_id = earlier.company_id AND later.entry_number > earlier.entry_number AND later.entry_date < earlier.entry_date`)
		if outOfOrder != 0 {
			t.Fatal("history entries are not numbered in date order")
		}
		secondSaleDate := harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM journal_entries WHERE source_type = 'sale' AND entry_date = $1`, localDate(1))
		if secondSaleDate != 1 {
			t.Fatal("the historical sale was not dated on its own day")
		}

		if accountBalance(harness, company.Id, "inventory") != liveStockValue(harness, company.Id) {
			t.Fatalf("inventory account %d, live stock %d", accountBalance(harness, company.Id, "inventory"), liveStockValue(harness, company.Id))
		}
		branchStockValue := harness.QueryIntForCompany(company.Id, `
			SELECT CAST(COALESCE(SUM(ss.quantity * p.cost_price), 0) AS BIGINT) FROM shop_stock ss
			JOIN products p ON p.company_id = ss.company_id AND p.id = ss.product_id WHERE ss.shop_id = $1`, branchId)
		if shopInventory(harness, company.Id, branchId) != branchStockValue || shopInventory(harness, company.Id, mainShopId)+branchStockValue != liveStockValue(harness, company.Id) {
			t.Fatal("per-shop stock value does not match the shops")
		}
		if accountBalance(harness, company.Id, "mobile_money") != 25000+7080 {
			t.Fatalf("mobile money balance %d", accountBalance(harness, company.Id, "mobile_money"))
		}

		trialBalance := harness.Call(http.MethodGet, "/api/accounting/trial-balance", company.OwnerToken, nil)
		if trialBalance.Code() != "feature_off" {
			t.Fatal("the trial balance was shown in simple mode")
		}
		turnOn(t, harness, company.OwnerToken, map[string]any{"accounting_mode": "full"})
		trialData := harness.Call(http.MethodGet, "/api/accounting/trial-balance", company.OwnerToken, nil).Data()
		if trialData["is_balanced"] != true || number(t, trialData, "total_debit_balance") != number(t, trialData, "total_credit_balance") {
			t.Fatalf("trial balance %v", trialData)
		}

		rangeQuery := "?from=" + localDate(6) + "&to=" + localDate(0)
		profitAndLoss := harness.Call(http.MethodGet, "/api/accounting/profit-and-loss"+rangeQuery, company.OwnerToken, nil).Data()
		salesReport := harness.Call(http.MethodGet, "/api/reports/summary"+rangeQuery+"&shop=all", company.OwnerToken, nil).Data()
		ledgerSales := int64(0)
		for _, incomeRow := range profitAndLoss["income"].([]any) {
			if incomeRow.(map[string]any)["system_key"] == "sales" {
				ledgerSales = number(t, incomeRow.(map[string]any), "amount")
			}
		}
		if ledgerSales != number(t, salesReport, "net_sales") || ledgerSales-number(t, profitAndLoss, "cost_of_goods") != number(t, salesReport, "gross_profit") {
			t.Fatalf("books profit (sales %d, cogs %d) differs from the sales report %v", ledgerSales, number(t, profitAndLoss, "cost_of_goods"), salesReport)
		}
		expectedNet := number(t, salesReport, "gross_profit") + 2*sodaCost - 1*2000
		if number(t, profitAndLoss, "net_profit") != expectedNet {
			t.Fatalf("net profit %d, want %d", number(t, profitAndLoss, "net_profit"), expectedNet)
		}

		integrity := harness.Call(http.MethodGet, "/api/accounting/integrity"+rangeQuery, company.OwnerToken, nil).Data()
		if integrity["is_balanced"] != true || number(t, integrity, "sales_difference") != 0 || number(t, integrity, "inventory_difference") != 0 || number(t, integrity, "unposted_count") != 0 {
			t.Fatalf("integrity check %v", integrity)
		}

		balanceSheet := harness.Call(http.MethodGet, "/api/accounting/balance-sheet", company.OwnerToken, nil).Data()
		if balanceSheet["is_balanced"] != true || number(t, balanceSheet, "total_assets") != number(t, balanceSheet, "total_liabilities")+number(t, balanceSheet, "total_equity") {
			t.Fatalf("balance sheet %v", balanceSheet)
		}

		harness.Call(http.MethodPut, "/api/products/"+sodaId, company.OwnerToken, map[string]any{"sku": "SODA", "name": "Soda", "price": sodaPrice, "cost_price": sodaCost + 100})
		changedCost := harness.Call(http.MethodGet, "/api/accounting/integrity"+rangeQuery, company.OwnerToken, nil).Data()
		if number(t, changedCost, "inventory_difference") >= 0 {
			t.Fatalf("a higher cost price did not show as a difference: %v", changedCost)
		}
	})
}
