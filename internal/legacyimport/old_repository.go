package legacyimport

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"net/url"
	"strings"
	"time"
)

var (
	ErrOldDataNotFound   = errors.New("no data from the old Balce app was found on this computer")
	ErrOldDataUnreadable = errors.New("the old Balce data file could not be read; it may be damaged or from an unknown version")
)

var oldTableNames = []string{
	"companies", "settings", "roles", "permissions", "role_permissions", "user_permissions", "users",
	"products", "barcodes", "product_addons", "sales", "sale_items", "discounts", "suppliers", "purchases",
}

var oldTimeLayouts = []string{
	"2006-01-02 15:04:05.999999999 -0700 MST",
	"2006-01-02 15:04:05.999999999-07:00",
	"2006-01-02T15:04:05.999999999-07:00",
	"2006-01-02 15:04:05.999999999Z07:00",
	"2006-01-02T15:04:05.999999999Z07:00",
	"2006-01-02 15:04:05.999999999",
	"2006-01-02T15:04:05.999999999",
	"2006-01-02",
}

type oldTable struct {
	name    string
	columns map[string]bool
}

func (table oldTable) exists() bool {
	return len(table.columns) > 0
}

func (table oldTable) value(columnName string, fallback string) string {
	if table.columns[columnName] {
		return "COALESCE(" + columnName + ", " + fallback + ")"
	}
	return fallback
}

func (table oldTable) integer(columnName string, fallback string) string {
	return "CAST(" + table.value(columnName, fallback) + " AS INTEGER)"
}

func (table oldTable) real(columnName string) string {
	return "CAST(" + table.value(columnName, "0") + " AS REAL)"
}

func (table oldTable) text(columnName string) string {
	return "CAST(" + table.value(columnName, "''") + " AS TEXT)"
}

func (table oldTable) nullable(columnName string, sqlType string) string {
	if table.columns[columnName] {
		return "CAST(" + columnName + " AS " + sqlType + ")"
	}
	return "CAST(NULL AS " + sqlType + ")"
}

func (table oldTable) id() string {
	return table.integer("id", "rowid")
}

func (table oldTable) live() string {
	if table.columns["deleted_at"] {
		return "deleted_at IS NULL"
	}
	return "1 = 1"
}

func (table oldTable) isDeleted() string {
	if table.columns["deleted_at"] {
		return "(deleted_at IS NOT NULL)"
	}
	return "0"
}

type oldReader struct {
	database *sql.DB
	tables   map[string]oldTable
}

func openOldDatabase(ctx context.Context, oldDatabasePath string) (*oldReader, error) {
	parameters := url.Values{}
	parameters.Add("mode", "ro")
	parameters.Add("_pragma", "query_only(1)")
	parameters.Add("_pragma", "busy_timeout(5000)")

	oldDatabase, openError := sql.Open("sqlite", "file:"+oldDatabasePath+"?"+parameters.Encode())
	if openError != nil {
		return nil, fmt.Errorf("%w: %v", ErrOldDataUnreadable, openError)
	}
	oldDatabase.SetMaxOpenConns(1)

	reader := &oldReader{database: oldDatabase, tables: map[string]oldTable{}}
	for _, tableName := range oldTableNames {
		columnNames, columnsError := reader.readColumns(ctx, tableName)
		if columnsError != nil {
			oldDatabase.Close()
			return nil, fmt.Errorf("%w: %v", ErrOldDataUnreadable, columnsError)
		}
		reader.tables[tableName] = oldTable{name: tableName, columns: columnNames}
	}

	hasBalceTables := reader.tables["companies"].exists() || reader.tables["users"].exists() || reader.tables["products"].exists()
	if !hasBalceTables {
		oldDatabase.Close()
		return nil, ErrOldDataUnreadable
	}

	return reader, nil
}

func (reader *oldReader) Close() error {
	return reader.database.Close()
}

