package legacyimport_test

import (
	"crypto/sha256"
	"database/sql"
	"fmt"
	"math"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/config"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/chrisostomemataba/balceinv-api/license"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

const oldSchema = `
CREATE TABLE companies (id integer PRIMARY KEY AUTOINCREMENT, name text NOT NULL, business_type text NOT NULL, phone text DEFAULT null, address text DEFAULT null, tin text DEFAULT null, logo text DEFAULT null, receipt_header text DEFAULT null, receipt_footer text DEFAULT null, primary_color text DEFAULT '#3b82f6', is_seeded numeric DEFAULT false, created_at datetime, updated_at datetime);
CREATE TABLE roles (id integer PRIMARY KEY AUTOINCREMENT, name text NOT NULL);
CREATE UNIQUE INDEX idx_roles_name ON roles (name);
CREATE TABLE permissions (id integer PRIMARY KEY AUTOINCREMENT, name text NOT NULL, resource text NOT NULL, action text NOT NULL, description text DEFAULT null, created_at datetime);
CREATE TABLE suppliers (id integer PRIMARY KEY AUTOINCREMENT, name text NOT NULL, phone text DEFAULT null, notes text DEFAULT null, created_at datetime);
CREATE TABLE users (id integer PRIMARY KEY AUTOINCREMENT, name text NOT NULL, email text NOT NULL, password_hash text NOT NULL, role_id integer NOT NULL, company_id integer NOT NULL, created_at datetime, updated_at datetime);
CREATE UNIQUE INDEX idx_users_email ON users (email);
CREATE TABLE role_permissions (id integer PRIMARY KEY AUTOINCREMENT, role_id integer NOT NULL, permission_id integer NOT NULL, created_at datetime);
CREATE TABLE user_permissions (id integer PRIMARY KEY AUTOINCREMENT, user_id integer NOT NULL, permission_id integer NOT NULL, created_at datetime);
CREATE TABLE settings (id integer PRIMARY KEY AUTOINCREMENT, company_id integer NOT NULL, tax_rate real DEFAULT 18, currency text DEFAULT 'TZS', currency_symbol text DEFAULT 'TZS', date_format text DEFAULT 'DD/MM/YYYY', receipt_number_format text DEFAULT 'SALE-{DATE}-{COUNTER}', efd_enabled numeric DEFAULT false, efd_endpoint text DEFAULT null, efd_api_key text DEFAULT null, efd_last_test_date datetime DEFAULT null, efd_test_status text DEFAULT null, low_stock_threshold integer DEFAULT 5, email_notifications_enabled numeric DEFAULT false, notification_email text DEFAULT null, alert_sound_enabled numeric DEFAULT true, alert_on_low_stock numeric DEFAULT true, alert_on_out_of_stock numeric DEFAULT true, alert_on_dead_stock numeric DEFAULT false, dead_stock_days integer DEFAULT 30, print_receipt_automatically numeric DEFAULT false, show_tax_on_receipt numeric DEFAULT true, show_barcodes_on_receipt numeric DEFAULT false, printer_enabled numeric DEFAULT false, printer_port text DEFAULT '', printer_model text DEFAULT '', printer_baud_rate integer DEFAULT 9600, printer_paper_width integer DEFAULT 80, open_cash_drawer numeric DEFAULT false, updated_by integer DEFAULT null, created_at datetime, updated_at datetime);
CREATE TABLE purchases (id integer PRIMARY KEY AUTOINCREMENT, supplier_id integer DEFAULT null, invoice_number text DEFAULT null, total_cost real NOT NULL, supplier_name text DEFAULT null, user_id integer DEFAULT null, created_at datetime);
CREATE TABLE products (id integer PRIMARY KEY AUTOINCREMENT, name text NOT NULL, sku text NOT NULL, barcode text DEFAULT null, parent_id integer DEFAULT null, variant_label text DEFAULT '', price real NOT NULL, cost_price real NOT NULL, quantity integer NOT NULL DEFAULT 0, min_stock integer DEFAULT 5, wholesale_price real DEFAULT null, wholesale_min integer DEFAULT 10, category text DEFAULT null, unit text DEFAULT 'pcs', pieces_per_unit integer DEFAULT 1, image text DEFAULT null, metadata text DEFAULT null, created_at datetime, updated_at datetime, deleted_at datetime);
CREATE TABLE product_addons (id integer PRIMARY KEY AUTOINCREMENT, product_id integer NOT NULL, name text NOT NULL, price real NOT NULL DEFAULT 0, is_active numeric DEFAULT true, created_at datetime, updated_at datetime);
CREATE TABLE barcodes (id integer PRIMARY KEY AUTOINCREMENT, product_id integer NOT NULL, code text NOT NULL, pack_size integer DEFAULT 1, is_active numeric DEFAULT true, created_at datetime);
CREATE UNIQUE INDEX idx_barcodes_code ON barcodes (code);
CREATE TABLE sales (id integer PRIMARY KEY AUTOINCREMENT, receipt_number text NOT NULL, user_id integer NOT NULL, total_amount real NOT NULL, payment_type text NOT NULL, sale_type text DEFAULT 'retail', tax_amount real DEFAULT 0, amount_paid real DEFAULT 0, created_at datetime);
CREATE UNIQUE INDEX idx_sales_receipt_number ON sales (receipt_number);
CREATE TABLE sale_items (id integer PRIMARY KEY AUTOINCREMENT, sale_id integer NOT NULL, product_id integer NOT NULL, quantity integer NOT NULL, unit_price real NOT NULL, total_price real NOT NULL, is_wholesale numeric DEFAULT false);
CREATE TABLE stock_movements (id integer PRIMARY KEY AUTOINCREMENT, product_id integer NOT NULL, change integer NOT NULL, new_quantity integer NOT NULL, reason text NOT NULL, reference text DEFAULT null, user_id integer DEFAULT null, created_at datetime);
CREATE TABLE discounts (id integer PRIMARY KEY AUTOINCREMENT, name text NOT NULL, product_id integer DEFAULT null, discount_type text NOT NULL, value real NOT NULL, starts_at datetime NOT NULL, ends_at datetime NOT NULL, is_active numeric DEFAULT true, created_by integer NOT NULL, created_at datetime, updated_at datetime);
`

type expectedOldTotals struct {
	liveProducts   int64
	saleLines      int64
	salesValue     int64
	stockValue     int64
	stockOnHand    int64
	aprilSales     int64
	mobileSales    int64
	firstSaleMonth string
}

func isolateLicense(t *testing.T) {
	homeDirectory := t.TempDir()
	t.Setenv("HOME", homeDirectory)
	t.Setenv("APPDATA", homeDirectory)
	t.Setenv("XDG_CONFIG_HOME", homeDirectory)
	previousBaseUrl := license.DjangoBaseURL
	license.DjangoBaseURL = "http://127.0.0.1:9"
	t.Cleanup(func() { license.DjangoBaseURL = previousBaseUrl })
}

func openOld(t *testing.T, oldDatabasePath string) *sql.DB {
	t.Helper()
	oldDatabase, openError := sql.Open("sqlite", oldDatabasePath)
	if openError != nil {
		t.Fatalf("open old database: %v", openError)
	}
	oldDatabase.SetMaxOpenConns(1)
	return oldDatabase
}

func mustExec(t *testing.T, oldDatabase *sql.DB, statement string, arguments ...any) {
	t.Helper()
	_, execError := oldDatabase.Exec(statement, arguments...)
	if execError != nil {
		t.Fatalf("old exec %q: %v", statement, execError)
	}
}

func oldHash(t *testing.T, plainPassword string) string {
	t.Helper()
	hashedBytes, hashError := bcrypt.GenerateFromPassword([]byte(plainPassword), bcrypt.MinCost)
	if hashError != nil {
		t.Fatalf("hash: %v", hashError)
	}
	return string(hashedBytes)
}

func fileDigest(t *testing.T, filePath string) [32]byte {
	t.Helper()
	fileBytes, readError := os.ReadFile(filePath)
	if readError != nil {
		t.Fatalf("read %s: %v", filePath, readError)
	}
	return sha256.Sum256(fileBytes)
}

func buildOldShop(t *testing.T, oldDatabasePath string) expectedOldTotals {
	t.Helper()
	oldDatabase := openOld(t, oldDatabasePath)
	defer oldDatabase.Close()
	mustExec(t, oldDatabase, `PRAGMA journal_mode=WAL`)
	mustExec(t, oldDatabase, `PRAGMA synchronous=OFF`)

	for _, statement := range strings.Split(oldSchema, ";\n") {
		if strings.TrimSpace(statement) != "" {
			mustExec(t, oldDatabase, statement)
		}
	}

	openedAt := time.Date(2025, 12, 1, 8, 0, 0, 0, time.FixedZone("EAT", 3*3600))
	mustExec(t, oldDatabase, `INSERT INTO companies (name, business_type, phone, receipt_header, receipt_footer, created_at, updated_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
		"Duka la Zamani", "hardware", "+255 712 000 000", "Karibu Duka la Zamani", "Asante kwa kununua", openedAt, openedAt)
	mustExec(t, oldDatabase, `INSERT INTO settings (company_id, tax_rate, currency, printer_enabled, printer_port, printer_paper_width, open_cash_drawer, created_at, updated_at) VALUES (1, 18, 'TZS', 1, '/dev/usb/lp0', 58, 1, ?, ?)`, openedAt, openedAt)

	resources := []string{"products", "sales", "users", "roles", "stock_movements", "reports", "settings", "notifications"}
	actions := []string{"view", "create", "edit", "delete"}
	for _, resource := range resources {
		for _, action := range actions {
			mustExec(t, oldDatabase, `INSERT INTO permissions (name, resource, action, created_at) VALUES (?, ?, ?, ?)`, resource+":"+action, resource, action, openedAt)
		}
	}
	mustExec(t, oldDatabase, `INSERT INTO roles (name) VALUES ('Admin'), ('Cashier'), ('Helper')`)
	mustExec(t, oldDatabase, `INSERT INTO role_permissions (role_id, permission_id) SELECT 1, id FROM permissions`)
	mustExec(t, oldDatabase, `INSERT INTO role_permissions (role_id, permission_id) SELECT 2, id FROM permissions WHERE name IN ('sales:create', 'sales:view', 'products:view')`)

	mustExec(t, oldDatabase, `INSERT INTO users (name, email, password_hash, role_id, company_id, created_at, updated_at) VALUES (?, ?, ?, 1, 1, ?, ?)`, "Amina Juma", "amina@duka.test", oldHash(t, "old-admin-password"), openedAt, openedAt)
	mustExec(t, oldDatabase, `INSERT INTO users (name, email, password_hash, role_id, company_id, created_at, updated_at) VALUES (?, ?, ?, 2, 1, ?, ?)`, "Juma Cashier", "juma@duka.test", oldHash(t, "old-cashier-password"), openedAt, openedAt)
	mustExec(t, oldDatabase, `INSERT INTO users (name, email, password_hash, role_id, company_id, created_at, updated_at) VALUES (?, ?, ?, 3, 1, ?, ?)`, "Helper", " AMINA@duka.test", oldHash(t, "old-helper-password"), openedAt, openedAt)

	expected := expectedOldTotals{}
	productPrices := map[int]float64{}
	for productNumber := 1; productNumber <= 30; productNumber++ {
		price := float64(productNumber*1000 + 500)
		costPrice := math.Round(price * 0.7)
		quantity := productNumber % 7
		if productNumber == 5 {
			quantity = -2
		}
		sku := fmt.Sprintf("SKU-%03d", productNumber)
		if productNumber == 12 {
			sku = "SKU-011"
		}
		var parentId any
		variantLabel := ""
		if productNumber == 3 {
			parentId = 2
			variantLabel = "Large"
		}
		var barcode any
		if productNumber == 4 {
			barcode = "4000000000004"
		}
		if productNumber == 8 {
			barcode = "6000000000006"
		}
		var image any
		if productNumber == 1 {
			image = "uploads/product-1.png"
		}
		var deletedAt any
		if productNumber == 30 {
			deletedAt = openedAt
		}
		category := "Tools"
		if productNumber%2 == 0 {
			category = "Paint"
		}
		mustExec(t, oldDatabase, `
			INSERT INTO products (name, sku, barcode, parent_id, variant_label, price, cost_price, quantity, min_stock, category, image, metadata, created_at, updated_at, deleted_at)
			VALUES (?, ?, ?, ?, ?, ?, ?, ?, 3, ?, ?, '{"brand":"Old"}', ?, ?, ?)`,
			fmt.Sprintf("Product %d", productNumber), sku, barcode, parentId, variantLabel, price, costPrice, quantity, category, image, openedAt, openedAt, deletedAt)
		productPrices[productNumber] = price

		if productNumber != 30 {
			expected.liveProducts++
			expected.stockOnHand += int64(max(quantity, 0))
			expected.stockValue += int64(max(quantity, 0)) * int64(costPrice)
			mustExec(t, oldDatabase, `INSERT INTO stock_movements (product_id, change, new_quantity, reason, created_at) VALUES (?, ?, ?, 'adjust', ?)`, productNumber, quantity, quantity, openedAt)
		}
	}
	mustExec(t, oldDatabase, `INSERT INTO barcodes (product_id, code, pack_size) VALUES (6, '6000000000006', 1), (6, '6000000000060', 12)`)
	mustExec(t, oldDatabase, `INSERT INTO product_addons (product_id, name, price) VALUES (1, 'Extra', 200), (1, 'Gift wrap', 100)`)
	mustExec(t, oldDatabase, `INSERT INTO suppliers (name, phone, notes, created_at) VALUES ('Kariakoo Traders', '+255 713 111 111', 'Pays on Fridays', ?), ('Mwanza Paints', NULL, NULL, ?), ('Arusha Tools', '+255 714 222 222', NULL, ?)`, openedAt, openedAt, openedAt)
	mustExec(t, oldDatabase, `INSERT INTO purchases (supplier_id, total_cost, created_at) VALUES (1, 50000, ?)`, openedAt)
	mustExec(t, oldDatabase, `INSERT INTO discounts (name, product_id, discount_type, value, starts_at, ends_at, created_by) VALUES ('Paint week', 2, 'percent', 10, ?, ?, 1)`, openedAt, openedAt.AddDate(1, 0, 0))
	mustExec(t, oldDatabase, `INSERT INTO discounts (name, product_id, discount_type, value, starts_at, ends_at, created_by) VALUES ('Broken promo', 2, 'fixed', 500, ?, ?, 1)`, openedAt, openedAt.AddDate(0, 0, -1))

	firstSaleAt := time.Date(2026, 4, 1, 9, 0, 0, 0, time.UTC)
	for saleNumber := 1; saleNumber <= 100; saleNumber++ {
		soldAt := firstSaleAt.Add(time.Duration(saleNumber-1) * 36 * time.Hour)
		lineCount := 1 + saleNumber%3
		saleTotal := 0.0
		type saleLine struct {
			productId int
			quantity  int
			unitPrice float64
		}
		saleLines := []saleLine{}
		for lineNumber := 0; lineNumber < lineCount; lineNumber++ {
			productId := (saleNumber+lineNumber*7)%29 + 1
			saleLines = append(saleLines, saleLine{productId: productId, quantity: 1 + lineNumber, unitPrice: productPrices[productId]})
		}
		if saleNumber == 50 {
			saleLines = append(saleLines, saleLine{productId: 30, quantity: 1, unitPrice: productPrices[30]})
		}
		if saleNumber == 60 {
			saleLines = append(saleLines, saleLine{productId: 999, quantity: 2, unitPrice: 2500})
		}
		if saleNumber == 70 {
			saleLines = append(saleLines, saleLine{productId: 1, quantity: 0, unitPrice: productPrices[1]})
		}
		for _, line := range saleLines {
			saleTotal += line.unitPrice * float64(line.quantity)
			if line.quantity > 0 {
				expected.saleLines++
			}
		}

		paymentType := []string{"cash", "card", "mobile"}[saleNumber%3]
		amountPaid := 0.0
		if paymentType == "cash" {
			amountPaid = math.Ceil(saleTotal/1000)*1000 + 1000
		}
		if paymentType == "mobile" {
			expected.mobileSales++
		}
		if soldAt.Before(time.Date(2026, 5, 1, 0, 0, 0, 0, time.UTC)) {
			expected.aprilSales++
		}
		expected.salesValue += int64(math.Round(saleTotal))

		saleResult, insertError := oldDatabase.Exec(`INSERT INTO sales (receipt_number, user_id, total_amount, payment_type, tax_amount, amount_paid, created_at) VALUES (?, ?, ?, ?, ?, ?, ?)`,
			fmt.Sprintf("SALE-%s-%04d", soldAt.Format("20060102"), saleNumber), 1+saleNumber%2, saleTotal, paymentType, saleTotal*18/118, amountPaid, soldAt)
		if insertError != nil {
			t.Fatalf("insert old sale: %v", insertError)
		}
		saleId, _ := saleResult.LastInsertId()
		for _, line := range saleLines {
			mustExec(t, oldDatabase, `INSERT INTO sale_items (sale_id, product_id, quantity, unit_price, total_price) VALUES (?, ?, ?, ?, ?)`, saleId, line.productId, line.quantity, line.unitPrice, line.unitPrice*float64(line.quantity))
		}
	}
	expected.firstSaleMonth = "2026-04"

	return expected
}

func noteCount(resultData map[string]any, noteCode string) float64 {
	notes, _ := resultData["notes"].([]any)
	for _, note := range notes {
		noteFields := note.(map[string]any)
		if noteFields["code"] == noteCode {
			return noteFields["count"].(float64)
		}
	}
	return 0
}

func hasPermission(meData map[string]any, permissionId string) bool {
	permissions, _ := meData["permissions"].([]any)
	for _, permission := range permissions {
		if permission.(map[string]any)["id"] == permissionId {
			return true
		}
	}
	return false
}

func TestBringingOverTheOldAppsData(t *testing.T) {
	isolateLicense(t)
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		if engineCase.Engine != config.EngineSqlite {
			return
		}
		harness := apptest.StartDesktop(t, engineCase)
		oldDatabasePath := filepath.Join(harness.Config.DataDirectory, "balce.db")
		expected := buildOldShop(t, oldDatabasePath)
		digestBefore := fileDigest(t, oldDatabasePath)

		preview := harness.Call(http.MethodGet, "/api/setup/import-old/preview", "", nil)
		if preview.Status != http.StatusOK {
			t.Fatalf("preview returned %d %v", preview.Status, preview.Body)
		}
		previewData := preview.Data()
		previewCounts := previewData["counts"].(map[string]any)
		if previewData["business_name"] != "Duka la Zamani" || previewCounts["products"] != float64(expected.liveProducts) ||
			previewCounts["users"] != float64(3) || previewCounts["sales"] != float64(100) ||
			previewCounts["sale_lines"] != float64(expected.saleLines) || previewCounts["suppliers"] != float64(3) {
			t.Fatalf("preview showed %v", previewData)
		}
		ownerChoices := previewData["owner_choices"].([]any)
		if len(ownerChoices) != 1 || ownerChoices[0].(map[string]any)["email"] != "amina@duka.test" || previewData["passwords_kept"] != true {
			t.Fatalf("preview owner choices were %v", previewData)
		}
		if !strings.HasPrefix(previewData["first_sale_at"].(string), "2026-04-01") || !strings.HasPrefix(previewData["last_sale_at"].(string), "2026-08-") {
			t.Fatalf("preview sale dates were %v to %v", previewData["first_sale_at"], previewData["last_sale_at"])
		}

		unknownOwner := harness.Call(http.MethodPost, "/api/setup/import-old", "", map[string]any{"owner_id": 2})
		if unknownOwner.Status != http.StatusBadRequest || unknownOwner.Code() != "unknown_owner" {
			t.Fatalf("a non-admin owner returned %d %v", unknownOwner.Status, unknownOwner.Body)
		}

		_, triggerError := harness.Database.Writer.Exec(`
			CREATE TRIGGER lose_one_product AFTER INSERT ON products WHEN NEW.sku = 'SKU-009'
			BEGIN UPDATE products SET is_active = FALSE WHERE id = NEW.id; END`)
		if triggerError != nil {
			t.Fatalf("create trigger: %v", triggerError)
		}
		mismatch := harness.Call(http.MethodPost, "/api/setup/import-old", "", map[string]any{})
		if mismatch.Status != http.StatusUnprocessableEntity || mismatch.Code() != "import_mismatch" || mismatch.Data()["matched"] != false {
			t.Fatalf("a lost product returned %d %v", mismatch.Status, mismatch.Body)
		}
		if harness.Call(http.MethodGet, "/api/setup/status", "", nil).Data()["configured"] != false {
			t.Fatal("a mismatched import was kept")
		}
		harness.Database.Writer.Exec(`DROP TRIGGER lose_one_product`)

		supplierCount := int64(0)
		harness.Database.Writer.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'suppliers'`).Scan(&supplierCount)
		if supplierCount == 0 {
			_, createError := harness.Database.Writer.Exec(`CREATE TABLE suppliers (id TEXT PRIMARY KEY, company_id TEXT NOT NULL, name TEXT NOT NULL, phone TEXT NULL, notes TEXT NULL, is_active BOOLEAN NOT NULL DEFAULT TRUE, created_at DATETIME NOT NULL, updated_at DATETIME NOT NULL)`)
			if createError != nil {
				t.Fatalf("create suppliers table: %v", createError)
			}
		}

		imported := harness.Call(http.MethodPost, "/api/setup/import-old", "", map[string]any{"business_name": "Duka Jipya", "owner_id": 1})
		if imported.Status != http.StatusCreated {
			t.Fatalf("import returned %d %v", imported.Status, imported.Body)
		}
		importData := imported.Data()
		selfCheck := importData["self_check"].(map[string]any)
		if selfCheck["matched"] != true {
			t.Fatalf("self-check did not match: %v", selfCheck)
		}
		expectedChecks := map[string]int64{
			"products":    expected.liveProducts,
			"users":       3,
			"sales":       100,
			"sale_lines":  expected.saleLines,
			"sales_value": expected.salesValue,
			"stock_value": expected.stockValue,
		}
		for checkName, expectedValue := range expectedChecks {
			check := selfCheck[checkName].(map[string]any)
			if check["old"] != float64(expectedValue) || check["new"] != float64(expectedValue) {
				t.Fatalf("self-check %s was %v, want %d", checkName, check, expectedValue)
			}
		}
		supplierCheck, _ := selfCheck["suppliers"].(map[string]any)
		if supplierCount == 0 && (supplierCheck == nil || supplierCheck["new"] != float64(3)) {
			t.Fatalf("suppliers were not brought over: %v", selfCheck["suppliers"])
		}
		if importData["passwords_kept"] != true || len(importData["temporary_passwords"].([]any)) != 0 {
			t.Fatalf("bcrypt passwords were not kept: %v", importData)
		}

		renamedSkus := importData["renamed_skus"].([]any)
		if len(renamedSkus) != 1 || renamedSkus[0].(map[string]any)["new"] != "SKU-011-2" {
			t.Fatalf("renamed skus were %v", renamedSkus)
		}
		renamedBarcodes := importData["renamed_barcodes"].([]any)
		if len(renamedBarcodes) != 1 || renamedBarcodes[0].(map[string]any)["new"] != "6000000000006-2" {
			t.Fatalf("renamed barcodes were %v", renamedBarcodes)
		}
		changedEmails := importData["changed_emails"].([]any)
		if len(changedEmails) != 1 || changedEmails[0].(map[string]any)["new"] != "amina+3@duka.test" {
			t.Fatalf("changed emails were %v", changedEmails)
		}
		expectedNotes := map[string]float64{
			"history_products_archived":  2,
			"discounts_skipped":          1,
			"product_images_not_copied":  1,
			"negative_stock_set_to_zero": 1,
			"purchases_not_imported":     1,
		}
		for noteCode, expectedCount := range expectedNotes {
			if noteCount(importData, noteCode) != expectedCount {
				t.Fatalf("note %s was %v, want %v (notes %v)", noteCode, noteCount(importData, noteCode), expectedCount, importData["notes"])
			}
		}

		trialState, loadError := license.LoadLicenseState()
		if loadError != nil || !trialState.IsTrial {
			t.Fatalf("the import did not start a trial: %v %v", trialState, loadError)
		}

		ownerToken := harness.MustLogin("amina@duka.test", "old-admin-password")
		ownerMe := harness.Call(http.MethodGet, "/api/auth/me", ownerToken, nil).Data()
		ownerPermissions, _ := ownerMe["permissions"].([]any)
		if ownerMe["is_owner"] != true || ownerMe["company_name"] != "Duka Jipya" || len(ownerPermissions) != 61 {
			t.Fatalf("owner after import: %v", ownerMe)
		}
		cashierToken := harness.MustLogin("JUMA@duka.test", "old-cashier-password")
		cashierMe := harness.Call(http.MethodGet, "/api/auth/me", cashierToken, nil).Data()
		if cashierMe["is_owner"] != false || !hasPermission(cashierMe, "sales:create") || hasPermission(cashierMe, "users:edit") {
			t.Fatalf("cashier after import: %v", cashierMe)
		}
		helperToken := harness.MustLogin("amina+3@duka.test", "old-helper-password")
		helperMe := harness.Call(http.MethodGet, "/api/auth/me", helperToken, nil).Data()
		helperPermissions, _ := helperMe["permissions"].([]any)
		if len(helperPermissions) != 2 || !hasPermission(helperMe, "sales:create") || !hasPermission(helperMe, "products:view") {
			t.Fatalf("a user whose old role had no permissions got %v", helperMe["permissions"])
		}

		sales := harness.Call(http.MethodGet, "/api/sales?limit=5", ownerToken, nil)
		if sales.Status != http.StatusOK || sales.Data()["total"] != float64(100) {
			t.Fatalf("sales list after import returned %d %v", sales.Status, sales.Data())
		}

		importedCompanyId := uuid.MustParse(ownerMe["company_id"].(string))
		queryCount := func(query string) int64 {
			return harness.QueryIntForCompany(importedCompanyId, query, importedCompanyId)
		}
		if onHand := queryCount(`SELECT COALESCE(SUM(quantity), 0) FROM shop_stock WHERE company_id = $1`); onHand != expected.stockOnHand {
			t.Fatalf("stock on hand is %d, the old app had %d", onHand, expected.stockOnHand)
		}
		if openings := queryCount(`SELECT COUNT(*) FROM stock_movements WHERE company_id = $1 AND reason <> 'opening'`); openings != 0 {
			t.Fatalf("%d stock movements are not opening stock", openings)
		}
		if queryCount(`SELECT COUNT(*) FROM products WHERE company_id = $1 AND sku = 'SKU-030' AND is_active`) != 0 ||
			queryCount(`SELECT COUNT(*) FROM products WHERE company_id = $1 AND sku = 'SKU-030'`) != 1 ||
			queryCount(`SELECT COUNT(*) FROM shop_stock ss JOIN products p ON p.id = ss.product_id WHERE ss.company_id = $1 AND p.sku = 'SKU-030'`) != 0 {
			t.Fatal("the soft-deleted product came back as a live product")
		}
		if queryCount(`SELECT COUNT(*) FROM products c JOIN products p ON p.id = c.parent_id WHERE c.company_id = $1 AND c.sku = 'SKU-003' AND p.sku = 'SKU-002'`) != 1 {
			t.Fatal("the variant lost its parent product")
		}
		if queryCount(`SELECT COUNT(*) FROM product_addons WHERE company_id = $1`) != 2 || queryCount(`SELECT COUNT(*) FROM barcodes WHERE company_id = $1`) != 4 {
			t.Fatal("add-ons or barcodes were not brought over")
		}
		if queryCount(`SELECT COUNT(*) FROM sales WHERE company_id = $1 AND receipt_number LIKE 'OLD-SALE-%'`) != 100 {
			t.Fatal("old receipt numbers were not prefixed")
		}
		if aprilSales := queryCount(`SELECT COUNT(*) FROM sales WHERE company_id = $1 AND substr(created_at, 1, 7) = '` + expected.firstSaleMonth + `'`); aprilSales != expected.aprilSales {
			t.Fatalf("%d sales kept their April date, want %d", aprilSales, expected.aprilSales)
		}
		if mobileSales := queryCount(`SELECT COUNT(*) FROM sale_payments WHERE company_id = $1 AND method = 'mobile'`); mobileSales != expected.mobileSales {
			t.Fatalf("%d mobile payments, want %d", mobileSales, expected.mobileSales)
		}
		if queryCount(`SELECT COUNT(*) FROM sale_items WHERE company_id = $1 AND unit_cost = 0`) != 1 {
			t.Fatal("unit costs were not snapshotted from the product cost")
		}
		if queryCount(`SELECT printer_paper_width FROM settings WHERE company_id = $1`) != 58 || queryCount(`SELECT tax_rate_basis_points FROM settings WHERE company_id = $1`) != 1800 {
			t.Fatal("printer or tax settings were not brought over")
		}
		if queryCount(`SELECT COUNT(*) FROM companies WHERE id = $1 AND receipt_footer = 'Asante kwa kununua'`) != 1 {
			t.Fatal("the receipt footer was not brought over")
		}
		if queryCount(`SELECT COUNT(*) FROM discounts WHERE company_id = $1 AND kind = 'percent' AND value = 1000`) != 1 {
			t.Fatal("the valid discount was not brought over")
		}

		again := harness.Call(http.MethodPost, "/api/setup/import-old", "", map[string]any{})
		if again.Status != http.StatusConflict || again.Code() != "already_configured" {
			t.Fatalf("a second import returned %d %v", again.Status, again.Body)
		}
		if previewAgain := harness.Call(http.MethodGet, "/api/setup/import-old/preview", "", nil); previewAgain.Status != http.StatusConflict {
			t.Fatalf("preview after setup returned %d", previewAgain.Status)
		}

		if fileDigest(t, oldDatabasePath) != digestBefore {
			t.Fatal("the old app's database file was changed")
		}
	})
}

