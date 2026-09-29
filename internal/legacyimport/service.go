package legacyimport

import (
	"context"
	"crypto/rand"
	"encoding/json"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"math/big"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/access"
	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/discounts"
	"github.com/chrisostomemataba/balceinv-api/internal/products"
	"github.com/chrisostomemataba/balceinv-api/internal/sales"
	"github.com/chrisostomemataba/balceinv-api/internal/settings"
	"github.com/chrisostomemataba/balceinv-api/internal/stock"
	"github.com/chrisostomemataba/balceinv-api/internal/tenancy"
	"github.com/chrisostomemataba/balceinv-api/internal/users"
	"github.com/google/uuid"
	"golang.org/x/crypto/bcrypt"
)

var (
	ErrNoOldUsers          = errors.New("the old data has no user accounts to sign in with; set up a new business instead")
	ErrUnknownOwner        = errors.New("choose one of the old admin accounts as the owner")
	ErrBusinessNameMissing = errors.New("enter the business name")
	ErrImportMismatch      = errors.New("the brought-over data did not match the old data, so nothing was saved")
)

const (
	noteSuppliersNotImported   = "suppliers_not_imported"
	noteDiscountsSkipped       = "discounts_skipped"
	noteProductImagesNotCopied = "product_images_not_copied"
	noteHistoryProductsHidden  = "history_products_archived"
	noteNegativeStockZeroed    = "negative_stock_set_to_zero"
	notePurchasesNotImported   = "purchases_not_imported"
	noteEfdSettingsNotCopied   = "efd_settings_not_copied"
	openingStockReference      = "Old app"
	importRequestHash          = "old-app-import"
	temporaryPasswordAlphabet  = "abcdefghjkmnpqrstuvwxyz23456789"
	temporaryPasswordLength    = 10
)

var zeroDecimalCurrencies = map[string]bool{"TZS": true, "UGX": true, "RWF": true, "BIF": true}

var allowedDateFormats = map[string]bool{"DD/MM/YYYY": true, "MM/DD/YYYY": true, "YYYY-MM-DD": true}

type Service struct {
	tenancyService      *tenancy.Service
	stockService        *stock.Service
	repository          *Repository
	accessRepository    *access.Repository
	usersRepository     *users.Repository
	settingsRepository  *settings.Repository
	productsRepository  *products.Repository
	stockRepository     *stock.Repository
	salesRepository     *sales.Repository
	discountsRepository *discounts.Repository
}

func NewService(tenancyService *tenancy.Service, stockService *stock.Service) *Service {
	return &Service{
		tenancyService:      tenancyService,
		stockService:        stockService,
		repository:          NewRepository(),
		accessRepository:    access.NewRepository(),
		usersRepository:     users.NewRepository(),
		settingsRepository:  settings.NewRepository(),
		productsRepository:  products.NewRepository(),
		stockRepository:     stock.NewRepository(),
		salesRepository:     sales.NewRepository(),
		discountsRepository: discounts.NewRepository(),
	}
}

func (service *Service) Preview(ctx context.Context, querier database.Querier, oldDatabasePath string) (PreviewView, error) {
	configuredError := service.refuseWhenConfigured(ctx, querier)
	if configuredError != nil {
		return PreviewView{}, configuredError
	}

	loadedData, _, readError := readOldFile(ctx, oldDatabasePath)
	if readError != nil {
		return PreviewView{}, readError
	}

	currencyCode, currencyDecimals := currencyOf(loadedData)
	preview := PreviewView{
		BusinessName:     companyNameOf(loadedData),
		CurrencyCode:     currencyCode,
		CurrencyDecimals: currencyDecimals,
		OwnerChoices:     []OldUserView{},
		PasswordsKept:    true,
		Counts: PreviewCountsView{
			Users:     len(loadedData.Users),
			Sales:     len(loadedData.Sales),
			Suppliers: len(loadedData.Suppliers),
			Discounts: len(loadedData.Discounts),
		},
	}

	for _, product := range loadedData.Products {
		if !product.IsDeleted {
			preview.Counts.Products++
		}
	}
	for _, user := range loadedData.Users {
		preview.PasswordsKept = preview.PasswordsKept && isKeepableHash(user.PasswordHash)
	}
	for saleIndex := range loadedData.Sales {
		sale := loadedData.Sales[saleIndex]
		preview.Counts.SaleLines += len(sale.Lines)
		if sale.CreatedAt.IsZero() {
			continue
		}
		if preview.FirstSaleAt == nil || sale.CreatedAt.Before(*preview.FirstSaleAt) {
			preview.FirstSaleAt = &loadedData.Sales[saleIndex].CreatedAt
		}
		if preview.LastSaleAt == nil || sale.CreatedAt.After(*preview.LastSaleAt) {
			preview.LastSaleAt = &loadedData.Sales[saleIndex].CreatedAt
		}
	}

	for _, ownerChoice := range ownerChoicesOf(loadedData) {
		preview.OwnerChoices = append(preview.OwnerChoices, OldUserView{Id: ownerChoice.Id, Name: ownerChoice.Name, Email: ownerChoice.Email})
	}
	if len(preview.OwnerChoices) > 0 {
		preview.DefaultOwnerId = &preview.OwnerChoices[0].Id
	}

	return preview, nil
}