func (reader *oldReader) readColumns(ctx context.Context, tableName string) (map[string]bool, error) {
	columnRows, queryError := reader.database.QueryContext(ctx, "SELECT name FROM pragma_table_info('"+tableName+"')")
	if queryError != nil {
		return nil, fmt.Errorf("failed to read old table %s: %w", tableName, queryError)
	}
	defer columnRows.Close()

	columnNames := map[string]bool{}
	for columnRows.Next() {
		columnName := ""
		scanError := columnRows.Scan(&columnName)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan old column of %s: %w", tableName, scanError)
		}
		columnNames[strings.ToLower(columnName)] = true
	}

	return columnNames, columnRows.Err()
}

func (reader *oldReader) eachRow(ctx context.Context, query string, scanRow func(rows *sql.Rows) error) error {
	oldRows, queryError := reader.database.QueryContext(ctx, query)
	if queryError != nil {
		return fmt.Errorf("%w: %v", ErrOldDataUnreadable, queryError)
	}
	defer oldRows.Close()

	for oldRows.Next() {
		scanError := scanRow(oldRows)
		if scanError != nil {
			return fmt.Errorf("%w: %v", ErrOldDataUnreadable, scanError)
		}
	}

	return oldRows.Err()
}

func (reader *oldReader) readAll(ctx context.Context) (oldData, error) {
	loadedData := oldData{Roles: map[int64]oldRole{}}
	readSteps := []func(ctx context.Context, loadedData *oldData) error{
		reader.readCompany,
		reader.readSettings,
		reader.readRoles,
		reader.readUsers,
		reader.readProducts,
		reader.readBarcodes,
		reader.readAddons,
		reader.readSales,
		reader.readDiscounts,
		reader.readSuppliers,
		reader.readPurchaseCount,
	}
	for _, readStep := range readSteps {
		readError := readStep(ctx, &loadedData)
		if readError != nil {
			return oldData{}, readError
		}
	}
	return loadedData, nil
}

func (reader *oldReader) readCompany(ctx context.Context, loadedData *oldData) error {
	companies := reader.tables["companies"]
	if !companies.exists() {
		return nil
	}

	query := `SELECT ` + strings.Join([]string{
		companies.text("name"),
		companies.text("business_type"),
		companies.nullable("phone", "TEXT"),
		companies.nullable("address", "TEXT"),
		companies.nullable("tin", "TEXT"),
		companies.nullable("receipt_header", "TEXT"),
		companies.nullable("receipt_footer", "TEXT"),
	}, ", ") + ` FROM companies WHERE ` + companies.live() + ` ORDER BY ` + companies.id() + ` LIMIT 1`

	return reader.eachRow(ctx, query, func(rows *sql.Rows) error {
		company := oldCompany{}
		scanError := rows.Scan(&company.Name, &company.BusinessType, &company.Phone, &company.Address, &company.Tin, &company.ReceiptHeader, &company.ReceiptFooter)
		loadedData.Company = &company
		return scanError
	})
}

func (reader *oldReader) readSettings(ctx context.Context, loadedData *oldData) error {
	settingsTable := reader.tables["settings"]
	if !settingsTable.exists() {
		return nil
	}

	query := `SELECT ` + strings.Join([]string{
		settingsTable.real("tax_rate"),
		settingsTable.text("currency"),
		settingsTable.text("date_format"),
		settingsTable.integer("low_stock_threshold", "5"),
		settingsTable.integer("email_notifications_enabled", "0"),
		settingsTable.nullable("notification_email", "TEXT"),
		settingsTable.integer("alert_sound_enabled", "1"),
		settingsTable.integer("alert_on_low_stock", "1"),
		settingsTable.integer("alert_on_out_of_stock", "1"),
		settingsTable.integer("alert_on_dead_stock", "0"),
		settingsTable.integer("dead_stock_days", "30"),
		settingsTable.integer("print_receipt_automatically", "0"),
		settingsTable.integer("show_tax_on_receipt", "1"),
		settingsTable.integer("show_barcodes_on_receipt", "0"),
		settingsTable.integer("printer_enabled", "0"),
		settingsTable.text("printer_port"),
		settingsTable.text("printer_model"),
		settingsTable.integer("printer_baud_rate", "9600"),
		settingsTable.integer("printer_paper_width", "80"),
		settingsTable.integer("open_cash_drawer", "0"),
		settingsTable.integer("efd_enabled", "0"),
	}, ", ") + ` FROM settings WHERE ` + settingsTable.live() + ` ORDER BY ` + settingsTable.id() + ` LIMIT 1`

	return reader.eachRow(ctx, query, func(rows *sql.Rows) error {
		loadedSettings := oldSettings{}
		scanError := rows.Scan(
			&loadedSettings.TaxRate,
			&loadedSettings.Currency,
			&loadedSettings.DateFormat,
			&loadedSettings.LowStockThreshold,
			&loadedSettings.EmailNotificationsEnabled,
			&loadedSettings.NotificationEmail,
			&loadedSettings.AlertSoundEnabled,
			&loadedSettings.AlertOnLowStock,
			&loadedSettings.AlertOnOutOfStock,
			&loadedSettings.AlertOnDeadStock,
			&loadedSettings.DeadStockDays,
			&loadedSettings.PrintReceiptAutomatically,
			&loadedSettings.ShowTaxOnReceipt,
			&loadedSettings.ShowBarcodesOnReceipt,
			&loadedSettings.PrinterEnabled,
			&loadedSettings.PrinterPort,
			&loadedSettings.PrinterModel,
			&loadedSettings.PrinterBaudRate,
			&loadedSettings.PrinterPaperWidth,
			&loadedSettings.OpenCashDrawer,
			&loadedSettings.EfdEnabled,
		)
		loadedData.Settings = &loadedSettings
		return scanError
	})
}

