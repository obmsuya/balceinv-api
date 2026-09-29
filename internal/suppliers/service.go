package suppliers

import (
	"context"
	"net/mail"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/accounting"
	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/identity"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/chrisostomemataba/balceinv-api/internal/common/storage"
	"github.com/chrisostomemataba/balceinv-api/internal/features"
	"github.com/chrisostomemataba/balceinv-api/internal/settings"
	"github.com/chrisostomemataba/balceinv-api/internal/stock"
	"github.com/google/uuid"
)

type Service struct {
	repository         *Repository
	featuresRepository *features.Repository
	settingsRepository *settings.Repository
	stockService       *stock.Service
	objectStore        storage.Store
	ledger             *accounting.Ledger
}

func NewService(repository *Repository, featuresRepository *features.Repository, settingsRepository *settings.Repository, stockService *stock.Service, objectStore storage.Store, ledger *accounting.Ledger) *Service {
	return &Service{
		repository:         repository,
		featuresRepository: featuresRepository,
		settingsRepository: settingsRepository,
		stockService:       stockService,
		objectStore:        objectStore,
		ledger:             ledger,
	}
}

func suppliersRule(companyFeatures features.Features) bool {
	return companyFeatures.SuppliersEnabled
}

func purchasesRule(companyFeatures features.Features) bool {
	return companyFeatures.SuppliersEnabled || companyFeatures.AccountingEnabled()
}

func ordersRule(companyFeatures features.Features) bool {
	return companyFeatures.SuppliersEnabled && companyFeatures.PurchaseOrdersEnabled
}

func (service *Service) requireFeature(ctx context.Context, querier database.Querier, companyId uuid.UUID, isAllowed func(features.Features) bool) (features.Features, error) {
	companyFeatures, findError := service.featuresRepository.Find(ctx, querier, companyId)
	if findError != nil {
		return features.Features{}, findError
	}
	if !isAllowed(companyFeatures) {
		return features.Features{}, ErrFeatureOff
	}
	return companyFeatures, nil
}

func (service *Service) companyLocation(ctx context.Context, querier database.Querier, companyId uuid.UUID) (*time.Location, error) {
	companyProfile, profileError := service.settingsRepository.FindCompanyProfile(ctx, querier, companyId)
	if profileError != nil {
		return nil, profileError
	}
	if companyProfile == nil {
		return nil, ErrMissingSettings
	}
	companyLocation, locationError := time.LoadLocation(companyProfile.Timezone)
	if locationError != nil {
		return time.UTC, nil
	}
	return companyLocation, nil
}

func (service *Service) resolveShop(ctx context.Context, querier database.Querier, principal *identity.Principal, requestedShopId *string) (uuid.UUID, error) {
	shopId := principal.ShopId
	if requestedShopId != nil {
		parsedShopId := uuid.MustParse(*requestedShopId)
		shopId = &parsedShopId
	}
	if shopId == nil {
		return uuid.Nil, ErrNoActiveShop
	}

	isOpenShop, findError := service.repository.FindOpenShop(ctx, querier, principal.CompanyId, *shopId)
	if findError != nil {
		return uuid.Nil, findError
	}
	if !isOpenShop {
		return uuid.Nil, ErrShopNotFound
	}

	if !principal.IsOwner {
		isAssigned, assignedError := service.repository.IsAssignedToShop(ctx, querier, principal.CompanyId, principal.UserId, *shopId)
		if assignedError != nil {
			return uuid.Nil, assignedError
		}
		if !isAssigned {
			return uuid.Nil, ErrShopNotAssigned
		}
	}

	return *shopId, nil
}

func (service *Service) findUsableSupplier(ctx context.Context, querier database.Querier, companyId uuid.UUID, supplierId uuid.UUID, mustBeActive bool) (*Supplier, error) {
	foundSupplier, findError := service.repository.FindSupplier(ctx, querier, companyId, supplierId)
	if findError != nil {
		return nil, findError
	}
	if foundSupplier == nil {
		return nil, ErrSupplierNotFound
	}
	if mustBeActive && !foundSupplier.IsActive {
		return nil, ErrSupplierInactive
	}
	return foundSupplier, nil
}

