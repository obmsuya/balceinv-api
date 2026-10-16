package cleardata_test

import (
	"context"
	"net/http"
	"sort"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/cleardata"
	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/config"
	"github.com/chrisostomemataba/balceinv-api/internal/licensing"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/google/uuid"
)

func TestOwnerClearsEverythingButTheOwnerAndOneShop(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Trial Shop", "owner@trialshop.test")
		otherCompany := harness.CreateCompany("Other Shop", "owner@othershop.test")

		mustCall(t, harness, http.MethodPut, "/api/features", company.OwnerToken, map[string]any{"accounting_mode": "simple", "vat_registered": false}, http.StatusOK)
		mustCall(t, harness, http.MethodPost, "/api/accounting/start", company.OwnerToken, map[string]any{"cash_in_drawer": 10000}, http.StatusCreated)
		mustCall(t, harness, http.MethodPost, "/api/shops", company.OwnerToken, map[string]any{"name": "Branch"}, http.StatusCreated)
		cashierToken := harness.CreateStaff(company, "cashier@trialshop.test", []string{"sales:create", "sales:view"}, []uuid.UUID{company.ShopId})
		productId := mustCall(t, harness, http.MethodPost, "/api/products", company.OwnerToken, map[string]any{"sku": "SODA", "name": "Soda", "price": 1000, "cost_price": 600, "opening_quantity": 10}, http.StatusCreated).Data()["id"].(string)
		mustCall(t, harness, http.MethodPost, "/api/sales", cashierToken, map[string]any{
			"client_ref": "trial-sale-1",
			"items":      []map[string]any{{"product_id": productId, "quantity": 2}},
			"payments":   []map[string]any{{"method": "cash", "amount": 2000}},
		}, http.StatusCreated)
		otherProductId := mustCall(t, harness, http.MethodPost, "/api/products", otherCompany.OwnerToken, map[string]any{"sku": "KEEP", "name": "Keep", "price": 500, "opening_quantity": 3}, http.StatusCreated).Data()["id"].(string)

		if harness.Call(http.MethodPost, "/api/business/clear-data", cashierToken, clearBody("Trial Shop", company.OwnerPassword)).Code() != "clear_owner_only" {
			t.Fatal("staff cleared the business")
		}
		if harness.Call(http.MethodPost, "/api/business/clear-data", company.OwnerToken, clearBody("Trial Shp", company.OwnerPassword)).Code() != "clear_name_mismatch" {
			t.Fatal("a mistyped business name cleared the business")
		}
		if harness.Call(http.MethodPost, "/api/business/clear-data", company.OwnerToken, clearBody("Trial Shop", "wrong-password")).Code() != "clear_wrong_password" {
			t.Fatal("a wrong password cleared the business")
		}
		if harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM sales WHERE company_id = $1`, company.Id) != 1 {
			t.Fatal("a refused clear deleted data")
		}

		cleared := mustCall(t, harness, http.MethodPost, "/api/business/clear-data", company.OwnerToken, clearBody("  trial   SHOP ", company.OwnerPassword), http.StatusOK).Data()
		if cleared["products"] != float64(1) || cleared["sales"] != float64(1) || cleared["shops_removed"] != float64(1) || cleared["staff_removed"] != float64(1) {
			t.Fatalf("the clear reported %v", cleared)
		}
		if cleared["kept_shop_id"] != company.ShopId.String() {
			t.Fatalf("kept shop %v, want the owner's current shop %s", cleared["kept_shop_id"], company.ShopId)
		}

		for _, tableName := range cleardata.ClearedTables {
			if remaining := harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM `+tableName+` WHERE company_id = $1`, company.Id); remaining != 0 {
				t.Fatalf("%d rows left in %s", remaining, tableName)
			}
		}
		if harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM users WHERE company_id = $1`, company.Id) != 1 || harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM shops WHERE company_id = $1`, company.Id) != 1 {
			t.Fatal("staff or other shops survived the clear")
		}
		if harness.QueryIntForCompany(company.Id, `SELECT next_receipt_number FROM shops WHERE company_id = $1`, company.Id) != 1 {
			t.Fatal("receipt numbers did not start again")
		}
		if harness.QueryIntForCompany(otherCompany.Id, `SELECT COUNT(*) FROM products WHERE id = $1`, otherProductId) != 1 {
			t.Fatal("another business lost its products")
		}
		if harness.Call(http.MethodGet, "/api/sales", cashierToken, nil).Status != http.StatusUnauthorized {
			t.Fatal("a removed cashier can still sign in with an old session")
		}

		freshProductId := mustCall(t, harness, http.MethodPost, "/api/products", company.OwnerToken, map[string]any{"sku": "SODA", "name": "Soda", "price": 1000, "opening_quantity": 5}, http.StatusCreated).Data()["id"].(string)
		freshSale := mustCall(t, harness, http.MethodPost, "/api/sales", company.OwnerToken, map[string]any{
			"client_ref": "trial-sale-1",
			"items":      []map[string]any{{"product_id": freshProductId, "quantity": 1}},
			"payments":   []map[string]any{{"method": "cash", "amount": 1000}},
		}, http.StatusCreated).Data()
		if freshSale["receipt_number"] == nil {
			t.Fatalf("selling after the clear returned %v", freshSale)
		}
		mustCall(t, harness, http.MethodPost, "/api/accounting/start", company.OwnerToken, map[string]any{"cash_in_drawer": 0}, http.StatusCreated)
	})
}