func (service *Service) Import(ctx context.Context, querier database.Querier, oldDatabasePath string, request ImportRequest) (ImportResultView, error) {
	configuredError := service.refuseWhenConfigured(ctx, querier)
	if configuredError != nil {
		return ImportResultView{}, configuredError
	}

	loadedData, oldSideTotals, readError := readOldFile(ctx, oldDatabasePath)
	if readError != nil {
		return ImportResultView{}, readError
	}

	ownerChoices := ownerChoicesOf(loadedData)
	if len(ownerChoices) == 0 {
		return ImportResultView{}, ErrNoOldUsers
	}
	chosenOwner := ownerChoices[0]
	if request.OwnerId != nil {
		isKnownChoice := false
		for _, ownerChoice := range ownerChoices {
			if ownerChoice.Id == *request.OwnerId {
				chosenOwner = ownerChoice
				isKnownChoice = true
			}
		}
		if !isKnownChoice {
			return ImportResultView{}, ErrUnknownOwner
		}
	}

	businessName := strings.TrimSpace(request.BusinessName)
	if businessName == "" {
		businessName = companyNameOf(loadedData)
	}
	if businessName == "" {
		return ImportResultView{}, ErrBusinessNameMissing
	}

	currencyCode, currencyDecimals := currencyOf(loadedData)
	run := &importRun{
		service:          service,
		querier:          querier,
		loadedData:       loadedData,
		currencyCode:     currencyCode,
		currencyDecimals: currencyDecimals,
		importedAt:       time.Now().UTC(),
		takenEmails:      map[string]bool{},
		takenSkus:        map[string]bool{},
		takenBarcodes:    map[string]bool{},
		takenReceipts:    map[string]bool{},
		takenRoleNames:   map[string]bool{"owner": true},
		userIdByOld:      map[int64]uuid.UUID{},
		productByOld:     map[int64]sales.PricingProduct{},
		notes:            map[string]int{},
		result: ImportResultView{
			PasswordsKept:      true,
			TemporaryPasswords: []TemporaryPasswordView{},
			RenamedSkus:        []RenamedView{},
			RenamedBarcodes:    []RenamedView{},
			ChangedEmails:      []RenamedView{},
			Notes:              []NoteView{},
		},
	}

	importSteps := []func(ctx context.Context) error{
		func(ctx context.Context) error { return run.createCompany(ctx, chosenOwner, businessName) },
		run.copySettings,
		func(ctx context.Context) error { return run.importUsers(ctx, chosenOwner) },
		run.importProducts,
		run.importDiscounts,
		run.importSales,
		run.importSuppliers,
	}
	for _, importStep := range importSteps {
		stepError := importStep(ctx)
		if stepError != nil {
			return ImportResultView{}, stepError
		}
	}

	checkError := run.checkAgainst(ctx, oldSideTotals)
	if checkError != nil {
		return ImportResultView{}, checkError
	}
	run.finishNotes()

	slog.Info("old app data import self-check",
		"matched", run.result.SelfCheck.Matched,
		"products", run.result.SelfCheck.Products,
		"users", run.result.SelfCheck.Users,
		"sales", run.result.SelfCheck.Sales,
		"saleLines", run.result.SelfCheck.SaleLines,
		"suppliers", run.result.SelfCheck.Suppliers,
		"salesValue", run.result.SelfCheck.SalesValue,
		"stockValue", run.result.SelfCheck.StockValue,
	)
	if !run.result.SelfCheck.Matched {
		return run.result, ErrImportMismatch
	}

	return run.result, nil
}

func (service *Service) refuseWhenConfigured(ctx context.Context, querier database.Querier) error {
	isConfigured, checkError := service.tenancyService.IsConfigured(ctx, querier)
	if checkError != nil {
		return checkError
	}
	if isConfigured {
		return tenancy.ErrAlreadyConfigured
	}
	return nil
}

func readOldFile(ctx context.Context, oldDatabasePath string) (oldData, oldTotals, error) {
	if !tenancy.OldDesktopDataExists(oldDatabasePath) {
		return oldData{}, oldTotals{}, ErrOldDataNotFound
	}

	reader, openError := openOldDatabase(ctx, oldDatabasePath)
	if openError != nil {
		return oldData{}, oldTotals{}, openError
	}
	defer reader.Close()

	loadedData, readError := reader.readAll(ctx)
	if readError != nil {
		return oldData{}, oldTotals{}, readError
	}

	_, currencyDecimals := currencyOf(loadedData)
	oldSideTotals, totalsError := reader.readTotals(ctx, currencyDecimals)
	if totalsError != nil {
		return oldData{}, oldTotals{}, totalsError
	}

	return loadedData, oldSideTotals, nil
}

type importRun struct {
	service          *Service
	querier          database.Querier
	loadedData       oldData
	currencyCode     string
	currencyDecimals int
	importedAt       time.Time
	companyId        uuid.UUID
	shopId           uuid.UUID
	ownerId          uuid.UUID
	taxBasisPoints   int
	fallbackRoleId   *uuid.UUID
	takenEmails      map[string]bool
	takenSkus        map[string]bool
	takenBarcodes    map[string]bool
	takenReceipts    map[string]bool
	takenRoleNames   map[string]bool
	userIdByOld      map[int64]uuid.UUID
	productByOld     map[int64]sales.PricingProduct
	oldProductById   map[int64]oldProduct
	importsSuppliers bool
	notes            map[string]int
	result           ImportResultView
}