func (service *Service) ListSuppliers(ctx context.Context, querier database.Querier, principal *identity.Principal, filter SupplierFilter, limit int, offset int) (response.Page[SupplierView], error) {
	_, featureError := service.requireFeature(ctx, querier, principal.CompanyId, suppliersRule)
	if featureError != nil {
		return response.Page[SupplierView]{}, featureError
	}

	totalSuppliers, countError := service.repository.CountSuppliers(ctx, querier, principal.CompanyId, filter)
	if countError != nil {
		return response.Page[SupplierView]{}, countError
	}
	pageSuppliers, listError := service.repository.ListSuppliers(ctx, querier, principal.CompanyId, filter, limit, offset)
	if listError != nil {
		return response.Page[SupplierView]{}, listError
	}

	supplierViews, viewError := service.withBalances(ctx, querier, principal.CompanyId, pageSuppliers, false)
	if viewError != nil {
		return response.Page[SupplierView]{}, viewError
	}

	supplierPage := response.Page[SupplierView]{
		Items:  supplierViews,
		Total:  totalSuppliers,
		Limit:  limit,
		Offset: offset,
	}
	return supplierPage, nil
}

func (service *Service) GetSupplier(ctx context.Context, querier database.Querier, principal *identity.Principal, supplierId uuid.UUID) (SupplierView, error) {
	_, featureError := service.requireFeature(ctx, querier, principal.CompanyId, suppliersRule)
	if featureError != nil {
		return SupplierView{}, featureError
	}
	return service.supplierView(ctx, querier, principal.CompanyId, supplierId)
}

func (service *Service) supplierView(ctx context.Context, querier database.Querier, companyId uuid.UUID, supplierId uuid.UUID) (SupplierView, error) {
	foundSupplier, findError := service.findUsableSupplier(ctx, querier, companyId, supplierId, false)
	if findError != nil {
		return SupplierView{}, findError
	}

	supplierViews, viewError := service.withBalances(ctx, querier, companyId, []Supplier{*foundSupplier}, true)
	if viewError != nil {
		return SupplierView{}, viewError
	}
	return supplierViews[0], nil
}

func (service *Service) withBalances(ctx context.Context, querier database.Querier, companyId uuid.UUID, pageSuppliers []Supplier, includeAging bool) ([]SupplierView, error) {
	companyLocation, locationError := service.companyLocation(ctx, querier, companyId)
	if locationError != nil {
		return nil, locationError
	}
	ledgers, ledgerError := service.repository.LoadLedgers(ctx, querier, companyId, pageSuppliers, true)
	if ledgerError != nil {
		return nil, ledgerError
	}

	today := civilDate(time.Now(), companyLocation)
	supplierViews := make([]SupplierView, 0, len(pageSuppliers))
	for _, pageSupplier := range pageSuppliers {
		openDebits, unappliedCredit := Allocate(ledgers[pageSupplier.Id])
		agingBuckets := Age(openDebits, pageSupplier.PaymentTermsDays, today, companyLocation)
		supplierView := toSupplierView(pageSupplier)
		supplierView.Balance = Balance(openDebits, unappliedCredit)
		supplierView.OverdueAmount = agingBuckets.Overdue()
		if includeAging {
			supplierView.Aging = &agingBuckets
		}
		supplierViews = append(supplierViews, supplierView)
	}
	return supplierViews, nil
}