func TestJournalStaysAppendOnlyOutsideAClear(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Locked Books", "owner@lockedbooks.test")
		mustCall(t, harness, http.MethodPut, "/api/features", company.OwnerToken, map[string]any{"accounting_mode": "simple", "vat_registered": false}, http.StatusOK)
		mustCall(t, harness, http.MethodPost, "/api/accounting/start", company.OwnerToken, map[string]any{"cash_in_drawer": 5000}, http.StatusCreated)

		testContext := context.Background()
		deleteTransaction, beginError := harness.Database.Writer.BeginTx(testContext, nil)
		if beginError != nil {
			t.Fatalf("begin: %v", beginError)
		}
		defer deleteTransaction.Rollback()
		setTenantError := database.SetTenant(testContext, deleteTransaction, harness.Database.IsPostgres(), company.Id)
		if setTenantError != nil {
			t.Fatalf("set tenant: %v", setTenantError)
		}
		_, deleteError := deleteTransaction.ExecContext(testContext, `DELETE FROM journal_lines WHERE company_id = $1`, company.Id)
		if deleteError == nil {
			t.Fatal("journal lines were deleted without a clear")
		}
	})
}

func TestEveryCompanyTableIsClearedOrKept(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		openDatabase := testkit.OpenMigrated(t, engineCase)
		query := `
			SELECT m.name FROM sqlite_master m
			WHERE m.type = 'table' AND EXISTS (SELECT 1 FROM pragma_table_info(m.name) p WHERE p.name = 'company_id')
		`
		if engineCase.Engine == config.EnginePostgres {
			query = `
				SELECT c.table_name FROM information_schema.columns c
				JOIN information_schema.tables t ON t.table_schema = c.table_schema AND t.table_name = c.table_name
				WHERE c.table_schema = 'public' AND c.column_name = 'company_id' AND t.table_type = 'BASE TABLE'
			`
		}

		tableRows, queryError := openDatabase.Reader.QueryContext(context.Background(), query)
		if queryError != nil {
			t.Fatalf("list tables: %v", queryError)
		}
		defer tableRows.Close()

		knownTables := map[string]bool{}
		for _, tableName := range append(append([]string{}, cleardata.ClearedTables...), cleardata.KeptTables...) {
			knownTables[tableName] = true
		}
		unknownTables := []string{}
		for tableRows.Next() {
			tableName := ""
			scanError := tableRows.Scan(&tableName)
			if scanError != nil {
				t.Fatalf("scan table: %v", scanError)
			}
			if !knownTables[tableName] && !isTemporaryRebuild(tableName) {
				unknownTables = append(unknownTables, tableName)
			}
		}
		sort.Strings(unknownTables)
		if len(unknownTables) > 0 {
			t.Fatalf("add these tables to cleardata.ClearedTables or KeptTables: %v", unknownTables)
		}
	})
}

func isTemporaryRebuild(tableName string) bool {
	return tableName == "sale_payments_with_credit" || tableName == "stock_movements_with_order_reasons"
}

func clearBody(businessName string, password string) map[string]any {
	return map[string]any{"business_name": businessName, "password": password}
}

func mustCall(t *testing.T, harness *apptest.Harness, method string, path string, sessionToken string, body map[string]any, wantStatus int) apptest.Response {
	t.Helper()
	called := harness.Call(method, path, sessionToken, body)
	if called.Status != wantStatus {
		t.Fatalf("%s %s returned %d, want %d: %v", method, path, called.Status, wantStatus, called.Body)
	}
	return called
}

func TestClearingStaysOpenWhenTheDesktopTrialHasEnded(t *testing.T) {
	if !licensing.IsSkippedPath("/api/business/clear-data") {
		t.Fatal("a locked desktop cannot clear its test data")
	}
}