func (reader *oldReader) readRoles(ctx context.Context, loadedData *oldData) error {
	roles := reader.tables["roles"]
	if !roles.exists() {
		return nil
	}

	rolesQuery := `SELECT ` + roles.id() + `, ` + roles.text("name") + ` FROM roles WHERE ` + roles.live()
	rolesError := reader.eachRow(ctx, rolesQuery, func(rows *sql.Rows) error {
		role := oldRole{}
		scanError := rows.Scan(&role.Id, &role.Name)
		loadedData.Roles[role.Id] = role
		return scanError
	})
	if rolesError != nil {
		return rolesError
	}

	permissionNamesByRole, grantsError := reader.readPermissionGrants(ctx, "role_permissions", "role_id")
	if grantsError != nil {
		return grantsError
	}
	for roleId, permissionNames := range permissionNamesByRole {
		role, isKnownRole := loadedData.Roles[roleId]
		if isKnownRole {
			role.PermissionIds = permissionNames
			loadedData.Roles[roleId] = role
		}
	}

	return nil
}

func (reader *oldReader) readPermissionGrants(ctx context.Context, grantTableName string, ownerColumn string) (map[int64][]string, error) {
	grants := reader.tables[grantTableName]
	permissions := reader.tables["permissions"]
	permissionNamesByOwner := map[int64][]string{}
	canJoin := grants.columns[ownerColumn] && grants.columns["permission_id"] && permissions.columns["id"] && permissions.columns["name"]
	if !canJoin {
		return permissionNamesByOwner, nil
	}

	query := `
		SELECT CAST(g.` + ownerColumn + ` AS INTEGER), CAST(p.name AS TEXT)
		FROM ` + grantTableName + ` g
		JOIN permissions p ON p.id = g.permission_id
		WHERE p.name IS NOT NULL
	`
	grantsError := reader.eachRow(ctx, query, func(rows *sql.Rows) error {
		ownerId := int64(0)
		permissionName := ""
		scanError := rows.Scan(&ownerId, &permissionName)
		permissionNamesByOwner[ownerId] = append(permissionNamesByOwner[ownerId], strings.TrimSpace(permissionName))
		return scanError
	})

	return permissionNamesByOwner, grantsError
}