func (run *importRun) createCompany(ctx context.Context, owner oldUser, businessName string) error {
	company := oldCompany{}
	if run.loadedData.Company != nil {
		company = *run.loadedData.Company
	}

	temporaryPassword, passwordError := newTemporaryPassword()
	if passwordError != nil {
		return passwordError
	}
	ownerEmail := run.assignEmail(owner)
	keepsOwnerPassword := isKeepableHash(owner.PasswordHash)
	currencyDecimals := run.currencyDecimals

	setupResult, setupError := run.service.tenancyService.CreateCompany(ctx, run.querier, false, tenancy.SetupRequest{
		BusinessName:     businessName,
		BusinessType:     strings.TrimSpace(company.BusinessType),
		Phone:            trimmedOrNil(company.Phone),
		Address:          trimmedOrNil(company.Address),
		Tin:              trimmedOrNil(company.Tin),
		CurrencyCode:     run.currencyCode,
		CurrencyDecimals: &currencyDecimals,
		OwnerName:        nameOrFallback(owner.Name, ownerEmail),
		OwnerEmail:       ownerEmail,
		OwnerPassword:    temporaryPassword,
		OwnerMustReset:   !keepsOwnerPassword,
	})
	if setupError != nil {
		return setupError
	}

	run.companyId = setupResult.CompanyId
	run.shopId = setupResult.ShopId
	run.ownerId = setupResult.UserId
	run.userIdByOld[owner.Id] = setupResult.UserId
	run.result.CompanyId = setupResult.CompanyId
	run.result.OwnerEmail = ownerEmail

	if !keepsOwnerPassword {
		run.rememberTemporaryPassword(nameOrFallback(owner.Name, ownerEmail), ownerEmail, temporaryPassword)
		return nil
	}
	return run.service.repository.SetPasswordHash(ctx, run.querier, run.companyId, run.ownerId, owner.PasswordHash)
}

func (run *importRun) copySettings(ctx context.Context) error {
	companySettings, findSettingsError := run.service.settingsRepository.FindSettings(ctx, run.querier, run.companyId)
	if findSettingsError != nil {
		return findSettingsError
	}
	run.taxBasisPoints = companySettings.TaxRateBasisPoints

	old := run.loadedData.Settings
	if old != nil {
		run.taxBasisPoints = clamp(int(math.Round(old.TaxRate*100)), 0, 10000)
		companySettings.TaxRateBasisPoints = run.taxBasisPoints
		if allowedDateFormats[strings.TrimSpace(old.DateFormat)] {
			companySettings.DateFormat = strings.TrimSpace(old.DateFormat)
		}
		companySettings.LowStockThreshold = max(old.LowStockThreshold, 0)
		companySettings.EmailNotificationsEnabled = old.EmailNotificationsEnabled
		companySettings.NotificationEmail = trimmedOrNil(old.NotificationEmail)
		companySettings.AlertSoundEnabled = old.AlertSoundEnabled
		companySettings.AlertOnLowStock = old.AlertOnLowStock
		companySettings.AlertOnOutOfStock = old.AlertOnOutOfStock
		companySettings.AlertOnDeadStock = old.AlertOnDeadStock
		companySettings.DeadStockDays = clamp(old.DeadStockDays, 1, 3650)
		companySettings.PrintReceiptAutomatically = old.PrintReceiptAutomatically
		companySettings.ShowTaxOnReceipt = old.ShowTaxOnReceipt
		companySettings.ShowBarcodesOnReceipt = old.ShowBarcodesOnReceipt
		companySettings.PrinterEnabled = old.PrinterEnabled
		companySettings.PrinterPort = strings.TrimSpace(old.PrinterPort)
		companySettings.PrinterModel = strings.TrimSpace(old.PrinterModel)
		if old.PrinterBaudRate > 0 {
			companySettings.PrinterBaudRate = old.PrinterBaudRate
		}
		if old.PrinterPaperWidth == 58 || old.PrinterPaperWidth == 80 {
			companySettings.PrinterPaperWidth = old.PrinterPaperWidth
		}
		companySettings.OpenCashDrawer = old.OpenCashDrawer
		if old.EfdEnabled {
			run.notes[noteEfdSettingsNotCopied] = 1
		}
	}

	saveSettingsError := run.service.settingsRepository.SaveSettings(ctx, run.querier, *companySettings)
	if saveSettingsError != nil {
		return saveSettingsError
	}

	if run.loadedData.Company == nil {
		return nil
	}
	companyProfile, findProfileError := run.service.settingsRepository.FindCompanyProfile(ctx, run.querier, run.companyId)
	if findProfileError != nil {
		return findProfileError
	}
	companyProfile.ReceiptHeader = trimmedOrNil(run.loadedData.Company.ReceiptHeader)
	companyProfile.ReceiptFooter = trimmedOrNil(run.loadedData.Company.ReceiptFooter)
	return run.service.settingsRepository.SaveCompanyProfile(ctx, run.querier, *companyProfile)
}

