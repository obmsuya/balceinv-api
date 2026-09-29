package accounting_test

import (
	"net/http"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/google/uuid"
)

func findAccountId(t *testing.T, harness *apptest.Harness, sessionToken string, systemKey string) string {
	t.Helper()
	accounts := harness.Call(http.MethodGet, "/api/accounting/accounts", sessionToken, nil).Body["data"].([]any)
	for _, rawAccount := range accounts {
		account := rawAccount.(map[string]any)
		if account["system_key"] == systemKey {
			return account["id"].(string)
		}
	}
	t.Fatalf("no %s account", systemKey)
	return ""
}

func TestMoneyPageEntriesReversalsAndPermissions(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Money Shop", "owner@money.test")
		otherCompany := harness.CreateCompany("Other Money", "owner@othermoney.test")
		turnOn(t, harness, company.OwnerToken, simpleBooks(true))
		turnOn(t, harness, otherCompany.OwnerToken, simpleBooks(false))
		startBooks(t, harness, company.OwnerToken, map[string]any{"cash_in_drawer": 400000, "bank": 1000000})
		startBooks(t, harness, otherCompany.OwnerToken, map[string]any{})

		cashierToken := harness.CreateStaff(company, "cashier@money.test", []string{"sales:create"}, []uuid.UUID{company.ShopId})
		clerkToken := harness.CreateStaff(company, "clerk@money.test", []string{"accounting:view", "accounting:create"}, []uuid.UUID{company.ShopId})
		rentId := findAccountId(t, harness, company.OwnerToken, "rent")
		cogsId := findAccountId(t, harness, company.OwnerToken, "cogs")

		if harness.Call(http.MethodGet, "/api/accounting/overview", cashierToken, nil).Status != http.StatusForbidden {
			t.Fatal("a cashier saw the books")
		}

		rentBody := map[string]any{
			"client_ref": "money-rent-0001", "kind": "expense", "amount": 118000, "money_account": "cash", "expense_account_id": rentId,
			"shop_id": company.ShopId.String(), "note": "September rent", "includes_vat": true, "supplier_tin": "123-456-789", "receipt_number": "EFD-99",
		}
		rent := harness.Call(http.MethodPost, "/api/accounting/money", clerkToken, rentBody)
		if rent.Status != http.StatusCreated || rent.Data()["number"] == nil || rent.Data()["is_reversible"] != true {
			t.Fatalf("recording rent returned %d %v", rent.Status, rent.Body)
		}
		rentEntryId := rent.Data()["id"].(string)
		rentLines := harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM journal_lines WHERE entry_id = $1 AND shop_id = $2`, rentEntryId, company.ShopId)
		if rentLines != 3 || accountBalance(harness, company.Id, "rent") != 100000 || accountBalance(harness, company.Id, "vat_input") != 18000 || accountBalance(harness, company.Id, "cash") != 400000-118000 {
			t.Fatalf("rent posted %d lines, rent %d, vat %d", rentLines, accountBalance(harness, company.Id, "rent"), accountBalance(harness, company.Id, "vat_input"))
		}

		retried := harness.Call(http.MethodPost, "/api/accounting/money", clerkToken, rentBody)
		if retried.Status != http.StatusOK || retried.Data()["id"] != rentEntryId {
			t.Fatalf("a retried expense returned %d %v", retried.Status, retried.Body)
		}
		reusedRef := harness.Call(http.MethodPost, "/api/accounting/money", clerkToken, map[string]any{
			"client_ref": "money-rent-0001", "kind": "owner_in", "amount": 5, "money_account": "cash",
		})
		if reusedRef.Status != http.StatusConflict || reusedRef.Code() != "entry_ref_reused" {
			t.Fatalf("reusing a reference for another kind returned %d %s", reusedRef.Status, reusedRef.Code())
		}

		refusals := []struct {
			name string
			body map[string]any
			code string
		}{
			{"zero amount", map[string]any{"client_ref": "money-bad-00001", "kind": "owner_in", "amount": 0, "money_account": "cash"}, "validation_failed"},
			{"negative amount", map[string]any{"client_ref": "money-bad-00002", "kind": "owner_in", "amount": -5, "money_account": "cash"}, "validation_failed"},
			{"no expense account", map[string]any{"client_ref": "money-bad-00003", "kind": "expense", "amount": 500, "money_account": "cash"}, "expense_account_required"},
			{"automatic account", map[string]any{"client_ref": "money-bad-00004", "kind": "expense", "amount": 500, "money_account": "cash", "expense_account_id": cogsId}, "expense_account_required"},
			{"move to the same place", map[string]any{"client_ref": "money-bad-00005", "kind": "money_move", "amount": 500, "money_account": "cash", "to_money_account": "cash"}, "same_money_account"},
			{"vat as big as the amount", map[string]any{"client_ref": "money-bad-00006", "kind": "expense", "amount": 500, "money_account": "cash", "expense_account_id": rentId, "includes_vat": true, "vat_amount": 500}, "vat_too_large"},
			{"future date", map[string]any{"client_ref": "money-bad-00007", "kind": "owner_in", "amount": 500, "money_account": "cash", "entry_date": "2999-01-01"}, "future_date"},
			{"before the books", map[string]any{"client_ref": "money-bad-00008", "kind": "owner_in", "amount": 500, "money_account": "cash", "entry_date": "2001-01-01"}, "before_books_start"},
			{"foreign receipt photo", map[string]any{"client_ref": "money-bad-00009", "kind": "owner_in", "amount": 500, "money_account": "cash", "attachment_key": "receipts/" + otherCompany.Id.String() + "/x.png"}, "invalid_attachment"},
			{"another company's shop", map[string]any{"client_ref": "money-bad-00010", "kind": "owner_in", "amount": 500, "money_account": "cash", "shop_id": otherCompany.ShopId.String()}, "not_found"},
		}
		for _, refusal := range refusals {
			refused := harness.Call(http.MethodPost, "/api/accounting/money", company.OwnerToken, refusal.body)
			if refused.Code() != refusal.code {
				t.Fatalf("%s returned %d %s, want %s", refusal.name, refused.Status, refused.Code(), refusal.code)
			}
		}

		ownerIn := harness.Call(http.MethodPost, "/api/accounting/money", company.OwnerToken, map[string]any{"client_ref": "money-owner-in1", "kind": "owner_in", "amount": 200000, "money_account": "mobile_money"})
		ownerOut := harness.Call(http.MethodPost, "/api/accounting/money", company.OwnerToken, map[string]any{"client_ref": "money-owner-ou1", "kind": "owner_out", "amount": 50000, "money_account": "cash"})
		moved := harness.Call(http.MethodPost, "/api/accounting/money", company.OwnerToken, map[string]any{"client_ref": "money-move-0001", "kind": "money_move", "amount": 100000, "fee": 1500, "money_account": "cash", "to_money_account": "bank"})
		otherIncome := harness.Call(http.MethodPost, "/api/accounting/money", company.OwnerToken, map[string]any{"client_ref": "money-other-001", "kind": "other_income", "amount": 30000, "money_account": "cash"})
		for _, recorded := range []apptest.Response{ownerIn, ownerOut, moved, otherIncome} {
			if recorded.Status != http.StatusCreated {
				t.Fatalf("recording money returned %d %v", recorded.Status, recorded.Body)
			}
		}
		wantBalances := map[string]int64{
			"cash": 400000 - 118000 - 50000 - 101500 + 30000, "mobile_money": 200000, "bank": 1000000 + 100000,
			"owner_capital": -1400000 - 200000, "owner_drawings": 50000, "money_charges": 1500, "other_income": -30000,
		}
		for systemKey, want := range wantBalances {
			if got := accountBalance(harness, company.Id, systemKey); got != want {
				t.Fatalf("%s balance %d, want %d", systemKey, got, want)
			}
		}

		overview := harness.Call(http.MethodGet, "/api/accounting/overview", company.OwnerToken, nil)
		overviewData := overview.Data()
		balances := overviewData["balances"].(map[string]any)
		vat := overviewData["vat"].(map[string]any)
		if overview.Status != http.StatusOK || number(t, balances, "cash") != wantBalances["cash"] || number(t, balances, "bank") != 1100000 ||
			number(t, overviewData, "money_in") != 200000+30000 || number(t, overviewData, "money_out") != 118000+50000+1500 ||
			number(t, overviewData, "profit") != 30000-100000-1500 || number(t, vat, "reclaimable") != 18000 || number(t, vat, "to_pay") != -18000 {
			t.Fatalf("overview returned %d %v", overview.Status, overviewData)
		}
		if number(t, overviewData, "what_i_own") != wantBalances["cash"]+200000+1100000+18000 {
			t.Fatalf("what I own was %v", overviewData["what_i_own"])
		}

		if harness.Call(http.MethodPost, "/api/accounting/entries/"+rentEntryId+"/reverse", clerkToken, map[string]any{"reason": "Typed twice"}).Status != http.StatusForbidden {
			t.Fatal("a clerk without delete permission reversed an entry")
		}
		if harness.Call(http.MethodPost, "/api/accounting/entries/"+rentEntryId+"/reverse", otherCompany.OwnerToken, map[string]any{"reason": "Not mine"}).Status != http.StatusNotFound {
			t.Fatal("another company reversed this company's entry")
		}
		if harness.Call(http.MethodGet, "/api/accounting/entries/"+rentEntryId, otherCompany.OwnerToken, nil).Status != http.StatusNotFound {
			t.Fatal("another company saw this company's entry")
		}
		if len(harness.Call(http.MethodGet, "/api/accounting/entries", otherCompany.OwnerToken, nil).Items()) != 0 {
			t.Fatal("another company listed this company's entries")
		}
		if harness.Call(http.MethodPost, "/api/accounting/entries/"+rentEntryId+"/reverse", company.OwnerToken, map[string]any{"reason": ""}).Code() != "validation_failed" {
			t.Fatal("a reversal without a reason was accepted")
		}
		reversal := harness.Call(http.MethodPost, "/api/accounting/entries/"+rentEntryId+"/reverse", company.OwnerToken, map[string]any{"reason": "Typed twice"})
		if reversal.Status != http.StatusCreated || reversal.Data()["reverses_entry_id"] != rentEntryId || accountBalance(harness, company.Id, "rent") != 0 || accountBalance(harness, company.Id, "vat_input") != 0 {
			t.Fatalf("reversal returned %d %v", reversal.Status, reversal.Body)
		}
		if harness.Call(http.MethodPost, "/api/accounting/entries/"+rentEntryId+"/reverse", company.OwnerToken, map[string]any{"reason": "Again"}).Code() != "already_reversed" {
			t.Fatal("an entry was reversed twice")
		}
		if harness.Call(http.MethodPost, "/api/accounting/entries/"+reversal.Data()["id"].(string)+"/reverse", company.OwnerToken, map[string]any{"reason": "Undo undo"}).Code() != "not_reversible" {
			t.Fatal("a reversal was reversed")
		}
		original := harness.Call(http.MethodGet, "/api/accounting/entries/"+rentEntryId, company.OwnerToken, nil).Data()
		if original["reversed_by_entry_id"] != reversal.Data()["id"] || original["is_reversible"] != false {
			t.Fatalf("the reversed entry reads %v", original)
		}
		afterReversal := harness.Call(http.MethodGet, "/api/accounting/overview", company.OwnerToken, nil).Data()
		if number(t, afterReversal, "money_out") != 50000+1500 {
			t.Fatalf("money out after the reversal was %v", afterReversal["money_out"])
		}

		sodaId := newProduct(t, harness, company.OwnerToken, map[string]any{"sku": "SODA", "name": "Soda", "price": sodaPrice, "cost_price": sodaCost, "opening_quantity": 5})
		sale := sell(t, harness, company.OwnerToken, "money-sale-0001", sodaId, 1, cash(sodaPrice))
		saleEntries := harness.Call(http.MethodGet, "/api/accounting/entries?source_type=sale", company.OwnerToken, nil).Items()
		if len(saleEntries) != 1 || saleEntries[0].(map[string]any)["source_id"] != sale["id"] {
			t.Fatalf("sale entries were %v", saleEntries)
		}
		saleEntryId := saleEntries[0].(map[string]any)["id"].(string)
		if harness.Call(http.MethodPost, "/api/accounting/entries/"+saleEntryId+"/reverse", company.OwnerToken, map[string]any{"reason": "Wrong"}).Code() != "not_reversible" {
			t.Fatal("an automatic sale entry was reversed from the Money page")
		}

		moneyPageEntries := harness.Call(http.MethodGet, "/api/accounting/entries?source_type=expense,owner_in,owner_out,money_move,other_income,reversal", company.OwnerToken, nil)
		if len(moneyPageEntries.Items()) != 6 {
			t.Fatalf("money page listed %d entries", len(moneyPageEntries.Items()))
		}
		cashBook := harness.Call(http.MethodGet, "/api/accounting/statement?account=cash", company.OwnerToken, nil).Data()
		if number(t, cashBook, "closing_balance") != accountBalance(harness, company.Id, "cash") || number(t, cashBook, "opening_balance") != 0 {
			t.Fatalf("the cash book was %v", cashBook)
		}

		if harness.Call(http.MethodPost, "/api/accounting/manual", clerkToken, map[string]any{}).Status != http.StatusForbidden {
			t.Fatal("a clerk with only create permission reached manual entries")
		}
		if harness.Call(http.MethodPost, "/api/accounting/manual", company.OwnerToken, map[string]any{}).Code() != "feature_off" {
			t.Fatal("manual entries were open in simple mode")
		}
		assertEveryEntryBalances(t, harness, company.Id)
		assertEveryEntryBalances(t, harness, otherCompany.Id)
	})
}

func TestFullModeAccountsManualEntriesAndClosing(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Full Shop", "owner@full.test")
		turnOn(t, harness, company.OwnerToken, map[string]any{"accounting_mode": "full", "vat_registered": true, "vat_number": "40-1"})
		startBooks(t, harness, company.OwnerToken, map[string]any{"mode": "today", "cash_in_drawer": 10000})
		harness.ExecForCompany(company.Id, `UPDATE accounting_settings SET started_on = $1`, localDate(40))

		cashId := findAccountId(t, harness, company.OwnerToken, "cash")
		capitalId := findAccountId(t, harness, company.OwnerToken, "owner_capital")

		security := harness.Call(http.MethodPost, "/api/accounting/accounts", company.OwnerToken, map[string]any{"code": "6500", "name": "Security guard", "type": "expense"})
		if security.Status != http.StatusCreated || security.Data()["is_spendable"] != true {
			t.Fatalf("adding an account returned %d %v", security.Status, security.Body)
		}
		securityId := security.Data()["id"].(string)
		if harness.Call(http.MethodPost, "/api/accounting/accounts", company.OwnerToken, map[string]any{"code": "6500", "name": "Twin", "type": "expense"}).Code() != "account_code_taken" {
			t.Fatal("two accounts shared a code")
		}
		if harness.Call(http.MethodPut, "/api/accounting/accounts/"+cashId, company.OwnerToken, map[string]any{"is_active": false}).Code() != "system_account" {
			t.Fatal("a built-in account was turned off")
		}

		unbalanced := harness.Call(http.MethodPost, "/api/accounting/manual", company.OwnerToken, map[string]any{
			"client_ref": "manual-entry-01", "reason": "Fix opening cash",
			"lines": []map[string]any{{"account_id": cashId, "debit": 500}, {"account_id": capitalId, "credit": 400}},
		})
		if unbalanced.Code() != "entry_unbalanced" {
			t.Fatalf("an unbalanced manual entry returned %d %s", unbalanced.Status, unbalanced.Code())
		}
		twoSided := harness.Call(http.MethodPost, "/api/accounting/manual", company.OwnerToken, map[string]any{
			"client_ref": "manual-entry-02", "reason": "Fix",
			"lines": []map[string]any{{"account_id": cashId, "debit": 500, "credit": 500}, {"account_id": capitalId, "credit": 0}},
		})
		if twoSided.Code() != "entry_lines_invalid" {
			t.Fatalf("a two-sided line returned %d %s", twoSided.Status, twoSided.Code())
		}
		noReason := harness.Call(http.MethodPost, "/api/accounting/manual", company.OwnerToken, map[string]any{
			"client_ref": "manual-entry-03", "lines": []map[string]any{{"account_id": cashId, "debit": 500}, {"account_id": capitalId, "credit": 500}},
		})
		if noReason.Code() != "validation_failed" {
			t.Fatal("a manual entry without a reason was accepted")
		}
		manual := harness.Call(http.MethodPost, "/api/accounting/manual", company.OwnerToken, map[string]any{
			"client_ref": "manual-entry-04", "reason": "Guard paid from cash", "entry_date": localDate(20),
			"lines": []map[string]any{{"account_id": securityId, "debit": 3000}, {"account_id": cashId, "credit": 3000}},
		})
		if manual.Status != http.StatusCreated || manual.Data()["entry_date"] != localDate(20) || len(manual.Data()["lines"].([]any)) != 2 {
			t.Fatalf("a manual entry returned %d %v", manual.Status, manual.Body)
		}

		harness.Call(http.MethodPut, "/api/accounting/accounts/"+securityId, company.OwnerToken, map[string]any{"is_active": false})
		inactive := harness.Call(http.MethodPost, "/api/accounting/manual", company.OwnerToken, map[string]any{
			"client_ref": "manual-entry-05", "reason": "Guard again",
			"lines": []map[string]any{{"account_id": securityId, "debit": 3000}, {"account_id": cashId, "credit": 3000}},
		})
		if inactive.Code() != "account_not_usable" {
			t.Fatalf("an inactive account was posted to: %d %s", inactive.Status, inactive.Code())
		}
		securityRows := harness.Call(http.MethodGet, "/api/accounting/entries?account_id="+securityId, company.OwnerToken, nil).Items()
		if len(securityRows) != 1 {
			t.Fatal("the inactive account lost its history")
		}

		closeRequests := []struct {
			closedUntil string
			code        string
		}{
			{localDate(0), "close_too_recent"},
			{localDate(60), "before_books_start"},
			{localDate(10), ""},
			{localDate(15), "close_backwards"},
			{localDate(10), "close_backwards"},
		}
		for _, closeRequest := range closeRequests {
			closed := harness.Call(http.MethodPost, "/api/accounting/close", company.OwnerToken, map[string]any{"closed_until": closeRequest.closedUntil})
			if closed.Code() != closeRequest.code {
				t.Fatalf("closing until %s returned %d %s, want %q", closeRequest.closedUntil, closed.Status, closed.Code(), closeRequest.code)
			}
		}
		backDated := harness.Call(http.MethodPost, "/api/accounting/money", company.OwnerToken, map[string]any{
			"client_ref": "closed-money-01", "kind": "owner_in", "amount": 500, "money_account": "cash", "entry_date": localDate(12),
		})
		if backDated.Code() != "period_closed" {
			t.Fatalf("a back-dated entry in a closed month returned %d %s", backDated.Status, backDated.Code())
		}
		openDay := harness.Call(http.MethodPost, "/api/accounting/money", company.OwnerToken, map[string]any{
			"client_ref": "closed-money-02", "kind": "owner_in", "amount": 500, "money_account": "cash", "entry_date": localDate(9),
		})
		if openDay.Status != http.StatusCreated {
			t.Fatalf("an entry after the closed period returned %d %v", openDay.Status, openDay.Body)
		}

		receiptPhoto := append([]byte("\x89PNG\r\n\x1a\n"), make([]byte, 64)...)
		uploaded := harness.Upload("/api/accounting/receipts", company.OwnerToken, "image", "receipt.png", receiptPhoto)
		if uploaded.Status != http.StatusCreated {
			t.Fatalf("uploading a receipt returned %d %v", uploaded.Status, uploaded.Body)
		}
		withPhoto := harness.Call(http.MethodPost, "/api/accounting/money", company.OwnerToken, map[string]any{
			"client_ref": "photo-money-01", "kind": "owner_out", "amount": 100, "money_account": "cash", "attachment_key": uploaded.Data()["attachment_key"],
		})
		if withPhoto.Status != http.StatusCreated || withPhoto.Data()["has_attachment"] != true {
			t.Fatalf("an entry with a receipt photo returned %d %v", withPhoto.Status, withPhoto.Body)
		}
		photo := harness.Call(http.MethodGet, "/api/accounting/entries/"+withPhoto.Data()["id"].(string)+"/receipt", company.OwnerToken, nil)
		if photo.Status != http.StatusOK || len(photo.Raw) != len(receiptPhoto) || photo.Headers.Get("Content-Type") != "image/png" {
			t.Fatalf("reading the receipt photo returned %d", photo.Status)
		}
		if harness.Call(http.MethodGet, "/api/accounting/entries/"+openDay.Data()["id"].(string)+"/receipt", company.OwnerToken, nil).Status != http.StatusNotFound {
			t.Fatal("an entry without a photo returned one")
		}

		profitAndLoss := harness.Call(http.MethodGet, "/api/accounting/profit-and-loss?from="+localDate(40)+"&to="+localDate(0), company.OwnerToken, nil).Data()
		if number(t, profitAndLoss, "net_profit") != -3000 || len(profitAndLoss["expenses"].([]any)) != 1 {
			t.Fatalf("profit and loss %v", profitAndLoss)
		}
		balanceSheet := harness.Call(http.MethodGet, "/api/accounting/balance-sheet", company.OwnerToken, nil).Data()
		if balanceSheet["is_balanced"] != true || number(t, balanceSheet, "profit_to_date") != -3000 || number(t, balanceSheet, "total_assets") != 10000-3000+500-100 {
			t.Fatalf("balance sheet %v", balanceSheet)
		}
		if harness.Call(http.MethodGet, "/api/accounting/statement?account=cash&from=2020-01-01&to=2026-12-31", company.OwnerToken, nil).Code() != "range_too_long" {
			t.Fatal("a statement for years of lines was accepted")
		}
		assertEveryEntryBalances(t, harness, company.Id)
	})
}

func TestVatReportAndDueDates(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Vat Shop", "owner@vat.test")
		turnOn(t, harness, company.OwnerToken, simpleBooks(true))
		startBooks(t, harness, company.OwnerToken, map[string]any{"mode": "today"})
		harness.ExecForCompany(company.Id, `UPDATE accounting_settings SET started_on = '2026-01-01'`)
		sodaId := newProduct(t, harness, company.OwnerToken, map[string]any{"sku": "SODA", "name": "Soda", "price": sodaPrice, "cost_price": sodaCost, "opening_quantity": 20})
		sell(t, harness, company.OwnerToken, "vat-sale-00001", sodaId, 3, cash(3*sodaPrice))
		suppliesId := findAccountId(t, harness, company.OwnerToken, "supplies")
		for index, entryDate := range []string{"2026-08-14", "2026-08-30"} {
			harness.Call(http.MethodPost, "/api/accounting/money", company.OwnerToken, map[string]any{
				"client_ref": "vat-supplies-0" + string(rune('1'+index)), "kind": "expense", "amount": 23600, "money_account": "cash",
				"expense_account_id": suppliesId, "includes_vat": true, "entry_date": entryDate,
			})
		}
		customVat := harness.Call(http.MethodPost, "/api/accounting/money", company.OwnerToken, map[string]any{
			"client_ref": "vat-supplies-03", "kind": "expense", "amount": 10000, "money_account": "cash",
			"expense_account_id": suppliesId, "includes_vat": true, "vat_amount": 1000, "entry_date": "2026-07-02",
		})
		if customVat.Status != http.StatusCreated {
			t.Fatalf("an expense with typed VAT returned %d %v", customVat.Status, customVat.Body)
		}

		vatReport := harness.Call(http.MethodGet, "/api/accounting/vat?from=2026-01-01&to="+localDate(0), company.OwnerToken, nil).Data()
		months := vatReport["months"].([]any)
		monthsByKey := map[string]map[string]any{}
		for _, rawMonth := range months {
			month := rawMonth.(map[string]any)
			monthsByKey[month["month"].(string)] = month
		}
		july := monthsByKey["2026-07"]
		august := monthsByKey["2026-08"]
		if july == nil || number(t, july, "reclaimable") != 1000 || july["due_date"] != "2026-08-20" {
			t.Fatalf("july VAT %v", july)
		}
		if august == nil || number(t, august, "reclaimable") != 7200 || number(t, august, "to_pay") != -7200 || august["due_date"] != "2026-09-20" {
			t.Fatalf("august VAT %v", august)
		}
		saleVat := int64(3 * 180)
		if number(t, vatReport, "total_charged") != saleVat || number(t, vatReport, "total_to_pay") != saleVat-8200 {
			t.Fatalf("VAT totals %v", vatReport)
		}

		turnOn(t, harness, company.OwnerToken, map[string]any{"vat_registered": false})
		plainOverview := harness.Call(http.MethodGet, "/api/accounting/overview", company.OwnerToken, nil).Data()
		if plainOverview["vat"] != nil {
			t.Fatal("the VAT card showed for a shop that is not VAT registered")
		}
	})
}