func TestOldDataImportRefusesWhatItCannotRead(t *testing.T) {
	isolateLicense(t)
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		cloudHarness := apptest.Start(t, engineCase)
		if cloudHarness.Call(http.MethodGet, "/api/setup/import-old/preview", "", nil).Status != http.StatusNotFound ||
			cloudHarness.Call(http.MethodPost, "/api/setup/import-old", "", map[string]any{}).Status != http.StatusNotFound {
			t.Fatal("the old data import exists outside the desktop app")
		}
		if engineCase.Engine != config.EngineSqlite {
			return
		}

		harness := apptest.StartDesktop(t, engineCase)
		oldDatabasePath := filepath.Join(harness.Config.DataDirectory, "balce.db")

		missing := harness.Call(http.MethodGet, "/api/setup/import-old/preview", "", nil)
		if missing.Status != http.StatusNotFound || missing.Code() != "old_data_not_found" {
			t.Fatalf("preview without old data returned %d %v", missing.Status, missing.Body)
		}

		os.WriteFile(oldDatabasePath, []byte("this is not a database, just a damaged file that happens to be here"), 0o600)
		damagedPreview := harness.Call(http.MethodGet, "/api/setup/import-old/preview", "", nil)
		damagedImport := harness.Call(http.MethodPost, "/api/setup/import-old", "", map[string]any{})
		if damagedPreview.Status != http.StatusBadRequest || damagedPreview.Code() != "old_data_unreadable" ||
			damagedImport.Status != http.StatusBadRequest || damagedImport.Code() != "old_data_unreadable" {
			t.Fatalf("a damaged file returned %d %v and %d %v", damagedPreview.Status, damagedPreview.Body, damagedImport.Status, damagedImport.Body)
		}
		os.Remove(oldDatabasePath)

		oldDatabase := openOld(t, oldDatabasePath)
		mustExec(t, oldDatabase, `CREATE TABLE companies (id integer PRIMARY KEY AUTOINCREMENT, name text NOT NULL, business_type text NOT NULL)`)
		mustExec(t, oldDatabase, `CREATE TABLE roles (id integer PRIMARY KEY AUTOINCREMENT, name text NOT NULL)`)
		mustExec(t, oldDatabase, `CREATE TABLE users (id integer PRIMARY KEY AUTOINCREMENT, name text NOT NULL, email text NOT NULL, password_hash text NOT NULL, role_id integer NOT NULL)`)
		mustExec(t, oldDatabase, `CREATE TABLE products (id integer PRIMARY KEY AUTOINCREMENT, name text NOT NULL, sku text NOT NULL, price real NOT NULL, cost_price real NOT NULL, quantity integer NOT NULL)`)
		mustExec(t, oldDatabase, `CREATE TABLE suppliers (id integer PRIMARY KEY AUTOINCREMENT, name text NOT NULL)`)
		mustExec(t, oldDatabase, `INSERT INTO companies (name, business_type) VALUES ('Duka la Zamani Sana', 'retail')`)
		mustExec(t, oldDatabase, `INSERT INTO roles (name) VALUES ('Manager')`)
		mustExec(t, oldDatabase, `INSERT INTO users (name, email, password_hash, role_id) VALUES ('Baraka', 'baraka@duka.test', 'not-a-bcrypt-hash', 1)`)
		mustExec(t, oldDatabase, `INSERT INTO products (name, sku, price, cost_price, quantity) VALUES ('Soap', '', 1500, 1000, 12), ('Salt', 'SALT', 800, 500, 4)`)
		mustExec(t, oldDatabase, `INSERT INTO suppliers (name) VALUES ('Kariakoo Traders'), ('Mwanza Paints')`)
		oldDatabase.Close()

		supplierTableCount := int64(0)
		harness.Database.Writer.QueryRow(`SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name = 'suppliers'`).Scan(&supplierTableCount)

		imported := harness.Call(http.MethodPost, "/api/setup/import-old", "", map[string]any{})
		if imported.Status != http.StatusCreated {
			t.Fatalf("an older file returned %d %v", imported.Status, imported.Body)
		}
		importData := imported.Data()
		if importData["self_check"].(map[string]any)["matched"] != true || importData["passwords_kept"] != false {
			t.Fatalf("older file import result: %v", importData)
		}
		if supplierTableCount == 0 && noteCount(importData, "suppliers_not_imported") != 2 {
			t.Fatalf("missing suppliers table was not noted: %v", importData["notes"])
		}
		temporaryPasswords := importData["temporary_passwords"].([]any)
		if len(temporaryPasswords) != 1 {
			t.Fatalf("temporary passwords were %v", temporaryPasswords)
		}
		temporaryPassword := temporaryPasswords[0].(map[string]any)["password"].(string)
		ownerLogin := harness.Send(http.MethodPost, "/api/auth/login", "", map[string]any{"email": "baraka@duka.test", "password": temporaryPassword}, map[string]string{"X-Balce-Client": "desktop"})
		loggedInUser, _ := ownerLogin.Data()["user"].(map[string]any)
		if ownerLogin.Status != http.StatusOK || loggedInUser["must_change_password"] != true {
			t.Fatalf("temporary password login returned %d %v", ownerLogin.Status, ownerLogin.Body)
		}
		renamedSkus := importData["renamed_skus"].([]any)
		if len(renamedSkus) != 1 || renamedSkus[0].(map[string]any)["new"] != "OLD-1" {
			t.Fatalf("an empty sku was not given one: %v", renamedSkus)
		}
	})
}