func (run *importRun) importUsers(ctx context.Context, owner oldUser) error {
	knownPermissions, permissionsError := run.service.accessRepository.ListPermissions(ctx, run.querier)
	if permissionsError != nil {
		return permissionsError
	}
	knownPermissionIds := map[string]bool{}
	for _, knownPermission := range knownPermissions {
		knownPermissionIds[knownPermission.Id] = true
	}

	roleIdByOld := map[int64]uuid.UUID{}
	oldRoleIds := make([]int64, 0, len(run.loadedData.Roles))
	for oldRoleId := range run.loadedData.Roles {
		oldRoleIds = append(oldRoleIds, oldRoleId)
	}
	sort.Slice(oldRoleIds, func(left int, right int) bool { return oldRoleIds[left] < oldRoleIds[right] })

	for _, oldRoleId := range oldRoleIds {
		oldRole := run.loadedData.Roles[oldRoleId]
		mappedPermissionIds := keepKnown(oldRole.PermissionIds, knownPermissionIds)
		if len(mappedPermissionIds) == 0 {
			continue
		}
		roleName := nameOrFallback(oldRole.Name, "Role "+strconv.FormatInt(oldRole.Id, 10))
		roleId, roleError := run.insertRole(ctx, roleName, mappedPermissionIds)
		if roleError != nil {
			return roleError
		}
		roleIdByOld[oldRole.Id] = roleId
	}

	for _, oldUser := range run.loadedData.Users {
		if oldUser.Id == owner.Id {
			continue
		}

		roleId, hasMappedRole := roleIdByOld[oldUser.RoleId]
		if !hasMappedRole {
			fallbackRoleId, fallbackError := run.cashierRoleId(ctx)
			if fallbackError != nil {
				return fallbackError
			}
			roleId = fallbackRoleId
		}

		userEmail := run.assignEmail(oldUser)
		userName := nameOrFallback(oldUser.Name, userEmail)
		passwordHash := oldUser.PasswordHash
		keepsPassword := isKeepableHash(passwordHash)
		if !keepsPassword {
			temporaryPassword, passwordError := newTemporaryPassword()
			if passwordError != nil {
				return passwordError
			}
			temporaryHash, hashError := bcrypt.GenerateFromPassword([]byte(temporaryPassword), bcrypt.DefaultCost)
			if hashError != nil {
				return fmt.Errorf("failed to hash temporary password: %w", hashError)
			}
			passwordHash = string(temporaryHash)
			run.rememberTemporaryPassword(userName, userEmail, temporaryPassword)
		}

		createdAt := timeOrFallback(oldUser.CreatedAt, run.importedAt)
		newUser := users.User{
			Id:                 uuid.Must(uuid.NewV7()),
			CompanyId:          run.companyId,
			RoleId:             roleId,
			Name:               userName,
			Email:              userEmail,
			PasswordHash:       passwordHash,
			IsActive:           true,
			MustChangePassword: !keepsPassword,
			CreatedAt:          createdAt,
			UpdatedAt:          createdAt,
		}
		insertError := run.service.usersRepository.Insert(ctx, run.querier, newUser)
		if insertError != nil {
			return insertError
		}
		run.userIdByOld[oldUser.Id] = newUser.Id

		_, shopsError := run.service.usersRepository.ReplaceShops(ctx, run.querier, run.companyId, newUser.Id, []uuid.UUID{run.shopId})
		if shopsError != nil {
			return shopsError
		}

		directPermissionIds := keepKnown(oldUser.PermissionIds, knownPermissionIds)
		if len(directPermissionIds) > 0 {
			grantError := run.service.accessRepository.ReplaceUserPermissions(ctx, run.querier, run.companyId, newUser.Id, directPermissionIds)
			if grantError != nil {
				return grantError
			}
		}
	}

	return nil
}

func (run *importRun) insertRole(ctx context.Context, roleName string, permissionIds []string) (uuid.UUID, error) {
	uniqueRoleName := roleName
	for suffixNumber := 2; run.takenRoleNames[strings.ToLower(uniqueRoleName)]; suffixNumber++ {
		uniqueRoleName = roleName + " (" + strconv.Itoa(suffixNumber) + ")"
	}
	run.takenRoleNames[strings.ToLower(uniqueRoleName)] = true

	newRole := access.Role{
		Id:        uuid.Must(uuid.NewV7()),
		CompanyId: run.companyId,
		Name:      uniqueRoleName,
		CreatedAt: run.importedAt,
		UpdatedAt: run.importedAt,
	}
	insertError := run.service.accessRepository.InsertRole(ctx, run.querier, newRole)
	if insertError != nil {
		return uuid.Nil, insertError
	}

	grantError := run.service.accessRepository.ReplaceRolePermissions(ctx, run.querier, run.companyId, newRole.Id, permissionIds)
	if grantError != nil {
		return uuid.Nil, grantError
	}

	return newRole.Id, nil
}

func (run *importRun) cashierRoleId(ctx context.Context) (uuid.UUID, error) {
	if run.fallbackRoleId != nil {
		return *run.fallbackRoleId, nil
	}
	cashierRoleId, roleError := run.insertRole(ctx, "Cashier", []string{"sales:create", "products:view"})
	if roleError != nil {
		return uuid.Nil, roleError
	}
	run.fallbackRoleId = &cashierRoleId
	return cashierRoleId, nil
}

func (run *importRun) assignEmail(user oldUser) string {
	normalizedEmail := users.NormalizeEmail(user.Email)
	atIndex := strings.Index(normalizedEmail, "@")
	assignedEmail := normalizedEmail
	if atIndex < 1 {
		assignedEmail = "user" + strconv.FormatInt(user.Id, 10) + "@balce.local"
	} else if run.takenEmails[normalizedEmail] {
		assignedEmail = normalizedEmail[:atIndex] + "+" + strconv.FormatInt(user.Id, 10) + normalizedEmail[atIndex:]
	}
	run.takenEmails[assignedEmail] = true

	if assignedEmail != normalizedEmail {
		run.result.ChangedEmails = append(run.result.ChangedEmails, RenamedView{Name: strings.TrimSpace(user.Name), Old: user.Email, New: assignedEmail})
	}
	return assignedEmail
}

func (run *importRun) rememberTemporaryPassword(userName string, userEmail string, temporaryPassword string) {
	run.result.PasswordsKept = false
	run.result.TemporaryPasswords = append(run.result.TemporaryPasswords, TemporaryPasswordView{Name: userName, Email: userEmail, Password: temporaryPassword})
}

