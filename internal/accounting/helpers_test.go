package accounting_test

import (
	"context"
	"net/http"
	"testing"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/google/uuid"
)

const (
	companyZone  = "Africa/Dar_es_Salaam"
	sodaPrice    = 1180
	sodaCost     = 600
	stockAdjusts = "/api/stock-movements"
)

func turnOn(t *testing.T, harness *apptest.Harness, ownerToken string, changes map[string]any) {
	t.Helper()
	saved := harness.Call(http.MethodPut, "/api/features", ownerToken, changes)
	if saved.Status != http.StatusOK {
		t.Fatalf("saving features %v returned %d %v", changes, saved.Status, saved.Body)
	}
}

func simpleBooks(vatRegistered bool) map[string]any {
	changes := map[string]any{"accounting_mode": "simple", "vat_registered": vatRegistered}
	if vatRegistered {
		changes["vat_number"] = "40-000111-A"
	}
	return changes
}

func newProduct(t *testing.T, harness *apptest.Harness, sessionToken string, body map[string]any) string {
	t.Helper()
	created := harness.Call(http.MethodPost, "/api/products", sessionToken, body)
	if created.Status != http.StatusCreated {
		t.Fatalf("create %v returned %d: %v", body["sku"], created.Status, created.Body)
	}
	return created.Data()["id"].(string)
}

func sell(t *testing.T, harness *apptest.Harness, sessionToken string, clientRef string, productId string, quantity int, payments []map[string]any) map[string]any {
	t.Helper()
	sold := harness.Call(http.MethodPost, "/api/sales", sessionToken, map[string]any{
		"client_ref": clientRef,
		"items":      []map[string]any{{"product_id": productId, "quantity": quantity}},
		"payments":   payments,
	})
	if sold.Status != http.StatusCreated {
		t.Fatalf("sale %s returned %d: %v", clientRef, sold.Status, sold.Body)
	}
	return sold.Data()
}

func cash(amount int64) []map[string]any {
	return []map[string]any{{"method": "cash", "amount": amount}}
}

func adjust(t *testing.T, harness *apptest.Harness, sessionToken string, productId string, reason string, change int) string {
	t.Helper()
	adjusted := harness.Call(http.MethodPost, stockAdjusts, sessionToken, map[string]any{"product_id": productId, "reason": reason, "change": change})
	if adjusted.Status != http.StatusCreated {
		t.Fatalf("adjust %s %d returned %d: %v", reason, change, adjusted.Status, adjusted.Body)
	}
	return adjusted.Data()["id"].(string)
}

func startBooks(t *testing.T, harness *apptest.Harness, sessionToken string, body map[string]any) map[string]any {
	t.Helper()
	started := harness.Call(http.MethodPost, "/api/accounting/start", sessionToken, body)
	if started.Status != http.StatusCreated {
		t.Fatalf("start returned %d: %v", started.Status, started.Body)
	}
	return started.Data()
}

func number(t *testing.T, values map[string]any, key string) int64 {
	t.Helper()
	value, isNumber := values[key].(float64)
	if !isNumber {
		t.Fatalf("%s is %v, want a number (%v)", key, values[key], values)
	}
	return int64(value)
}

func accountBalance(harness *apptest.Harness, companyId uuid.UUID, systemKey string) int64 {
	return harness.QueryIntForCompany(companyId, `
		SELECT CAST(COALESCE(SUM(l.debit - l.credit), 0) AS BIGINT)
		FROM journal_lines l JOIN accounts a ON a.company_id = l.company_id AND a.id = l.account_id
		WHERE a.system_key = $1 AND l.company_id = $2`, systemKey, companyId)
}

func shopInventory(harness *apptest.Harness, companyId uuid.UUID, shopId string) int64 {
	return harness.QueryIntForCompany(companyId, `
		SELECT CAST(COALESCE(SUM(l.debit - l.credit), 0) AS BIGINT)
		FROM journal_lines l JOIN accounts a ON a.company_id = l.company_id AND a.id = l.account_id
		WHERE a.system_key = 'inventory' AND l.shop_id = $1 AND l.company_id = $2`, shopId, companyId)
}

func liveStockValue(harness *apptest.Harness, companyId uuid.UUID) int64 {
	return harness.QueryIntForCompany(companyId, `
		SELECT CAST(COALESCE(SUM(ss.quantity * p.cost_price), 0) AS BIGINT)
		FROM shop_stock ss JOIN products p ON p.company_id = ss.company_id AND p.id = ss.product_id
		WHERE ss.company_id = $1`, companyId)
}