func (reader *oldReader) readUsers(ctx context.Context, loadedData *oldData) error {
	users := reader.tables["users"]
	if !users.exists() {
		return nil
	}

	query := `SELECT ` + strings.Join([]string{
		users.id(),
		users.text("name"),
		users.text("email"),
		users.text("password_hash"),
		users.integer("role_id", "0"),
		users.text("created_at"),
	}, ", ") + ` FROM users WHERE ` + users.live() + ` ORDER BY ` + users.id()

	usersError := reader.eachRow(ctx, query, func(rows *sql.Rows) error {
		user := oldUser{}
		createdAtText := ""
		scanError := rows.Scan(&user.Id, &user.Name, &user.Email, &user.PasswordHash, &user.RoleId, &createdAtText)
		user.CreatedAt = parseOldTime(createdAtText)
		loadedData.Users = append(loadedData.Users, user)
		return scanError
	})
	if usersError != nil {
		return usersError
	}

	permissionNamesByUser, grantsError := reader.readPermissionGrants(ctx, "user_permissions", "user_id")
	if grantsError != nil {
		return grantsError
	}
	for userIndex := range loadedData.Users {
		loadedData.Users[userIndex].PermissionIds = permissionNamesByUser[loadedData.Users[userIndex].Id]
	}

	return nil
}

func (reader *oldReader) readProducts(ctx context.Context, loadedData *oldData) error {
	products := reader.tables["products"]
	if !products.exists() {
		return nil
	}

	query := `SELECT ` + strings.Join([]string{
		products.id(),
		products.text("name"),
		products.text("sku"),
		products.nullable("barcode", "TEXT"),
		products.nullable("parent_id", "INTEGER"),
		products.text("variant_label"),
		products.real("price"),
		products.real("cost_price"),
		products.nullable("wholesale_price", "REAL"),
		products.integer("wholesale_min", "10"),
		products.integer("quantity", "0"),
		products.integer("min_stock", "5"),
		products.nullable("category", "TEXT"),
		products.text("unit"),
		products.integer("pieces_per_unit", "1"),
		products.nullable("image", "TEXT"),
		products.nullable("metadata", "TEXT"),
		products.isDeleted(),
		products.text("created_at"),
	}, ", ") + ` FROM products ORDER BY ` + products.id()

	return reader.eachRow(ctx, query, func(rows *sql.Rows) error {
		product := oldProduct{}
		createdAtText := ""
		scanError := rows.Scan(
			&product.Id,
			&product.Name,
			&product.Sku,
			&product.Barcode,
			&product.ParentId,
			&product.VariantLabel,
			&product.Price,
			&product.CostPrice,
			&product.WholesalePrice,
			&product.WholesaleMin,
			&product.Quantity,
			&product.MinStock,
			&product.Category,
			&product.Unit,
			&product.PiecesPerUnit,
			&product.Image,
			&product.Metadata,
			&product.IsDeleted,
			&createdAtText,
		)
		product.CreatedAt = parseOldTime(createdAtText)
		loadedData.Products = append(loadedData.Products, product)
		return scanError
	})
}

func (reader *oldReader) readBarcodes(ctx context.Context, loadedData *oldData) error {
	barcodes := reader.tables["barcodes"]
	if !barcodes.exists() {
		return nil
	}

	query := `SELECT ` + strings.Join([]string{
		barcodes.integer("product_id", "0"),
		barcodes.text("code"),
		barcodes.integer("pack_size", "1"),
	}, ", ") + ` FROM barcodes WHERE ` + barcodes.live() + ` AND ` + barcodes.integer("is_active", "1") + ` <> 0 ORDER BY ` + barcodes.id()

	return reader.eachRow(ctx, query, func(rows *sql.Rows) error {
		barcode := oldBarcode{}
		scanError := rows.Scan(&barcode.ProductId, &barcode.Code, &barcode.PackSize)
		loadedData.Barcodes = append(loadedData.Barcodes, barcode)
		return scanError
	})
}

func (reader *oldReader) readAddons(ctx context.Context, loadedData *oldData) error {
	addons := reader.tables["product_addons"]
	if !addons.exists() {
		return nil
	}

	query := `SELECT ` + strings.Join([]string{
		addons.integer("product_id", "0"),
		addons.text("name"),
		addons.real("price"),
		addons.integer("is_active", "1"),
	}, ", ") + ` FROM product_addons WHERE ` + addons.live() + ` ORDER BY ` + addons.id()

	return reader.eachRow(ctx, query, func(rows *sql.Rows) error {
		addon := oldAddon{}
		scanError := rows.Scan(&addon.ProductId, &addon.Name, &addon.Price, &addon.IsActive)
		loadedData.Addons = append(loadedData.Addons, addon)
		return scanError
	})
}

