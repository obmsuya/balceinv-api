package database_test

import (
	"context"
	"testing"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/google/uuid"
)

func insertCompanyWithRole(t *testing.T, openDatabase *database.Database, roleName string, isOwner bool) (uuid.UUID, uuid.UUID) {
	t.Helper()
	testContext := context.Background()

	companyId := uuid.Must(uuid.NewV7())
	_, insertCompanyError := openDatabase.Writer.ExecContext(testContext,
		`INSERT INTO companies (id, name) VALUES ($1, $2)`, companyId, "Company "+companyId.String()[:8])
	if insertCompanyError != nil {
		t.Fatalf("insert company: %v", insertCompanyError)
	}

	roleId := uuid.Must(uuid.NewV7())
	_, insertRoleError := openDatabase.Writer.ExecContext(testContext,
		`INSERT INTO roles (id, company_id, name, is_owner) VALUES ($1, $2, $3, $4)`, roleId, companyId, roleName, isOwner)
	if insertRoleError != nil {
		t.Fatalf("insert role: %v", insertRoleError)
	}

	return companyId, roleId
}

func TestSchemaEnforcesTenantBoundaries(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		openDatabase := testkit.OpenMigrated(t, engineCase)
		testContext := context.Background()

		permissionCount := 0
		countError := openDatabase.Reader.QueryRowContext(testContext, `SELECT COUNT(*) FROM permissions`).Scan(&permissionCount)
		if countError != nil {
			t.Fatalf("count permissions: %v", countError)
		}
		if permissionCount != 61 {
			t.Fatalf("seeded %d permissions, want 61", permissionCount)
		}

		sampleDescription := ""
		describeError := openDatabase.Reader.QueryRowContext(testContext,
			`SELECT description FROM permissions WHERE id = $1`, "stock_movements:view").Scan(&sampleDescription)
		if describeError != nil {
			t.Fatalf("read permission: %v", describeError)
		}
		if sampleDescription != "View stock movements" {
			t.Fatalf("description %q, want %q", sampleDescription, "View stock movements")
		}

		firstCompanyId, firstRoleId := insertCompanyWithRole(t, openDatabase, "Manager", true)
		secondCompanyId, _ := insertCompanyWithRole(t, openDatabase, "Manager", true)

		_, duplicateRoleError := openDatabase.Writer.ExecContext(testContext,
			`INSERT INTO roles (id, company_id, name) VALUES ($1, $2, $3)`, uuid.Must(uuid.NewV7()), firstCompanyId, "Manager")
		if duplicateRoleError == nil {
			t.Fatal("a second Manager role in the same company was accepted")
		}

		_, secondOwnerRoleError := openDatabase.Writer.ExecContext(testContext,
			`INSERT INTO roles (id, company_id, name, is_owner) VALUES ($1, $2, $3, $4)`, uuid.Must(uuid.NewV7()), firstCompanyId, "Co-owner", true)
		if secondOwnerRoleError == nil {
			t.Fatal("a second owner role in the same company was accepted")
		}

		_, crossCompanyUserError := openDatabase.Writer.ExecContext(testContext,
			`INSERT INTO users (id, company_id, role_id, name, email, password_hash) VALUES ($1, $2, $3, $4, $5, $6)`,
			uuid.Must(uuid.NewV7()), secondCompanyId, firstRoleId, "Intruder", "intruder@example.com", "x")
		if crossCompanyUserError == nil {
			t.Fatal("a user in company B was allowed to take a role from company A")
		}

		_, mixedCaseEmailError := openDatabase.Writer.ExecContext(testContext,
			`INSERT INTO users (id, company_id, role_id, name, email, password_hash) VALUES ($1, $2, $3, $4, $5, $6)`,
			uuid.Must(uuid.NewV7()), firstCompanyId, firstRoleId, "Mixed", "Mixed@Example.com", "x")
		if mixedCaseEmailError == nil {
			t.Fatal("an email that is not lower case was accepted")
		}
	})
}