func entryCount(harness *apptest.Harness, companyId uuid.UUID) int64 {
	return harness.QueryIntForCompany(companyId, `SELECT COUNT(*) FROM journal_entries WHERE company_id = $1`, companyId)
}

func assertEveryEntryBalances(t *testing.T, harness *apptest.Harness, companyId uuid.UUID) {
	t.Helper()
	unbalancedEntries := harness.QueryIntForCompany(companyId, `
		SELECT COUNT(*) FROM (
			SELECT entry_id FROM journal_lines WHERE company_id = $1 GROUP BY entry_id HAVING SUM(debit) <> SUM(credit)
		) unbalanced`, companyId)
	if unbalancedEntries != 0 {
		t.Fatalf("%d entries do not balance", unbalancedEntries)
	}
	lonelyEntries := harness.QueryIntForCompany(companyId, `
		SELECT COUNT(*) FROM journal_entries e
		WHERE e.company_id = $1 AND (SELECT COUNT(*) FROM journal_lines l WHERE l.company_id = e.company_id AND l.entry_id = e.id) < 2`, companyId)
	if lonelyEntries != 0 {
		t.Fatalf("%d entries have fewer than two lines", lonelyEntries)
	}
	gappedNumbers := harness.QueryIntForCompany(companyId, `SELECT COALESCE(MAX(entry_number), 0) - COUNT(*) FROM journal_entries WHERE company_id = $1`, companyId)
	if gappedNumbers != 0 {
		t.Fatalf("entry numbers have %d gaps", gappedNumbers)
	}
}

func sourceLines(t *testing.T, harness *apptest.Harness, companyId uuid.UUID, sourceType string, sourceId string) map[string]int64 {
	t.Helper()
	netByKey := map[string]int64{}
	for _, systemKey := range []string{"cash", "mobile_money", "bank", "card_clearing", "receivable", "inventory", "vat_input", "payable",
		"vat_output", "customer_deposits", "owner_capital", "owner_drawings", "sales", "other_income", "stock_gains", "cogs", "stock_losses", "money_charges", "rent"} {
		netAmount := harness.QueryIntForCompany(companyId, `
			SELECT CAST(COALESCE(SUM(l.debit - l.credit), 0) AS BIGINT)
			FROM journal_lines l
			JOIN journal_entries e ON e.company_id = l.company_id AND e.id = l.entry_id
			JOIN accounts a ON a.company_id = l.company_id AND a.id = l.account_id
			WHERE e.source_type = $1 AND e.source_id = $2 AND a.system_key = $3 AND l.company_id = $4`, sourceType, sourceId, systemKey, companyId)
		if netAmount != 0 {
			netByKey[systemKey] = netAmount
		}
	}
	return netByKey
}

func assertLines(t *testing.T, label string, got map[string]int64, want map[string]int64) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("%s lines %v, want %v", label, got, want)
	}
	for systemKey, wantAmount := range want {
		if got[systemKey] != wantAmount {
			t.Fatalf("%s lines %v, want %v", label, got, want)
		}
	}
}

func localDaysAgo(daysAgo int) time.Time {
	companyLocation, _ := time.LoadLocation(companyZone)
	now := time.Now().In(companyLocation)
	return time.Date(now.Year(), now.Month(), now.Day(), 10, 0, 0, 0, companyLocation).AddDate(0, 0, -daysAgo).UTC()
}

func localDate(daysAgo int) string {
	companyLocation, _ := time.LoadLocation(companyZone)
	return localDaysAgo(daysAgo).In(companyLocation).Format("2006-01-02")
}

func execErrorForCompany(t *testing.T, harness *apptest.Harness, companyId uuid.UUID, statement string) error {
	t.Helper()
	testContext := context.Background()
	writeTransaction, beginError := harness.Database.Writer.BeginTx(testContext, nil)
	if beginError != nil {
		t.Fatalf("begin: %v", beginError)
	}
	defer writeTransaction.Rollback()
	setTenantError := database.SetTenant(testContext, writeTransaction, harness.Database.IsPostgres(), companyId)
	if setTenantError != nil {
		t.Fatalf("set tenant: %v", setTenantError)
	}
	_, execError := writeTransaction.ExecContext(testContext, statement)
	return execError
}