func (run *importRun) importProducts(ctx context.Context) error {
	liveProducts := []oldProduct{}
	run.oldProductById = map[int64]oldProduct{}
	for _, product := range run.loadedData.Products {
		run.oldProductById[product.Id] = product
		if !product.IsDeleted {
			liveProducts = append(liveProducts, product)
		}
	}
	sort.SliceStable(liveProducts, func(left int, right int) bool {
		return liveProducts[left].ParentId == nil && liveProducts[right].ParentId != nil
	})

	barcodesByProduct := map[int64][]oldBarcode{}
	for _, barcode := range run.loadedData.Barcodes {
		barcodesByProduct[barcode.ProductId] = append(barcodesByProduct[barcode.ProductId], barcode)
	}
	addonsByProduct := map[int64][]oldAddon{}
	for _, addon := range run.loadedData.Addons {
		addonsByProduct[addon.ProductId] = append(addonsByProduct[addon.ProductId], addon)
	}

	for _, product := range liveProducts {
		importedProduct, insertError := run.insertProduct(ctx, product, true)
		if insertError != nil {
			return insertError
		}

		productBarcodes := barcodesByProduct[product.Id]
		if product.Barcode != nil {
			productBarcodes = append([]oldBarcode{{ProductId: product.Id, Code: *product.Barcode, PackSize: 1}}, productBarcodes...)
		}
		barcodesError := run.insertBarcodes(ctx, product, importedProduct.Id, productBarcodes)
		if barcodesError != nil {
			return barcodesError
		}

		addonsError := run.insertAddons(ctx, importedProduct.Id, addonsByProduct[product.Id])
		if addonsError != nil {
			return addonsError
		}

		stockError := run.openStock(ctx, product, importedProduct.Id)
		if stockError != nil {
			return stockError
		}

		if product.Image != nil && strings.TrimSpace(*product.Image) != "" {
			run.notes[noteProductImagesNotCopied]++
		}
	}

	return nil
}

func (run *importRun) insertProduct(ctx context.Context, product oldProduct, isActive bool) (sales.PricingProduct, error) {
	originalSku := strings.TrimSpace(product.Sku)
	skuBase := originalSku
	if skuBase == "" {
		skuBase = "OLD-" + strconv.FormatInt(product.Id, 10)
	}
	uniqueSku := uniqueValue(skuBase, run.takenSkus)
	productName := nameOrFallback(product.Name, "Product "+strconv.FormatInt(product.Id, 10))
	if uniqueSku != originalSku && isActive {
		run.result.RenamedSkus = append(run.result.RenamedSkus, RenamedView{Name: productName, Old: product.Sku, New: uniqueSku})
	}

	var parentId *uuid.UUID
	if product.ParentId != nil && isActive {
		parentProduct, isParentImported := run.productByOld[*product.ParentId]
		if isParentImported {
			parentId = &parentProduct.Id
		}
	}

	var wholesalePrice *int64
	if product.WholesalePrice != nil {
		wholesaleMinor := max(toMinorUnits(*product.WholesalePrice, run.currencyDecimals), 0)
		wholesalePrice = &wholesaleMinor
	}
	wholesaleMinimum := product.WholesaleMin
	if wholesaleMinimum < 1 {
		wholesaleMinimum = 10
	}

	createdAt := timeOrFallback(product.CreatedAt, run.importedAt)
	newProduct := products.Product{
		Id:             uuid.Must(uuid.NewV7()),
		CompanyId:      run.companyId,
		ParentId:       parentId,
		Sku:            uniqueSku,
		Name:           productName,
		VariantLabel:   strings.TrimSpace(product.VariantLabel),
		Price:          max(toMinorUnits(product.Price, run.currencyDecimals), 0),
		CostPrice:      max(toMinorUnits(product.CostPrice, run.currencyDecimals), 0),
		WholesalePrice: wholesalePrice,
		WholesaleMin:   wholesaleMinimum,
		Category:       trimmedOrNil(product.Category),
		Unit:           nameOrFallback(product.Unit, "pcs"),
		PiecesPerUnit:  max(product.PiecesPerUnit, 1),
		Metadata:       metadataObject(product.Metadata),
		IsActive:       isActive,
		CreatedAt:      createdAt,
		UpdatedAt:      createdAt,
	}
	insertError := run.service.productsRepository.Insert(ctx, run.querier, newProduct)
	if insertError != nil {
		return sales.PricingProduct{}, insertError
	}

	importedProduct := sales.PricingProduct{
		Id:           newProduct.Id,
		Name:         newProduct.Name,
		VariantLabel: newProduct.VariantLabel,
		Sku:          newProduct.Sku,
		Unit:         newProduct.Unit,
		Price:        newProduct.Price,
		CostPrice:    newProduct.CostPrice,
	}
	run.productByOld[product.Id] = importedProduct
	return importedProduct, nil
}

func (run *importRun) insertBarcodes(ctx context.Context, product oldProduct, productId uuid.UUID, oldBarcodes []oldBarcode) error {
	seenCodes := map[string]bool{}
	newBarcodes := []products.Barcode{}
	for _, barcode := range oldBarcodes {
		originalCode := strings.TrimSpace(barcode.Code)
		if originalCode == "" || seenCodes[originalCode] {
			continue
		}
		seenCodes[originalCode] = true

		uniqueCode := uniqueValue(originalCode, run.takenBarcodes)
		if uniqueCode != originalCode {
			run.result.RenamedBarcodes = append(run.result.RenamedBarcodes, RenamedView{Name: nameOrFallback(product.Name, product.Sku), Old: originalCode, New: uniqueCode})
		}
		newBarcodes = append(newBarcodes, products.Barcode{
			Id:        uuid.Must(uuid.NewV7()),
			CompanyId: run.companyId,
			ProductId: productId,
			Code:      uniqueCode,
			PackSize:  max(barcode.PackSize, 1),
			CreatedAt: run.importedAt,
		})
	}

	if len(newBarcodes) == 0 {
		return nil
	}
	return run.service.productsRepository.ReplaceBarcodes(ctx, run.querier, run.companyId, productId, newBarcodes)
}