func (service *Service) CreateSupplier(ctx context.Context, querier database.Querier, principal *identity.Principal, request SupplierRequest) (SupplierView, error) {
	_, featureError := service.requireFeature(ctx, querier, principal.CompanyId, suppliersRule)
	if featureError != nil {
		return SupplierView{}, featureError
	}

	createdAt := time.Now().UTC()
	newSupplier, buildError := buildSupplier(Supplier{
		Id:        uuid.Must(uuid.NewV7()),
		CompanyId: principal.CompanyId,
		IsActive:  true,
		CreatedBy: &principal.UserId,
		CreatedAt: createdAt,
	}, request)
	if buildError != nil {
		return SupplierView{}, buildError
	}
	newSupplier.UpdatedBy = &principal.UserId
	newSupplier.UpdatedAt = createdAt

	insertError := service.repository.InsertSupplier(ctx, querier, newSupplier)
	if database.IsUniqueViolation(insertError) {
		return SupplierView{}, ErrSupplierNameTaken
	}
	if insertError != nil {
		return SupplierView{}, insertError
	}
	openingError := service.ledger.SyncSupplierOpening(ctx, querier, principal.CompanyId, newSupplier.Id)
	if openingError != nil {
		return SupplierView{}, openingError
	}

	return service.supplierView(ctx, querier, principal.CompanyId, newSupplier.Id)
}

func (service *Service) UpdateSupplier(ctx context.Context, querier database.Querier, principal *identity.Principal, supplierId uuid.UUID, request SupplierRequest) (SupplierView, error) {
	_, featureError := service.requireFeature(ctx, querier, principal.CompanyId, suppliersRule)
	if featureError != nil {
		return SupplierView{}, featureError
	}

	existingSupplier, findError := service.findUsableSupplier(ctx, querier, principal.CompanyId, supplierId, false)
	if findError != nil {
		return SupplierView{}, findError
	}

	changedSupplier, buildError := buildSupplier(*existingSupplier, request)
	if buildError != nil {
		return SupplierView{}, buildError
	}
	if request.IsActive != nil {
		changedSupplier.IsActive = *request.IsActive
	}
	changedSupplier.UpdatedBy = &principal.UserId
	changedSupplier.UpdatedAt = time.Now().UTC()

	updateError := service.repository.UpdateSupplier(ctx, querier, changedSupplier)
	if database.IsUniqueViolation(updateError) {
		return SupplierView{}, ErrSupplierNameTaken
	}
	if updateError != nil {
		return SupplierView{}, updateError
	}
	openingError := service.ledger.SyncSupplierOpening(ctx, querier, principal.CompanyId, supplierId)
	if openingError != nil {
		return SupplierView{}, openingError
	}

	return service.supplierView(ctx, querier, principal.CompanyId, supplierId)
}

func (service *Service) DeactivateSupplier(ctx context.Context, querier database.Querier, principal *identity.Principal, supplierId uuid.UUID) error {
	_, featureError := service.requireFeature(ctx, querier, principal.CompanyId, suppliersRule)
	if featureError != nil {
		return featureError
	}

	_, findError := service.findUsableSupplier(ctx, querier, principal.CompanyId, supplierId, false)
	if findError != nil {
		return findError
	}
	return service.repository.SetSupplierActive(ctx, querier, principal.CompanyId, supplierId, false, principal.UserId)
}

func (service *Service) Aging(ctx context.Context, querier database.Querier, principal *identity.Principal, asOfText string) (AgingView, error) {
	_, featureError := service.requireFeature(ctx, querier, principal.CompanyId, suppliersRule)
	if featureError != nil {
		return AgingView{}, featureError
	}
	companyLocation, locationError := service.companyLocation(ctx, querier, principal.CompanyId)
	if locationError != nil {
		return AgingView{}, locationError
	}

	asOfDate := civilDate(time.Now(), companyLocation)
	if asOfText != "" {
		parsedDate, parseError := time.Parse(dateLayout, asOfText)
		if parseError != nil {
			return AgingView{}, ErrInvalidDate
		}
		asOfDate = parsedDate
	}
	asOfYear, asOfMonth, asOfDay := asOfDate.Date()
	endOfAsOfDay := time.Date(asOfYear, asOfMonth, asOfDay+1, 0, 0, 0, 0, companyLocation)

	allSuppliers, listError := service.repository.ListAllSuppliers(ctx, querier, principal.CompanyId)
	if listError != nil {
		return AgingView{}, listError
	}
	ledgers, ledgerError := service.repository.LoadLedgers(ctx, querier, principal.CompanyId, allSuppliers, false)
	if ledgerError != nil {
		return AgingView{}, ledgerError
	}

	agingView := AgingView{
		AsOf:      asOfDate.Format(dateLayout),
		Suppliers: []SupplierAgingView{},
	}
	for _, listedSupplier := range allSuppliers {
		entriesUpToDate := []LedgerEntry{}
		for _, entry := range ledgers[listedSupplier.Id] {
			if entry.DatedAt.Before(endOfAsOfDay) {
				entriesUpToDate = append(entriesUpToDate, entry)
			}
		}
		openDebits, unappliedCredit := Allocate(entriesUpToDate)
		supplierBalance := Balance(openDebits, unappliedCredit)
		if supplierBalance == 0 {
			continue
		}
		agingBuckets := Age(openDebits, listedSupplier.PaymentTermsDays, asOfDate, companyLocation)
		agingView.Suppliers = append(agingView.Suppliers, SupplierAgingView{
			SupplierId: listedSupplier.Id,
			Name:       listedSupplier.Name,
			IsActive:   listedSupplier.IsActive,
			Balance:    supplierBalance,
			Aging:      agingBuckets,
		})
		agingView.TotalBalance += supplierBalance
		agingView.Totals = agingView.Totals.Plus(agingBuckets)
	}
	return agingView, nil
}

