package businessmove_test

import (
	"bytes"
	"context"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"path/filepath"
	"reflect"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/config"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
)

func pictureBytes(t *testing.T, shade uint8) []byte {
	t.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, 8, 8))
	picture.Set(2, 2, color.RGBA{R: shade, G: 90, B: 40, A: 255})
	encoded := &bytes.Buffer{}
	encodeError := png.Encode(encoded, picture)
	if encodeError != nil {
		t.Fatalf("encode picture: %v", encodeError)
	}
	return encoded.Bytes()
}

func mustCall(t *testing.T, harness *apptest.Harness, method string, path string, token string, body any, wantStatus int) apptest.Response {
	t.Helper()
	called := harness.Call(method, path, token, body)
	if called.Status != wantStatus {
		t.Fatalf("%s %s returned %d, want %d: %v", method, path, called.Status, wantStatus, called.Body)
	}
	return called
}

func buildDesktopBusiness(t *testing.T) (*apptest.Harness, apptest.Company) {
	t.Helper()
	desktopCase := testkit.EngineCase{Engine: config.EngineSqlite, SqlitePath: filepath.Join(t.TempDir(), "Application Support", "balce.sqlite")}
	desktop := apptest.StartDesktop(t, desktopCase)
	company := desktop.CreateCompany("Duka la Kuhamia", "owner@kuhamia.test")
	token := company.OwnerToken

	mustCall(t, desktop, http.MethodPut, "/api/features", token, map[string]any{"accounting_mode": "simple", "customers_enabled": true, "credit_sales_enabled": true}, http.StatusOK)
	if logo := desktop.Upload("/api/settings/upload-logo", token, "file", "logo.png", pictureBytes(t, 10)); logo.Status >= http.StatusBadRequest {
		t.Fatalf("logo upload returned %d: %v", logo.Status, logo.Body)
	}

	parentId := mustCall(t, desktop, http.MethodPost, "/api/products", token, map[string]any{"sku": "SODA", "name": "Soda", "price": 1000, "cost_price": 600, "opening_quantity": 50}, http.StatusCreated).Data()["id"].(string)
	mustCall(t, desktop, http.MethodPost, "/api/products", token, map[string]any{"sku": "SODA-L", "name": "Soda", "variant_label": "Large", "parent_id": parentId, "price": 1500, "cost_price": 900, "opening_quantity": 20}, http.StatusCreated)
	if photo := desktop.Upload("/api/products/"+parentId+"/image", token, "image", "soda.png", pictureBytes(t, 200)); photo.Status >= http.StatusBadRequest {
		t.Fatalf("product photo returned %d: %v", photo.Status, photo.Body)
	}

	mustCall(t, desktop, http.MethodPost, "/api/accounting/start", token, map[string]any{"cash_in_drawer": 50000}, http.StatusCreated)
	mustCall(t, desktop, http.MethodPost, "/api/sales", token, map[string]any{
		"client_ref": "move-sale-1", "items": []map[string]any{{"product_id": parentId, "quantity": 3}},
		"payments": []map[string]any{{"method": "cash", "amount": 3000}},
	}, http.StatusCreated)
	customerId := mustCall(t, desktop, http.MethodPost, "/api/customers", token, map[string]any{"name": "Mama Asha", "phone": "0713000111"}, http.StatusCreated).Data()["id"].(string)
	mustCall(t, desktop, http.MethodPost, "/api/sales", token, map[string]any{
		"client_ref": "move-sale-2", "customer_id": customerId, "items": []map[string]any{{"product_id": parentId, "quantity": 2}},
		"payments": []map[string]any{{"method": "credit", "amount": 2000}},
	}, http.StatusCreated)

	accounts := mustCall(t, desktop, http.MethodGet, "/api/accounting/accounts", token, nil, http.StatusOK).Body["data"].([]any)
	rentAccountId := ""
	for _, account := range accounts {
		accountFields := account.(map[string]any)
		if accountFields["system_key"] == "rent" {
			rentAccountId = accountFields["id"].(string)
		}
	}
	expense := mustCall(t, desktop, http.MethodPost, "/api/accounting/money", token, map[string]any{
		"client_ref": "move-rent-1", "kind": "expense", "amount": 20000, "money_account": "cash", "expense_account_id": rentAccountId,
	}, http.StatusCreated)
	mustCall(t, desktop, http.MethodPost, "/api/accounting/entries/"+expense.Data()["id"].(string)+"/reverse", token, map[string]any{"reason": "Typed twice"}, http.StatusCreated)
	return desktop, company
}