func (run *importRun) insertAddons(ctx context.Context, productId uuid.UUID, oldAddons []oldAddon) error {
	takenAddonNames := map[string]bool{}
	for addonIndex, addon := range oldAddons {
		addonName := uniqueValue(nameOrFallback(addon.Name, "Add-on "+strconv.Itoa(addonIndex+1)), takenAddonNames)
		insertError := run.service.productsRepository.InsertAddon(ctx, run.querier, products.Addon{
			Id:        uuid.Must(uuid.NewV7()),
			CompanyId: run.companyId,
			ProductId: productId,
			Name:      addonName,
			Price:     max(toMinorUnits(addon.Price, run.currencyDecimals), 0),
			IsActive:  addon.IsActive,
			CreatedAt: run.importedAt,
			UpdatedAt: run.importedAt,
		})
		if insertError != nil {
			return insertError
		}
	}
	return nil
}

func (run *importRun) openStock(ctx context.Context, product oldProduct, productId uuid.UUID) error {
	ensureError := run.service.stockRepository.EnsureShopStock(ctx, run.querier, run.companyId, run.shopId, productId, max(product.MinStock, 0))
	if ensureError != nil {
		return ensureError
	}

	if product.Quantity < 0 {
		run.notes[noteNegativeStockZeroed]++
	}
	if product.Quantity <= 0 {
		return nil
	}

	openingReference := openingStockReference
	_, movementError := run.service.stockService.RecordMovement(ctx, run.querier, stock.MovementRequest{
		CompanyId: run.companyId,
		ShopId:    run.shopId,
		ProductId: productId,
		Change:    product.Quantity,
		Reason:    "opening",
		Reference: &openingReference,
		UserId:    &run.ownerId,
	})
	return movementError
}

func (run *importRun) productForHistory(ctx context.Context, oldProductId int64) (sales.PricingProduct, error) {
	importedProduct, isImported := run.productByOld[oldProductId]
	if isImported {
		return importedProduct, nil
	}

	run.notes[noteHistoryProductsHidden]++
	deletedProduct, isDeletedProduct := run.oldProductById[oldProductId]
	if isDeletedProduct {
		return run.insertProduct(ctx, deletedProduct, false)
	}

	removedProduct := oldProduct{
		Id:   oldProductId,
		Name: "Removed product " + strconv.FormatInt(oldProductId, 10),
		Sku:  "OLD-REMOVED-" + strconv.FormatInt(oldProductId, 10),
		Unit: "pcs",
	}
	return run.insertProduct(ctx, removedProduct, false)
}

func (run *importRun) importDiscounts(ctx context.Context) error {
	for _, discount := range run.loadedData.Discounts {
		var productId *uuid.UUID
		if discount.ProductId != nil {
			importedProduct, isImported := run.productByOld[*discount.ProductId]
			if !isImported {
				run.notes[noteDiscountsSkipped]++
				continue
			}
			productId = &importedProduct.Id
		}

		discountKind := strings.ToLower(strings.TrimSpace(discount.DiscountType))
		discountValue := int64(0)
		switch discountKind {
		case "percent", "percentage":
			discountKind = "percent"
			discountValue = int64(math.Round(discount.Value * 100))
		case "fixed":
			discountValue = toMinorUnits(discount.Value, run.currencyDecimals)
		}
		isValidValue := (discountKind == "percent" && discountValue >= 1 && discountValue <= 10000) || (discountKind == "fixed" && discountValue > 0)
		hasValidDates := !discount.StartsAt.IsZero() && discount.EndsAt.After(discount.StartsAt)
		if !isValidValue || !hasValidDates {
			run.notes[noteDiscountsSkipped]++
			continue
		}

		var createdBy *uuid.UUID
		if discount.CreatedBy != nil {
			creatorId, isKnownCreator := run.userIdByOld[*discount.CreatedBy]
			if isKnownCreator {
				createdBy = &creatorId
			}
		}

		insertError := run.service.discountsRepository.Insert(ctx, run.querier, discounts.Discount{
			Id:        uuid.Must(uuid.NewV7()),
			CompanyId: run.companyId,
			Name:      nameOrFallback(discount.Name, "Discount"),
			ProductId: productId,
			Kind:      discountKind,
			Value:     discountValue,
			StartsAt:  discount.StartsAt,
			EndsAt:    discount.EndsAt,
			IsActive:  discount.IsActive,
			CreatedBy: createdBy,
			CreatedAt: run.importedAt,
			UpdatedAt: run.importedAt,
		})
		if insertError != nil {
			return insertError
		}
	}
	return nil
}