func (reader *oldReader) readSales(ctx context.Context, loadedData *oldData) error {
	sales := reader.tables["sales"]
	if !sales.exists() {
		return nil
	}

	salesQuery := `SELECT ` + strings.Join([]string{
		sales.id(),
		sales.text("receipt_number"),
		sales.integer("user_id", "0"),
		sales.real("total_amount"),
		sales.real("tax_amount"),
		sales.real("amount_paid"),
		sales.text("payment_type"),
		sales.text("created_at"),
	}, ", ") + ` FROM sales WHERE ` + sales.live() + ` ORDER BY ` + sales.id()

	saleIndexById := map[int64]int{}
	salesError := reader.eachRow(ctx, salesQuery, func(rows *sql.Rows) error {
		sale := oldSale{}
		createdAtText := ""
		scanError := rows.Scan(&sale.Id, &sale.ReceiptNumber, &sale.UserId, &sale.TotalAmount, &sale.TaxAmount, &sale.AmountPaid, &sale.PaymentType, &createdAtText)
		sale.CreatedAt = parseOldTime(createdAtText)
		saleIndexById[sale.Id] = len(loadedData.Sales)
		loadedData.Sales = append(loadedData.Sales, sale)
		return scanError
	})
	if salesError != nil {
		return salesError
	}

	saleItems := reader.tables["sale_items"]
	if !saleItems.exists() {
		return nil
	}

	linesQuery := `SELECT ` + strings.Join([]string{
		saleItems.integer("sale_id", "0"),
		saleItems.integer("product_id", "0"),
		saleItems.integer("quantity", "0"),
		saleItems.real("unit_price"),
		saleItems.integer("is_wholesale", "0"),
	}, ", ") + ` FROM sale_items WHERE ` + saleItems.live() + ` AND ` + saleItems.integer("quantity", "0") + ` > 0 ORDER BY ` + saleItems.integer("sale_id", "0") + `, ` + saleItems.id()

	return reader.eachRow(ctx, linesQuery, func(rows *sql.Rows) error {
		saleId := int64(0)
		line := oldSaleLine{}
		scanError := rows.Scan(&saleId, &line.ProductId, &line.Quantity, &line.UnitPrice, &line.IsWholesale)
		saleIndex, isLiveSale := saleIndexById[saleId]
		if isLiveSale {
			loadedData.Sales[saleIndex].Lines = append(loadedData.Sales[saleIndex].Lines, line)
		}
		return scanError
	})
}

func (reader *oldReader) readDiscounts(ctx context.Context, loadedData *oldData) error {
	discounts := reader.tables["discounts"]
	if !discounts.exists() {
		return nil
	}

	query := `SELECT ` + strings.Join([]string{
		discounts.text("name"),
		discounts.nullable("product_id", "INTEGER"),
		discounts.text("discount_type"),
		discounts.real("value"),
		discounts.text("starts_at"),
		discounts.text("ends_at"),
		discounts.integer("is_active", "1"),
		discounts.nullable("created_by", "INTEGER"),
	}, ", ") + ` FROM discounts WHERE ` + discounts.live() + ` ORDER BY ` + discounts.id()

	return reader.eachRow(ctx, query, func(rows *sql.Rows) error {
		discount := oldDiscount{}
		startsAtText := ""
		endsAtText := ""
		scanError := rows.Scan(&discount.Name, &discount.ProductId, &discount.DiscountType, &discount.Value, &startsAtText, &endsAtText, &discount.IsActive, &discount.CreatedBy)
		discount.StartsAt = parseOldTime(startsAtText)
		discount.EndsAt = parseOldTime(endsAtText)
		loadedData.Discounts = append(loadedData.Discounts, discount)
		return scanError
	})
}