func (service *Service) Statement(ctx context.Context, querier database.Querier, principal *identity.Principal, supplierId uuid.UUID, fromText string, toText string) (StatementView, error) {
	_, featureError := service.requireFeature(ctx, querier, principal.CompanyId, suppliersRule)
	if featureError != nil {
		return StatementView{}, featureError
	}
	companyLocation, locationError := service.companyLocation(ctx, querier, principal.CompanyId)
	if locationError != nil {
		return StatementView{}, locationError
	}
	foundSupplier, findError := service.findUsableSupplier(ctx, querier, principal.CompanyId, supplierId, false)
	if findError != nil {
		return StatementView{}, findError
	}

	fromDate, toDate, rangeError := parseRange(fromText, toText, civilDate(time.Now(), companyLocation))
	if rangeError != nil {
		return StatementView{}, rangeError
	}
	rangeStart := time.Date(fromDate.Year(), fromDate.Month(), fromDate.Day(), 0, 0, 0, 0, companyLocation)
	rangeEnd := time.Date(toDate.Year(), toDate.Month(), toDate.Day()+1, 0, 0, 0, 0, companyLocation)

	ledgers, ledgerError := service.repository.LoadLedgers(ctx, querier, principal.CompanyId, []Supplier{*foundSupplier}, true)
	if ledgerError != nil {
		return StatementView{}, ledgerError
	}
	openingBalance, statementLines, closingBalance := Statement(ledgers[supplierId], rangeStart, rangeEnd)

	statementView := StatementView{
		SupplierId:     supplierId,
		SupplierName:   foundSupplier.Name,
		From:           fromDate.Format(dateLayout),
		To:             toDate.Format(dateLayout),
		OpeningBalance: openingBalance,
		ClosingBalance: closingBalance,
		Lines:          statementLines,
	}
	return statementView, nil
}

func parseRange(fromText string, toText string, today time.Time) (time.Time, time.Time, error) {
	toDate := today
	if toText != "" {
		parsedDate, parseError := time.Parse(dateLayout, toText)
		if parseError != nil {
			return time.Time{}, time.Time{}, ErrInvalidRange
		}
		toDate = parsedDate
	}
	fromDate := toDate.AddDate(0, 0, -(defaultStatementDays - 1))
	if fromText != "" {
		parsedDate, parseError := time.Parse(dateLayout, fromText)
		if parseError != nil {
			return time.Time{}, time.Time{}, ErrInvalidRange
		}
		fromDate = parsedDate
	}

	rangeDays := int(toDate.Sub(fromDate).Hours()/24) + 1
	if rangeDays < 1 || rangeDays > maximumStatementDays {
		return time.Time{}, time.Time{}, ErrInvalidRange
	}
	return fromDate, toDate, nil
}