func (run *importRun) importSales(ctx context.Context) error {
	for _, oldSale := range run.loadedData.Sales {
		pricedLines := make([]sales.PricedLine, 0, len(oldSale.Lines))
		linesSubtotal := int64(0)
		for _, oldLine := range oldSale.Lines {
			historyProduct, productError := run.productForHistory(ctx, oldLine.ProductId)
			if productError != nil {
				return productError
			}
			unitPrice := max(toMinorUnits(oldLine.UnitPrice, run.currencyDecimals), 0)
			lineTotal := unitPrice * int64(oldLine.Quantity)
			linesSubtotal += lineTotal
			pricedLines = append(pricedLines, sales.PricedLine{
				Product:     historyProduct,
				Quantity:    oldLine.Quantity,
				UnitPrice:   unitPrice,
				IsWholesale: oldLine.IsWholesale,
				LineTotal:   lineTotal,
			})
		}

		saleTotal := toMinorUnits(oldSale.TotalAmount, run.currencyDecimals)
		discountTotal := linesSubtotal - saleTotal
		if discountTotal < 0 {
			linesSubtotal = saleTotal
			discountTotal = 0
		}

		paymentMethod := paymentMethodOf(oldSale.PaymentType)
		amountPaid := saleTotal
		changeGiven := int64(0)
		tenderedAmount := toMinorUnits(oldSale.AmountPaid, run.currencyDecimals)
		if paymentMethod == sales.PaymentCash && tenderedAmount > saleTotal {
			amountPaid = tenderedAmount
			changeGiven = tenderedAmount - saleTotal
		}

		saleUserId, isKnownUser := run.userIdByOld[oldSale.UserId]
		if !isKnownUser {
			saleUserId = run.ownerId
		}

		receiptBase := "OLD-" + strings.TrimSpace(oldSale.ReceiptNumber)
		if strings.TrimSpace(oldSale.ReceiptNumber) == "" {
			receiptBase = "OLD-" + strconv.FormatInt(oldSale.Id, 10)
		}

		newSale := sales.Sale{
			Id:                 uuid.Must(uuid.NewV7()),
			CompanyId:          run.companyId,
			ShopId:             run.shopId,
			UserId:             saleUserId,
			ClientRef:          "old-sale-" + strconv.FormatInt(oldSale.Id, 10),
			RequestHash:        importRequestHash,
			ReceiptNumber:      uniqueValue(receiptBase, run.takenReceipts),
			Subtotal:           linesSubtotal,
			DiscountTotal:      discountTotal,
			Total:              saleTotal,
			TaxTotal:           max(toMinorUnits(oldSale.TaxAmount, run.currencyDecimals), 0),
			TaxRateBasisPoints: run.taxBasisPoints,
			AmountPaid:         amountPaid,
			ChangeGiven:        changeGiven,
			CurrencyCode:       run.currencyCode,
			CurrencyDecimals:   run.currencyDecimals,
			CreatedAt:          timeOrFallback(oldSale.CreatedAt, run.importedAt),
		}
		insertSaleError := run.service.salesRepository.InsertSale(ctx, run.querier, newSale)
		if insertSaleError != nil {
			return insertSaleError
		}

		insertLinesError := run.service.salesRepository.InsertLines(ctx, run.querier, run.companyId, newSale.Id, pricedLines)
		if insertLinesError != nil {
			return insertLinesError
		}

		if amountPaid <= 0 {
			continue
		}
		insertPaymentsError := run.service.salesRepository.InsertPayments(ctx, run.querier, run.companyId, newSale.Id, []sales.Payment{{Method: paymentMethod, Amount: amountPaid}})
		if insertPaymentsError != nil {
			return insertPaymentsError
		}
	}
	return nil
}

func (run *importRun) importSuppliers(ctx context.Context) error {
	if len(run.loadedData.Suppliers) == 0 {
		return nil
	}

	supplierColumns, columnsError := run.service.repository.ListSupplierColumns(ctx, run.querier)
	if columnsError != nil {
		return columnsError
	}

	fillableColumns := map[string]bool{"id": true, "company_id": true, "name": true, "phone": true, "notes": true, "is_active": true, "created_at": true, "updated_at": true}
	usableColumns := []string{}
	usableColumnSet := map[string]bool{}
	canFillTable := len(supplierColumns) > 0
	for _, supplierColumn := range supplierColumns {
		if fillableColumns[supplierColumn.Name] {
			usableColumns = append(usableColumns, supplierColumn.Name)
			usableColumnSet[supplierColumn.Name] = true
			continue
		}
		canFillTable = canFillTable && !supplierColumn.IsRequired
	}
	hasKeyColumns := usableColumnSet["id"] && usableColumnSet["company_id"] && usableColumnSet["name"]
	if !canFillTable || !hasKeyColumns {
		run.notes[noteSuppliersNotImported] = len(run.loadedData.Suppliers)
		return nil
	}

	for supplierIndex, supplier := range run.loadedData.Suppliers {
		createdAt := timeOrFallback(supplier.CreatedAt, run.importedAt)
		valuesByColumn := map[string]any{
			"id":         uuid.Must(uuid.NewV7()),
			"company_id": run.companyId,
			"name":       nameOrFallback(supplier.Name, "Supplier "+strconv.Itoa(supplierIndex+1)),
			"phone":      trimmedOrNil(supplier.Phone),
			"notes":      trimmedOrNil(supplier.Notes),
			"is_active":  true,
			"created_at": createdAt,
			"updated_at": createdAt,
		}
		columnValues := make([]any, 0, len(usableColumns))
		for _, columnName := range usableColumns {
			columnValues = append(columnValues, valuesByColumn[columnName])
		}
		insertError := run.service.repository.InsertSupplier(ctx, run.querier, usableColumns, columnValues)
		if insertError != nil {
			return insertError
		}
	}

	run.importsSuppliers = true
	return nil
}

func (run *importRun) checkAgainst(ctx context.Context, oldSideTotals oldTotals) error {
	newSideTotals, countError := run.service.repository.CountImported(ctx, run.querier, run.companyId, run.importsSuppliers)
	if countError != nil {
		return countError
	}

	selfCheck := SelfCheckView{
		Products:   compareCounts(oldSideTotals.Products, newSideTotals.Products),
		Users:      compareCounts(oldSideTotals.Users, newSideTotals.Users),
		Sales:      compareCounts(oldSideTotals.Sales, newSideTotals.Sales),
		SaleLines:  compareCounts(oldSideTotals.SaleLines, newSideTotals.SaleLines),
		SalesValue: compareCounts(oldSideTotals.SalesValue, newSideTotals.SalesValue),
		StockValue: compareCounts(oldSideTotals.StockValue, newSideTotals.StockValue),
	}
	selfCheck.Matched = selfCheck.Products.Matched && selfCheck.Users.Matched && selfCheck.Sales.Matched &&
		selfCheck.SaleLines.Matched && selfCheck.SalesValue.Matched && selfCheck.StockValue.Matched
	if run.importsSuppliers {
		supplierCheck := compareCounts(oldSideTotals.Suppliers, newSideTotals.Suppliers)
		selfCheck.Suppliers = &supplierCheck
		selfCheck.Matched = selfCheck.Matched && supplierCheck.Matched
	}

	run.result.SelfCheck = selfCheck
	return nil
}