func TestSchemaGuardsProductsAndStock(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		openDatabase := testkit.OpenMigrated(t, engineCase)
		testContext := context.Background()
		firstCompanyId, _ := insertCompanyWithRole(t, openDatabase, "Owner", true)
		secondCompanyId, _ := insertCompanyWithRole(t, openDatabase, "Owner", true)

		insertProduct := func(companyId uuid.UUID, parentId *uuid.UUID, sku string) (uuid.UUID, error) {
			productId := uuid.Must(uuid.NewV7())
			_, insertError := openDatabase.Writer.ExecContext(testContext,
				`INSERT INTO products (id, company_id, parent_id, sku, name, price) VALUES ($1, $2, $3, $4, $5, $6)`,
				productId, companyId, parentId, sku, "Product "+sku, 1000)
			return productId, insertError
		}

		firstProductId, firstInsertError := insertProduct(firstCompanyId, nil, "SKU-1")
		if firstInsertError != nil {
			t.Fatalf("insert product: %v", firstInsertError)
		}
		if _, sameSkuOtherCompanyError := insertProduct(secondCompanyId, nil, "SKU-1"); sameSkuOtherCompanyError != nil {
			t.Fatalf("the same SKU in another company was rejected: %v", sameSkuOtherCompanyError)
		}
		if _, duplicateSkuError := insertProduct(firstCompanyId, nil, "SKU-1"); duplicateSkuError == nil {
			t.Fatal("a duplicate SKU in the same company was accepted")
		}
		if _, foreignParentError := insertProduct(secondCompanyId, &firstProductId, "SKU-2"); foreignParentError == nil {
			t.Fatal("a variant was allowed to point at another company's product")
		}

		insertBarcode := func(companyId uuid.UUID, productId uuid.UUID, code string) error {
			_, insertError := openDatabase.Writer.ExecContext(testContext,
				`INSERT INTO barcodes (id, company_id, product_id, code) VALUES ($1, $2, $3, $4)`,
				uuid.Must(uuid.NewV7()), companyId, productId, code)
			return insertError
		}
		if barcodeError := insertBarcode(firstCompanyId, firstProductId, "6001234"); barcodeError != nil {
			t.Fatalf("insert barcode: %v", barcodeError)
		}
		if duplicateBarcodeError := insertBarcode(firstCompanyId, firstProductId, "6001234"); duplicateBarcodeError == nil {
			t.Fatal("a duplicate barcode in the same company was accepted")
		}

		shopId := uuid.Must(uuid.NewV7())
		_, shopError := openDatabase.Writer.ExecContext(testContext, `INSERT INTO shops (id, company_id, name) VALUES ($1, $2, $3)`, shopId, firstCompanyId, "Main")
		if shopError != nil {
			t.Fatalf("insert shop: %v", shopError)
		}
		_, negativeStockError := openDatabase.Writer.ExecContext(testContext,
			`INSERT INTO shop_stock (company_id, shop_id, product_id, quantity) VALUES ($1, $2, $3, $4)`,
			firstCompanyId, shopId, firstProductId, -1)
		if negativeStockError == nil {
			t.Fatal("negative stock was accepted")
		}
		_, unknownReasonError := openDatabase.Writer.ExecContext(testContext,
			`INSERT INTO stock_movements (id, company_id, shop_id, product_id, change, quantity_after, reason) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			uuid.Must(uuid.NewV7()), firstCompanyId, shopId, firstProductId, 5, 5, "gift")
		if unknownReasonError == nil {
			t.Fatal("an unknown stock movement reason was accepted")
		}
	})
}

func TestSchemaGuardsTransfersAndNotifications(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		openDatabase := testkit.OpenMigrated(t, engineCase)
		testContext := context.Background()
		firstCompanyId, _ := insertCompanyWithRole(t, openDatabase, "Owner", true)
		secondCompanyId, _ := insertCompanyWithRole(t, openDatabase, "Owner", true)

		insertShop := func(companyId uuid.UUID, name string) uuid.UUID {
			shopId := uuid.Must(uuid.NewV7())
			_, shopError := openDatabase.Writer.ExecContext(testContext, `INSERT INTO shops (id, company_id, name) VALUES ($1, $2, $3)`, shopId, companyId, name)
			if shopError != nil {
				t.Fatalf("insert shop: %v", shopError)
			}
			return shopId
		}
		mainShopId := insertShop(firstCompanyId, "Main")
		branchShopId := insertShop(firstCompanyId, "Branch")
		foreignShopId := insertShop(secondCompanyId, "Foreign")

		foreignProductId := uuid.Must(uuid.NewV7())
		_, productError := openDatabase.Writer.ExecContext(testContext,
			`INSERT INTO products (id, company_id, sku, name, price) VALUES ($1, $2, $3, $4, $5)`, foreignProductId, secondCompanyId, "FOREIGN", "Foreign", 100)
		if productError != nil {
			t.Fatalf("insert product: %v", productError)
		}

		insertTransfer := func(fromShopId uuid.UUID, toShopId uuid.UUID) (uuid.UUID, error) {
			transferId := uuid.Must(uuid.NewV7())
			_, insertError := openDatabase.Writer.ExecContext(testContext,
				`INSERT INTO stock_transfers (id, company_id, from_shop_id, to_shop_id) VALUES ($1, $2, $3, $4)`,
				transferId, firstCompanyId, fromShopId, toShopId)
			return transferId, insertError
		}
		if _, sameShopError := insertTransfer(mainShopId, mainShopId); sameShopError == nil {
			t.Fatal("a transfer to the same shop was accepted")
		}
		if _, foreignShopError := insertTransfer(mainShopId, foreignShopId); foreignShopError == nil {
			t.Fatal("a transfer to another company's shop was accepted")
		}
		transferId, transferError := insertTransfer(mainShopId, branchShopId)
		if transferError != nil {
			t.Fatalf("insert transfer: %v", transferError)
		}
		_, foreignItemError := openDatabase.Writer.ExecContext(testContext,
			`INSERT INTO stock_transfer_items (company_id, transfer_id, product_id, quantity) VALUES ($1, $2, $3, $4)`,
			firstCompanyId, transferId, foreignProductId, 1)
		if foreignItemError == nil {
			t.Fatal("a transfer item pointing at another company's product was accepted")
		}

		_, unknownKindError := openDatabase.Writer.ExecContext(testContext,
			`INSERT INTO notifications (id, company_id, shop_id, product_id, kind, quantity, min_stock) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
			uuid.Must(uuid.NewV7()), secondCompanyId, foreignShopId, foreignProductId, "overstock", 1, 1)
		if unknownKindError == nil {
			t.Fatal("an unknown notification kind was accepted")
		}
	})
}