func countIn(t *testing.T, harness *apptest.Harness, query string, arguments ...any) int64 {
	t.Helper()
	var count int64
	scanError := harness.Database.Reader.QueryRowContext(context.Background(), query, arguments...).Scan(&count)
	if scanError != nil {
		t.Fatalf("count %q: %v", query, scanError)
	}
	return count
}

func TestADesktopBusinessMovesOnlineWithEverything(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		if engineCase.Engine != config.EnginePostgres {
			t.Skip("the online server runs on Postgres")
		}
		desktop, company := buildDesktopBusiness(t)

		cashierToken := desktop.CreateStaff(company, "cashier@kuhamia.test", []string{"sales:create"}, nil)
		movingFile := desktop.Call(http.MethodPost, "/api/move-to-web/file", company.OwnerToken, nil)
		if movingFile.Status != http.StatusOK || len(movingFile.Raw) < 1000 {
			t.Fatalf("moving file returned %d (%d bytes): %v", movingFile.Status, len(movingFile.Raw), movingFile.Body)
		}
		if desktop.Call(http.MethodPost, "/api/move-to-web/file", cashierToken, nil).Status != http.StatusForbidden {
			t.Fatal("a cashier could download the whole business")
		}

		cloud := apptest.Start(t, engineCase)
		moved := cloud.Upload("/api/setup/move-from-desktop", "", "file", "duka.balce", movingFile.Raw)
		if moved.Status != http.StatusOK {
			t.Fatalf("move returned %d: %v", moved.Status, moved.Body)
		}
		movedData := moved.Data()
		if movedData["business_name"] != "Duka la Kuhamia" || movedData["owner_email"] != "owner@kuhamia.test" || movedData["media_count"] != float64(2) {
			t.Fatalf("move result = %v", movedData)
		}

		tableRows, tablesError := cloud.Database.Reader.QueryContext(context.Background(), `
			SELECT table_name FROM information_schema.columns
			WHERE table_schema = 'public' AND column_name = 'company_id'
			  AND table_name NOT IN ('sessions', 'company_subscriptions', 'support_messages')
		`)
		if tablesError != nil {
			t.Fatalf("list tables: %v", tablesError)
		}
		tableNames := []string{}
		for tableRows.Next() {
			var tableName string
			tableRows.Scan(&tableName)
			tableNames = append(tableNames, tableName)
		}
		tableRows.Close()
		movedRowCount := int64(0)
		for _, tableName := range tableNames {
			desktopCount := countIn(t, desktop, `SELECT COUNT(*) FROM "`+tableName+`" WHERE company_id = $1`, company.Id.String())
			cloudCount := cloud.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM "`+tableName+`" WHERE company_id = $1`, company.Id)
			if desktopCount != cloudCount {
				t.Fatalf("%s has %d rows on the desktop and %d online", tableName, desktopCount, cloudCount)
			}
			movedRowCount += cloudCount
		}
		if movedRowCount < 50 {
			t.Fatalf("only %d rows moved; the test business is too small to prove anything", movedRowCount)
		}

		ownerToken := cloud.MustLogin("owner@kuhamia.test", "owner-password-123")
		me := mustCall(t, cloud, http.MethodGet, "/api/auth/me", ownerToken, nil, http.StatusOK).Data()
		logoUrl, _ := me["branding"].(map[string]any)["logo_url"].(string)
		if me["company_name"] != "Duka la Kuhamia" || logoUrl == "" {
			t.Fatalf("signed in to %v with logo %q", me["company_name"], logoUrl)
		}
		if logo := cloud.Call(http.MethodGet, logoUrl, "", nil); logo.Status != http.StatusOK || len(logo.Raw) == 0 {
			t.Fatalf("the moved logo returned %d", logo.Status)
		}
		cloud.MustLogin("cashier@kuhamia.test", "staff-password-123")

		desktopSummary := mustCall(t, desktop, http.MethodGet, "/api/reports/summary?shop=all", company.OwnerToken, nil, http.StatusOK).Data()
		cloudSummary := mustCall(t, cloud, http.MethodGet, "/api/reports/summary?shop=all", ownerToken, nil, http.StatusOK).Data()
		if desktopSummary["total"] != cloudSummary["total"] || desktopSummary["gross_profit"] != cloudSummary["gross_profit"] || cloudSummary["sale_count"] != float64(2) {
			t.Fatalf("sales summary desktop %v, online %v", desktopSummary, cloudSummary)
		}
		desktopBooks := mustCall(t, desktop, http.MethodGet, "/api/accounting/overview", company.OwnerToken, nil, http.StatusOK).Data()
		cloudBooks := mustCall(t, cloud, http.MethodGet, "/api/accounting/overview", ownerToken, nil, http.StatusOK).Data()
		for _, figure := range []string{"money_in", "money_out", "profit", "customers_owe", "balances"} {
			if !reflect.DeepEqual(desktopBooks[figure], cloudBooks[figure]) {
				t.Fatalf("books %s: desktop %v, online %v", figure, desktopBooks[figure], cloudBooks[figure])
			}
		}
		if cloudBooks["customers_owe"] != float64(2000) {
			t.Fatalf("the credit sale did not move: customers owe %v", cloudBooks["customers_owe"])
		}
		subscription := mustCall(t, cloud, http.MethodGet, "/api/license/status", ownerToken, nil, http.StatusOK).Data()
		if subscription["is_trial"] != true || subscription["days_remaining"] != float64(14) {
			t.Fatalf("the moved business is not on a fresh trial: %v", subscription)
		}
		mustCall(t, cloud, http.MethodPost, "/api/sales", ownerToken, map[string]any{
			"client_ref": "after-move-1", "items": []map[string]any{{"product_id": cloudProductId(t, cloud, ownerToken), "quantity": 1}},
			"payments": []map[string]any{{"method": "cash", "amount": 1000}},
		}, http.StatusCreated)

		again := cloud.Upload("/api/setup/move-from-desktop", "", "file", "duka.balce", movingFile.Raw)
		if again.Status != http.StatusConflict || again.Body["code"] != "move_already_done" {
			t.Fatalf("moving twice returned %d %v", again.Status, again.Body)
		}

		secondDesktop := apptest.StartDesktop(t, testkit.EngineCase{Engine: config.EngineSqlite, SqlitePath: filepath.Join(t.TempDir(), "second", "balce.sqlite")})
		secondCompany := secondDesktop.CreateCompany("Duka Jingine", "owner@kuhamia.test")
		secondFile := mustCall(t, secondDesktop, http.MethodPost, "/api/move-to-web/file", secondCompany.OwnerToken, nil, http.StatusOK)
		clash := cloud.Upload("/api/setup/move-from-desktop", "", "file", "jingine.balce", secondFile.Raw)
		if clash.Status != http.StatusConflict || clash.Body["code"] != "move_email_taken" {
			t.Fatalf("an email clash returned %d %v", clash.Status, clash.Body)
		}
		if cloud.QueryIntForCompany(secondCompany.Id, `SELECT COUNT(*) FROM roles WHERE company_id = $1`, secondCompany.Id) != 0 {
			t.Fatal("a refused move left rows behind")
		}

		for fileName, junk := range map[string][]byte{"notes.balce": []byte("not a zip"), "empty.balce": {}} {
			refused := cloud.Upload("/api/setup/move-from-desktop", "", "file", fileName, junk)
			if refused.Status != http.StatusBadRequest {
				t.Fatalf("%s returned %d %v", fileName, refused.Status, refused.Body)
			}
		}
	})
}

func cloudProductId(t *testing.T, cloud *apptest.Harness, token string) string {
	t.Helper()
	products := mustCall(t, cloud, http.MethodGet, "/api/products?search=SODA", token, nil, http.StatusOK)
	for _, item := range products.Items() {
		product := item.(map[string]any)
		if product["sku"] == "SODA" {
			return product["id"].(string)
		}
	}
	t.Fatal("the moved product is not listed online")
	return ""
}