func (run *importRun) finishNotes() {
	if run.loadedData.PurchaseCount > 0 {
		run.notes[notePurchasesNotImported] = int(run.loadedData.PurchaseCount)
	}

	noteCodes := make([]string, 0, len(run.notes))
	for noteCode := range run.notes {
		noteCodes = append(noteCodes, noteCode)
	}
	sort.Strings(noteCodes)
	for _, noteCode := range noteCodes {
		run.result.Notes = append(run.result.Notes, NoteView{Code: noteCode, Count: run.notes[noteCode]})
	}
}

func compareCounts(oldValue int64, newValue int64) CountCheckView {
	return CountCheckView{Old: oldValue, New: newValue, Matched: oldValue == newValue}
}

func ownerChoicesOf(loadedData oldData) []oldUser {
	adminUsers := []oldUser{}
	for _, user := range loadedData.Users {
		userRole := loadedData.Roles[user.RoleId]
		if strings.EqualFold(strings.TrimSpace(userRole.Name), "admin") {
			adminUsers = append(adminUsers, user)
		}
	}
	if len(adminUsers) > 0 {
		return adminUsers
	}
	return loadedData.Users
}

func companyNameOf(loadedData oldData) string {
	if loadedData.Company == nil {
		return ""
	}
	return strings.TrimSpace(loadedData.Company.Name)
}

func currencyOf(loadedData oldData) (string, int) {
	currencyCode := "TZS"
	if loadedData.Settings != nil {
		oldCurrency := strings.ToUpper(strings.TrimSpace(loadedData.Settings.Currency))
		isLetterCode := len(oldCurrency) == 3 && strings.Trim(oldCurrency, "ABCDEFGHIJKLMNOPQRSTUVWXYZ") == ""
		if isLetterCode && oldCurrency != "TSH" {
			currencyCode = oldCurrency
		}
	}
	if zeroDecimalCurrencies[currencyCode] {
		return currencyCode, 0
	}
	return currencyCode, 2
}

func paymentMethodOf(oldPaymentType string) string {
	switch strings.ToLower(strings.TrimSpace(oldPaymentType)) {
	case "card", "bank", "pos":
		return sales.PaymentCard
	case "mobile", "mobile_money", "mpesa", "m-pesa", "tigopesa", "airtel", "halopesa":
		return sales.PaymentMobile
	default:
		return sales.PaymentCash
	}
}

func toMinorUnits(amount float64, currencyDecimals int) int64 {
	return int64(math.Round(amount * math.Pow10(currencyDecimals)))
}

func isKeepableHash(passwordHash string) bool {
	_, costError := bcrypt.Cost([]byte(passwordHash))
	return costError == nil
}

func newTemporaryPassword() (string, error) {
	passwordLetters := make([]byte, temporaryPasswordLength)
	alphabetSize := big.NewInt(int64(len(temporaryPasswordAlphabet)))
	for letterIndex := range passwordLetters {
		randomIndex, randomError := rand.Int(rand.Reader, alphabetSize)
		if randomError != nil {
			return "", fmt.Errorf("failed to make a temporary password: %w", randomError)
		}
		passwordLetters[letterIndex] = temporaryPasswordAlphabet[randomIndex.Int64()]
	}
	return string(passwordLetters), nil
}

func uniqueValue(baseValue string, takenValues map[string]bool) string {
	uniqueCandidate := baseValue
	for suffixNumber := 2; takenValues[uniqueCandidate]; suffixNumber++ {
		uniqueCandidate = baseValue + "-" + strconv.Itoa(suffixNumber)
	}
	takenValues[uniqueCandidate] = true
	return uniqueCandidate
}

func keepKnown(permissionIds []string, knownPermissionIds map[string]bool) []string {
	keptPermissionIds := []string{}
	seenPermissionIds := map[string]bool{}
	for _, permissionId := range permissionIds {
		if knownPermissionIds[permissionId] && !seenPermissionIds[permissionId] {
			seenPermissionIds[permissionId] = true
			keptPermissionIds = append(keptPermissionIds, permissionId)
		}
	}
	return keptPermissionIds
}

func metadataObject(oldMetadata *string) []byte {
	if oldMetadata == nil {
		return []byte("{}")
	}
	metadataFields := map[string]any{}
	decodeError := json.Unmarshal([]byte(*oldMetadata), &metadataFields)
	if decodeError != nil || metadataFields == nil {
		return []byte("{}")
	}
	return []byte(*oldMetadata)
}

func trimmedOrNil(value *string) *string {
	if value == nil {
		return nil
	}
	trimmedValue := strings.TrimSpace(*value)
	if trimmedValue == "" {
		return nil
	}
	return &trimmedValue
}

func nameOrFallback(name string, fallback string) string {
	trimmedName := strings.TrimSpace(name)
	if trimmedName == "" {
		return fallback
	}
	return trimmedName
}

func timeOrFallback(value time.Time, fallback time.Time) time.Time {
	if value.IsZero() {
		return fallback
	}
	return value
}

func clamp(value int, lowest int, highest int) int {
	return min(max(value, lowest), highest)
}