func TestSchemaGuardsDiscountsAndSales(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		openDatabase := testkit.OpenMigrated(t, engineCase)
		testContext := context.Background()
		companyId, roleId := insertCompanyWithRole(t, openDatabase, "Owner", true)

		exec := func(statement string, arguments ...any) error {
			_, execError := openDatabase.Writer.ExecContext(testContext, statement, arguments...)
			return execError
		}
		mustExec := func(statement string, arguments ...any) {
			t.Helper()
			if execError := exec(statement, arguments...); execError != nil {
				t.Fatalf("%s: %v", statement, execError)
			}
		}

		userId := uuid.Must(uuid.NewV7())
		mustExec(`INSERT INTO users (id, company_id, role_id, name, email, password_hash) VALUES ($1, $2, $3, $4, $5, $6)`, userId, companyId, roleId, "Cashier", "cashier-"+userId.String()[:8]+"@example.com", "x")
		mainShopId := uuid.Must(uuid.NewV7())
		branchShopId := uuid.Must(uuid.NewV7())
		mustExec(`INSERT INTO shops (id, company_id, name) VALUES ($1, $2, $3)`, mainShopId, companyId, "Main")
		mustExec(`INSERT INTO shops (id, company_id, name) VALUES ($1, $2, $3)`, branchShopId, companyId, "Branch")
		productId := uuid.Must(uuid.NewV7())
		mustExec(`INSERT INTO products (id, company_id, sku, name, price) VALUES ($1, $2, $3, $4, $5)`, productId, companyId, "SODA", "Soda", 1000)

		insertDiscount := func(kind string, value int64, startsAt time.Time, endsAt time.Time) error {
			return exec(`INSERT INTO discounts (id, company_id, name, kind, value, starts_at, ends_at) VALUES ($1, $2, $3, $4, $5, $6, $7)`,
				uuid.Must(uuid.NewV7()), companyId, "Promo", kind, value, startsAt, endsAt)
		}
		now := time.Now().UTC()
		if insertDiscount("percent", 10001, now, now.Add(time.Hour)) == nil {
			t.Fatal("a discount above 100% was accepted")
		}
		if insertDiscount("fixed", 0, now, now.Add(time.Hour)) == nil {
			t.Fatal("a zero fixed discount was accepted")
		}
		if insertDiscount("percent", 1000, now, now) == nil {
			t.Fatal("a discount that ends when it starts was accepted")
		}
		if discountError := insertDiscount("percent", 1000, now, now.Add(time.Hour)); discountError != nil {
			t.Fatalf("a valid discount was refused: %v", discountError)
		}

		insertSale := func(shopId uuid.UUID, clientRef string, receiptNumber string, subtotal int64, discountTotal int64, total int64, amountPaid int64, changeGiven int64) (uuid.UUID, error) {
			saleId := uuid.Must(uuid.NewV7())
			insertError := exec(`
				INSERT INTO sales (id, company_id, shop_id, user_id, client_ref, request_hash, receipt_number, subtotal, discount_total, total,
				                   tax_total, tax_rate_basis_points, amount_paid, change_given, currency_code, currency_decimals)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16)`,
				saleId, companyId, shopId, userId, clientRef, "hash", receiptNumber, subtotal, discountTotal, total, 0, 1800, amountPaid, changeGiven, "TZS", 0)
			return saleId, insertError
		}
		saleId, saleError := insertSale(mainShopId, "client-ref-0001", "SALE-0001", 2000, 0, 2000, 5000, 3000)
		if saleError != nil {
			t.Fatalf("a valid sale was refused: %v", saleError)
		}
		refusedSales := []struct {
			why  string
			fail func() error
		}{
			{"a repeated client_ref", func() error {
				_, e := insertSale(branchShopId, "client-ref-0001", "SALE-0009", 1, 0, 1, 1, 0)
				return e
			}},
			{"a repeated receipt number in one shop", func() error { _, e := insertSale(mainShopId, "client-ref-0002", "SALE-0001", 1, 0, 1, 1, 0); return e }},
			{"a total that is not subtotal minus discount", func() error {
				_, e := insertSale(mainShopId, "client-ref-0003", "SALE-0002", 1000, 100, 1000, 1000, 0)
				return e
			}},
			{"a payment that does not equal total plus change", func() error {
				_, e := insertSale(mainShopId, "client-ref-0004", "SALE-0003", 1000, 0, 1000, 900, 0)
				return e
			}},
			{"a too-short client_ref", func() error { _, e := insertSale(mainShopId, "short", "SALE-0004", 1, 0, 1, 1, 0); return e }},
		}
		for _, refusedSale := range refusedSales {
			if refusedSale.fail() == nil {
				t.Fatalf("%s was accepted", refusedSale.why)
			}
		}
		if _, sameNumberOtherShop := insertSale(branchShopId, "client-ref-0005", "SALE-0001", 1, 0, 1, 1, 0); sameNumberOtherShop != nil {
			t.Fatalf("the same receipt number in another shop was refused: %v", sameNumberOtherShop)
		}

		insertItem := func(position int, quantity int, unitPrice int64, addonsUnitTotal int64, discountAmount int64, lineTotal int64) error {
			return exec(`
				INSERT INTO sale_items (id, company_id, sale_id, position, product_id, product_name, sku, unit, quantity, unit_price, unit_cost,
				                        addons_unit_total, discount_amount, line_total)
				VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14)`,
				uuid.Must(uuid.NewV7()), companyId, saleId, position, productId, "Soda", "SODA", "btl", quantity, unitPrice, 600, addonsUnitTotal, discountAmount, lineTotal)
		}
		if itemError := insertItem(0, 2, 1000, 100, 200, 2000); itemError != nil {
			t.Fatalf("a valid line was refused: %v", itemError)
		}
		if insertItem(1, 2, 1000, 0, 0, 1999) == nil {
			t.Fatal("a line whose total does not add up was accepted")
		}
		if exec(`INSERT INTO sale_payments (company_id, sale_id, method, amount) VALUES ($1, $2, $3, $4)`, companyId, saleId, "cheque", 100) == nil {
			t.Fatal("an unknown payment method was accepted")
		}
	})
}