func (reader *oldReader) readSuppliers(ctx context.Context, loadedData *oldData) error {
	suppliers := reader.tables["suppliers"]
	if !suppliers.exists() {
		return nil
	}

	query := `SELECT ` + strings.Join([]string{
		suppliers.text("name"),
		suppliers.nullable("phone", "TEXT"),
		suppliers.nullable("notes", "TEXT"),
		suppliers.text("created_at"),
	}, ", ") + ` FROM suppliers WHERE ` + suppliers.live() + ` ORDER BY ` + suppliers.id()

	return reader.eachRow(ctx, query, func(rows *sql.Rows) error {
		supplier := oldSupplier{}
		createdAtText := ""
		scanError := rows.Scan(&supplier.Name, &supplier.Phone, &supplier.Notes, &createdAtText)
		supplier.CreatedAt = parseOldTime(createdAtText)
		loadedData.Suppliers = append(loadedData.Suppliers, supplier)
		return scanError
	})
}

func (reader *oldReader) readPurchaseCount(ctx context.Context, loadedData *oldData) error {
	purchaseCount, countError := reader.countLive(ctx, "purchases", "1 = 1")
	loadedData.PurchaseCount = purchaseCount
	return countError
}

func (reader *oldReader) countLive(ctx context.Context, tableName string, extraCondition string) (int64, error) {
	table := reader.tables[tableName]
	if !table.exists() {
		return 0, nil
	}

	liveCount := int64(0)
	query := `SELECT COUNT(*) FROM ` + tableName + ` WHERE ` + table.live() + ` AND ` + extraCondition
	scanError := reader.database.QueryRowContext(ctx, query).Scan(&liveCount)
	if scanError != nil {
		return 0, fmt.Errorf("%w: %v", ErrOldDataUnreadable, scanError)
	}
	return liveCount, nil
}

func (reader *oldReader) readTotals(ctx context.Context, currencyDecimals int) (oldTotals, error) {
	totals := oldTotals{}
	sales := reader.tables["sales"]
	saleItems := reader.tables["sale_items"]
	products := reader.tables["products"]

	liveSaleLines := saleItems.integer("quantity", "0") + ` > 0 AND ` + saleItems.integer("sale_id", "0") + ` IN (SELECT ` + sales.id() + ` FROM sales WHERE ` + sales.live() + `)`
	if !sales.exists() {
		liveSaleLines = "0 = 1"
	}

	countTargets := []struct {
		tableName      string
		extraCondition string
		target         *int64
	}{
		{"products", "1 = 1", &totals.Products},
		{"users", "1 = 1", &totals.Users},
		{"sales", "1 = 1", &totals.Sales},
		{"sale_items", liveSaleLines, &totals.SaleLines},
		{"suppliers", "1 = 1", &totals.Suppliers},
	}
	for _, countTarget := range countTargets {
		liveCount, countError := reader.countLive(ctx, countTarget.tableName, countTarget.extraCondition)
		if countError != nil {
			return oldTotals{}, countError
		}
		*countTarget.target = liveCount
	}

	if sales.exists() {
		salesValueError := reader.eachRow(ctx, `SELECT `+sales.real("total_amount")+` FROM sales WHERE `+sales.live(), func(rows *sql.Rows) error {
			saleTotal := 0.0
			scanError := rows.Scan(&saleTotal)
			totals.SalesValue += toMinorUnits(saleTotal, currencyDecimals)
			return scanError
		})
		if salesValueError != nil {
			return oldTotals{}, salesValueError
		}
	}

	if products.exists() {
		stockValueError := reader.eachRow(ctx, `SELECT `+products.integer("quantity", "0")+`, `+products.real("cost_price")+` FROM products WHERE `+products.live(), func(rows *sql.Rows) error {
			quantity := 0
			costPrice := 0.0
			scanError := rows.Scan(&quantity, &costPrice)
			totals.StockValue += int64(max(quantity, 0)) * max(toMinorUnits(costPrice, currencyDecimals), 0)
			return scanError
		})
		if stockValueError != nil {
			return oldTotals{}, stockValueError
		}
	}

	return totals, nil
}

func parseOldTime(timeText string) time.Time {
	trimmedText, _, _ := strings.Cut(strings.TrimSpace(timeText), " m=")
	for _, timeLayout := range oldTimeLayouts {
		parsedTime, parseError := time.Parse(timeLayout, trimmedText)
		if parseError == nil {
			return parsedTime.UTC()
		}
	}
	return time.Time{}
}