func buildSupplier(baseSupplier Supplier, request SupplierRequest) (Supplier, error) {
	builtSupplier := baseSupplier
	builtSupplier.Name = strings.Join(strings.Fields(request.Name), " ")
	if builtSupplier.Name == "" {
		return Supplier{}, ErrNameRequired
	}
	builtSupplier.ContactPerson = trimmedOrNil(request.ContactPerson)
	builtSupplier.Tin = trimmedOrNil(request.Tin)
	builtSupplier.Vrn = trimmedOrNil(request.Vrn)
	builtSupplier.Address = trimmedOrNil(request.Address)
	builtSupplier.Notes = trimmedOrNil(request.Notes)
	builtSupplier.PaymentTermsDays = request.PaymentTermsDays
	builtSupplier.OpeningBalance = request.OpeningBalance

	builtSupplier.Phone = nil
	rawPhone := trimmedOrNil(request.Phone)
	if rawPhone != nil {
		normalizedPhone, isValidPhone := NormalizePhone(*rawPhone)
		if !isValidPhone {
			return Supplier{}, ErrInvalidPhone
		}
		builtSupplier.Phone = &normalizedPhone
	}

	builtSupplier.Email = nil
	rawEmail := trimmedOrNil(request.Email)
	if rawEmail != nil {
		parsedAddress, parseError := mail.ParseAddress(*rawEmail)
		if parseError != nil || parsedAddress.Address != *rawEmail {
			return Supplier{}, ErrInvalidEmail
		}
		lowerEmail := strings.ToLower(*rawEmail)
		builtSupplier.Email = &lowerEmail
	}

	return builtSupplier, nil
}

func toSupplierView(foundSupplier Supplier) SupplierView {
	return SupplierView{
		Id:               foundSupplier.Id,
		Name:             foundSupplier.Name,
		ContactPerson:    foundSupplier.ContactPerson,
		Phone:            foundSupplier.Phone,
		Email:            foundSupplier.Email,
		Tin:              foundSupplier.Tin,
		Vrn:              foundSupplier.Vrn,
		Address:          foundSupplier.Address,
		PaymentTermsDays: foundSupplier.PaymentTermsDays,
		OpeningBalance:   foundSupplier.OpeningBalance,
		Notes:            foundSupplier.Notes,
		IsActive:         foundSupplier.IsActive,
		CreatedAt:        foundSupplier.CreatedAt,
		UpdatedAt:        foundSupplier.UpdatedAt,
	}
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

func (service *Service) validateProducts(ctx context.Context, querier database.Querier, companyId uuid.UUID, rawProductIds []string) ([]uuid.UUID, error) {
	productIds := make([]uuid.UUID, 0, len(rawProductIds))
	seenProducts := map[uuid.UUID]bool{}
	for _, rawProductId := range rawProductIds {
		productId := uuid.MustParse(rawProductId)
		if seenProducts[productId] {
			return nil, ErrDuplicateProduct
		}
		seenProducts[productId] = true
		productIds = append(productIds, productId)
	}

	activeProductCount, countError := service.repository.CountActiveProducts(ctx, querier, companyId, productIds)
	if countError != nil {
		return nil, countError
	}
	if activeProductCount != len(productIds) {
		return nil, ErrProductNotFound
	}
	return productIds, nil
}

func resolveMoment(requestedMoment *time.Time, now time.Time) (time.Time, error) {
	if requestedMoment == nil {
		return now, nil
	}
	if requestedMoment.After(now.Add(futureTolerance)) {
		return time.Time{}, ErrDateInFuture
	}
	return requestedMoment.UTC(), nil
}

func parseOptionalDate(rawDate *string) (*string, error) {
	trimmedDate := trimmedOrNil(rawDate)
	if trimmedDate == nil {
		return nil, nil
	}
	_, parseError := time.Parse(dateLayout, *trimmedDate)
	if parseError != nil {
		return nil, ErrInvalidDate
	}
	return trimmedDate, nil
}

func requireReason(rawReason string) (string, error) {
	trimmedReason := strings.TrimSpace(rawReason)
	if trimmedReason == "" {
		return "", ErrReasonRequired
	}
	return trimmedReason, nil
}
