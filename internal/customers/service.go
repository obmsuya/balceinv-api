package customers

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/accounting"
	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/identity"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/chrisostomemataba/balceinv-api/internal/common/storage"
	"github.com/chrisostomemataba/balceinv-api/internal/features"
	"github.com/chrisostomemataba/balceinv-api/internal/settings"
	"github.com/google/uuid"
)

const (
	dateLayout                = "2006-01-02"
	defaultStatementRangeDays = 30
	maximumStatementRangeDays = 3660
)

var (
	ErrFeatureOff           = errors.New("this feature is turned off in Settings → Features")
	ErrCustomerNotFound     = errors.New("customer not found")
	ErrPaymentNotFound      = errors.New("payment not found")
	ErrInvalidPhone         = errors.New("enter a Tanzanian phone number, for example 0712 345 678")
	ErrPhoneTaken           = errors.New("another customer already uses this phone number")
	ErrOpeningBalanceTooLow = errors.New("the debt from before cannot go below what the customer has already paid")
	ErrOverpayment          = errors.New("this is more than the customer owes")
	ErrAlreadyVoided        = errors.New("this payment was already cancelled")
	ErrInvalidRange         = errors.New("choose a valid date range")
	ErrCustomerRequired     = errors.New("choose a customer to sell on credit")
	ErrCustomerInactive     = errors.New("this customer is turned off; choose an active customer")
	ErrCreditLimitExceeded  = errors.New("this would take the customer's debt over their credit limit")
	ErrMissingSettings      = errors.New("company settings are missing")
	ErrInvalidName          = errors.New("enter the customer's name")
	ErrInvalidReason        = errors.New("write why the payment is being cancelled")
)

type Service struct {
	repository         *Repository
	featuresRepository *features.Repository
	settingsRepository *settings.Repository
	ledger             *accounting.Ledger
	objectStore        storage.Store
}

func NewService(repository *Repository, featuresRepository *features.Repository, settingsRepository *settings.Repository, ledger *accounting.Ledger, objectStore storage.Store) *Service {
	return &Service{
		repository:         repository,
		featuresRepository: featuresRepository,
		settingsRepository: settingsRepository,
		ledger:             ledger,
		objectStore:        objectStore,
	}
}

func (service *Service) List(ctx context.Context, querier database.Querier, principal *identity.Principal, filter CustomerFilter, limit int, offset int) (response.Page[CustomerView], error) {
	filter.SearchPhone = phoneSearchFragment(filter.SearchText)
	foundCustomers, totalCustomers, listError := service.repository.List(ctx, querier, principal.CompanyId, filter, limit, offset)
	if listError != nil {
		return response.Page[CustomerView]{}, listError
	}

	customerViews, viewsError := service.viewsOf(ctx, querier, principal.CompanyId, foundCustomers, time.Now().UTC())
	if viewsError != nil {
		return response.Page[CustomerView]{}, viewsError
	}

	customerPage := response.Page[CustomerView]{
		Items:  customerViews,
		Total:  totalCustomers,
		Limit:  limit,
		Offset: offset,
	}
	return customerPage, nil
}

func (service *Service) Get(ctx context.Context, querier database.Querier, companyId uuid.UUID, customerId uuid.UUID) (CustomerView, error) {
	foundCustomer, findError := service.findCustomer(ctx, querier, companyId, customerId)
	if findError != nil {
		return CustomerView{}, findError
	}

	customerViews, viewsError := service.viewsOf(ctx, querier, companyId, []Customer{*foundCustomer}, time.Now().UTC())
	if viewsError != nil {
		return CustomerView{}, viewsError
	}
	return customerViews[0], nil
}

func (service *Service) Create(ctx context.Context, querier database.Querier, principal *identity.Principal, request CustomerRequest) (CustomerView, error) {
	createdAt := time.Now().UTC()
	newCustomer := Customer{
		Id:        uuid.Must(uuid.NewV7()),
		CompanyId: principal.CompanyId,
		IsActive:  true,
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}
	applyError := applyRequest(&newCustomer, request)
	if applyError != nil {
		return CustomerView{}, applyError
	}

	insertError := service.repository.Insert(ctx, querier, newCustomer)
	if database.IsUniqueViolation(insertError) {
		return CustomerView{}, ErrPhoneTaken
	}
	if insertError != nil {
		return CustomerView{}, insertError
	}
	openingError := service.ledger.SyncCustomerOpening(ctx, querier, principal.CompanyId, newCustomer.Id)
	if openingError != nil {
		return CustomerView{}, openingError
	}

	return service.Get(ctx, querier, principal.CompanyId, newCustomer.Id)
}

func (service *Service) Update(ctx context.Context, querier database.Querier, principal *identity.Principal, customerId uuid.UUID, request CustomerRequest) (CustomerView, error) {
	lockError := service.lock(ctx, querier, principal.CompanyId, customerId)
	if lockError != nil {
		return CustomerView{}, lockError
	}
	foundCustomer, findError := service.findCustomer(ctx, querier, principal.CompanyId, customerId)
	if findError != nil {
		return CustomerView{}, findError
	}

	changedCustomer := *foundCustomer
	applyError := applyRequest(&changedCustomer, request)
	if applyError != nil {
		return CustomerView{}, applyError
	}

	isLoweringOpeningBalance := changedCustomer.OpeningBalance < foundCustomer.OpeningBalance
	if isLoweringOpeningBalance {
		currentBalance, balanceError := service.repository.Balance(ctx, querier, principal.CompanyId, customerId)
		if balanceError != nil {
			return CustomerView{}, balanceError
		}
		balanceAfterChange := currentBalance - foundCustomer.OpeningBalance + changedCustomer.OpeningBalance
		if balanceAfterChange < 0 {
			return CustomerView{}, ErrOpeningBalanceTooLow
		}
	}

	changedCustomer.UpdatedAt = time.Now().UTC()
	updateError := service.repository.Update(ctx, querier, changedCustomer)
	if database.IsUniqueViolation(updateError) {
		return CustomerView{}, ErrPhoneTaken
	}
	if updateError != nil {
		return CustomerView{}, updateError
	}
	openingError := service.ledger.SyncCustomerOpening(ctx, querier, principal.CompanyId, customerId)
	if openingError != nil {
		return CustomerView{}, openingError
	}

	return service.Get(ctx, querier, principal.CompanyId, customerId)
}

func (service *Service) Deactivate(ctx context.Context, querier database.Querier, principal *identity.Principal, customerId uuid.UUID) (CustomerView, error) {
	return service.setActive(ctx, querier, principal, customerId, false)
}

func (service *Service) Restore(ctx context.Context, querier database.Querier, principal *identity.Principal, customerId uuid.UUID) (CustomerView, error) {
	return service.setActive(ctx, querier, principal, customerId, true)
}

func (service *Service) Debtors(ctx context.Context, querier database.Querier, principal *identity.Principal, asOfText string) (DebtorsView, error) {
	companyLocation, locationError := service.companyLocation(ctx, querier, principal.CompanyId)
	if locationError != nil {
		return DebtorsView{}, locationError
	}

	asOf := time.Now().UTC()
	if asOfText != "" {
		asOfDay, parseError := time.ParseInLocation(dateLayout, asOfText, companyLocation)
		if parseError != nil {
			return DebtorsView{}, ErrInvalidRange
		}
		asOf = asOfDay.AddDate(0, 0, 1).Add(-time.Nanosecond).UTC()
	}

	owingCustomers, listError := service.repository.ListOwing(ctx, querier, principal.CompanyId)
	if listError != nil {
		return DebtorsView{}, listError
	}
	customerViews, viewsError := service.viewsOf(ctx, querier, principal.CompanyId, owingCustomers, asOf)
	if viewsError != nil {
		return DebtorsView{}, viewsError
	}

	debtorsView := DebtorsView{
		AsOf:      asOf.In(companyLocation).Format(dateLayout),
		Customers: []CustomerView{},
	}
	for _, customerView := range customerViews {
		if customerView.Balance <= 0 {
			continue
		}
		debtorsView.Customers = append(debtorsView.Customers, customerView)
		debtorsView.Totals.Balance += customerView.Balance
		debtorsView.Totals.Aging.Days0To30 += customerView.Aging.Days0To30
		debtorsView.Totals.Aging.Days31To60 += customerView.Aging.Days31To60
		debtorsView.Totals.Aging.Days61To90 += customerView.Aging.Days61To90
		debtorsView.Totals.Aging.DaysOver90 += customerView.Aging.DaysOver90
	}
	sort.SliceStable(debtorsView.Customers, func(left int, right int) bool {
		return debtorsView.Customers[left].OldestDebtAt.Before(*debtorsView.Customers[right].OldestDebtAt)
	})

	return debtorsView, nil
}

func (service *Service) ListSales(ctx context.Context, querier database.Querier, companyId uuid.UUID, customerId uuid.UUID, limit int, offset int) (response.Page[CustomerSaleView], error) {
	_, findError := service.findCustomer(ctx, querier, companyId, customerId)
	if findError != nil {
		return response.Page[CustomerSaleView]{}, findError
	}

	saleCount, countError := service.repository.CountSales(ctx, querier, companyId, customerId)
	if countError != nil {
		return response.Page[CustomerSaleView]{}, countError
	}
	saleViews, listError := service.repository.ListSales(ctx, querier, companyId, customerId, limit, offset)
	if listError != nil {
		return response.Page[CustomerSaleView]{}, listError
	}

	salePage := response.Page[CustomerSaleView]{
		Items:  saleViews,
		Total:  saleCount,
		Limit:  limit,
		Offset: offset,
	}
	return salePage, nil
}

func (service *Service) ListPayments(ctx context.Context, querier database.Querier, companyId uuid.UUID, customerId uuid.UUID) ([]PaymentView, error) {
	_, findError := service.findCustomer(ctx, querier, companyId, customerId)
	if findError != nil {
		return nil, findError
	}
	return service.repository.ListPayments(ctx, querier, companyId, customerId)
}

func (service *Service) RecordPayment(ctx context.Context, querier database.Querier, principal *identity.Principal, customerId uuid.UUID, request PaymentRequest) (PaymentView, error) {
	lockError := service.lock(ctx, querier, principal.CompanyId, customerId)
	if lockError != nil {
		return PaymentView{}, lockError
	}

	currentBalance, balanceError := service.repository.Balance(ctx, querier, principal.CompanyId, customerId)
	if balanceError != nil {
		return PaymentView{}, balanceError
	}
	if request.Amount > currentBalance {
		return PaymentView{}, ErrOverpayment
	}

	newPayment := Payment{
		Id:         uuid.Must(uuid.NewV7()),
		CompanyId:  principal.CompanyId,
		CustomerId: customerId,
		ShopId:     principal.ShopId,
		Amount:     request.Amount,
		Method:     request.Method,
		Reference:  trimmedOrNil(request.Reference),
		ReceivedAt: time.Now().UTC(),
		CreatedBy:  principal.UserId,
	}
	insertError := service.repository.InsertPayment(ctx, querier, newPayment)
	if insertError != nil {
		return PaymentView{}, insertError
	}
	postError := service.ledger.PostCustomerPaymentById(ctx, querier, principal.CompanyId, newPayment.Id)
	if postError != nil {
		return PaymentView{}, postError
	}

	return service.findPayment(ctx, querier, principal.CompanyId, customerId, newPayment.Id)
}

func (service *Service) VoidPayment(ctx context.Context, querier database.Querier, principal *identity.Principal, customerId uuid.UUID, paymentId uuid.UUID, request VoidRequest) (PaymentView, error) {
	foundPayment, findError := service.findPayment(ctx, querier, principal.CompanyId, customerId, paymentId)
	if findError != nil {
		return PaymentView{}, findError
	}
	if foundPayment.VoidedAt != nil {
		return PaymentView{}, ErrAlreadyVoided
	}

	voidReason := strings.TrimSpace(request.Reason)
	if voidReason == "" {
		return PaymentView{}, ErrInvalidReason
	}
	wasVoided, voidError := service.repository.VoidPayment(ctx, querier, principal.CompanyId, customerId, paymentId, principal.UserId, voidReason, time.Now().UTC())
	if voidError != nil {
		return PaymentView{}, voidError
	}
	if !wasVoided {
		return PaymentView{}, ErrAlreadyVoided
	}
	voidPostError := service.ledger.PostCustomerPaymentVoidById(ctx, querier, principal.CompanyId, paymentId)
	if voidPostError != nil {
		return PaymentView{}, voidPostError
	}

	return service.findPayment(ctx, querier, principal.CompanyId, customerId, paymentId)
}

func (service *Service) Statement(ctx context.Context, querier database.Querier, principal *identity.Principal, customerId uuid.UUID, fromText string, toText string) (StatementView, error) {
	foundCustomer, findError := service.findCustomer(ctx, querier, principal.CompanyId, customerId)
	if findError != nil {
		return StatementView{}, findError
	}
	companyLocation, locationError := service.companyLocation(ctx, querier, principal.CompanyId)
	if locationError != nil {
		return StatementView{}, locationError
	}

	firstDay, lastDay, rangeError := statementRange(fromText, toText, companyLocation)
	if rangeError != nil {
		return StatementView{}, rangeError
	}
	startsAt := firstDay.UTC()
	endsAt := lastDay.AddDate(0, 0, 1).UTC()

	statementEntries, entriesError := service.repository.StatementEntries(ctx, querier, principal.CompanyId, customerId, endsAt)
	if entriesError != nil {
		return StatementView{}, entriesError
	}
	sort.SliceStable(statementEntries, func(left int, right int) bool {
		return statementEntries[left].At.Before(statementEntries[right].At)
	})

	statementView := StatementView{
		Customer:       StatementCustomerView{Id: foundCustomer.Id, Name: foundCustomer.Name, Phone: foundCustomer.Phone},
		From:           firstDay.Format(dateLayout),
		To:             lastDay.Format(dateLayout),
		OpeningBalance: foundCustomer.OpeningBalance,
		Entries:        []StatementEntryView{},
	}
	runningBalance := foundCustomer.OpeningBalance
	for _, statementEntry := range statementEntries {
		runningBalance += statementEntry.Debit - statementEntry.Credit
		isBeforeRange := statementEntry.At.Before(startsAt)
		if isBeforeRange {
			statementView.OpeningBalance = runningBalance
			continue
		}
		statementView.TotalDebits += statementEntry.Debit
		statementView.TotalCredits += statementEntry.Credit
		statementView.Entries = append(statementView.Entries, StatementEntryView{
			At:        statementEntry.At,
			Kind:      statementEntry.Kind,
			SaleId:    statementEntry.SaleId,
			PaymentId: statementEntry.PaymentId,
			Reference: statementEntry.Reference,
			Debit:     statementEntry.Debit,
			Credit:    statementEntry.Credit,
			Balance:   runningBalance,
		})
	}
	statementView.ClosingBalance = runningBalance

	return statementView, nil
}

func (service *Service) CheckSaleCustomer(ctx context.Context, querier database.Querier, companyId uuid.UUID, customerId *uuid.UUID, creditAmount int64) error {
	hasCustomer := customerId != nil
	hasCredit := creditAmount > 0
	if !hasCustomer && !hasCredit {
		return nil
	}

	companyFeatures, featuresError := service.featuresRepository.Find(ctx, querier, companyId)
	if featuresError != nil {
		return featuresError
	}
	if !companyFeatures.CustomersEnabled {
		return ErrFeatureOff
	}
	if hasCredit && !companyFeatures.CreditSalesEnabled {
		return ErrFeatureOff
	}
	if !hasCustomer {
		return ErrCustomerRequired
	}

	lockError := service.lock(ctx, querier, companyId, *customerId)
	if lockError != nil {
		return lockError
	}
	foundCustomer, findError := service.findCustomer(ctx, querier, companyId, *customerId)
	if findError != nil {
		return findError
	}
	if hasCredit && !foundCustomer.IsActive {
		return ErrCustomerInactive
	}

	hasCreditLimit := foundCustomer.CreditLimit != nil
	if !hasCredit || !hasCreditLimit {
		return nil
	}
	currentBalance, balanceError := service.repository.Balance(ctx, querier, companyId, *customerId)
	if balanceError != nil {
		return balanceError
	}
	if currentBalance+creditAmount > *foundCustomer.CreditLimit {
		return ErrCreditLimitExceeded
	}
	return nil
}

func (service *Service) RequireActive(ctx context.Context, querier database.Querier, companyId uuid.UUID, customerId uuid.UUID) error {
	foundCustomer, findError := service.findCustomer(ctx, querier, companyId, customerId)
	if findError != nil {
		return findError
	}
	if !foundCustomer.IsActive {
		return ErrCustomerInactive
	}
	return nil
}

func (service *Service) setActive(ctx context.Context, querier database.Querier, principal *identity.Principal, customerId uuid.UUID, isActive bool) (CustomerView, error) {
	foundCustomer, findError := service.findCustomer(ctx, querier, principal.CompanyId, customerId)
	if findError != nil {
		return CustomerView{}, findError
	}

	changedCustomer := *foundCustomer
	changedCustomer.IsActive = isActive
	changedCustomer.UpdatedAt = time.Now().UTC()
	updateError := service.repository.Update(ctx, querier, changedCustomer)
	if database.IsUniqueViolation(updateError) {
		return CustomerView{}, ErrPhoneTaken
	}
	if updateError != nil {
		return CustomerView{}, updateError
	}

	return service.Get(ctx, querier, principal.CompanyId, customerId)
}

func (service *Service) viewsOf(ctx context.Context, querier database.Querier, companyId uuid.UUID, foundCustomers []Customer, asOf time.Time) ([]CustomerView, error) {
	customerViews := make([]CustomerView, 0, len(foundCustomers))
	if len(foundCustomers) == 0 {
		return customerViews, nil
	}

	companyLocation, locationError := service.companyLocation(ctx, querier, companyId)
	if locationError != nil {
		return nil, locationError
	}

	customerIds := make([]uuid.UUID, 0, len(foundCustomers))
	for _, foundCustomer := range foundCustomers {
		customerIds = append(customerIds, foundCustomer.Id)
	}
	creditDebts, debtsError := service.repository.CreditDebts(ctx, querier, companyId, customerIds)
	if debtsError != nil {
		return nil, debtsError
	}
	paidByCustomer, paidError := service.repository.PaidTotals(ctx, querier, companyId, customerIds)
	if paidError != nil {
		return nil, paidError
	}

	debtsByCustomer := map[uuid.UUID][]Debt{}
	for _, creditDebt := range creditDebts {
		debtsByCustomer[creditDebt.CustomerId] = append(debtsByCustomer[creditDebt.CustomerId], creditDebt)
	}

	for _, foundCustomer := range foundCustomers {
		customerDebts := debtsByCustomer[foundCustomer.Id]
		if foundCustomer.OpeningBalance > 0 {
			openingDebt := Debt{CustomerId: foundCustomer.Id, OccurredAt: foundCustomer.CreatedAt, Amount: foundCustomer.OpeningBalance}
			customerDebts = append(customerDebts, openingDebt)
		}
		standing := StandingOf(customerDebts, paidByCustomer[foundCustomer.Id], asOf, companyLocation)
		customerViews = append(customerViews, toView(foundCustomer, standing))
	}
	return customerViews, nil
}

func (service *Service) lock(ctx context.Context, querier database.Querier, companyId uuid.UUID, customerId uuid.UUID) error {
	wasLocked, lockError := service.repository.Lock(ctx, querier, companyId, customerId)
	if lockError != nil {
		return lockError
	}
	if !wasLocked {
		return ErrCustomerNotFound
	}
	return nil
}

func (service *Service) findCustomer(ctx context.Context, querier database.Querier, companyId uuid.UUID, customerId uuid.UUID) (*Customer, error) {
	foundCustomer, findError := service.repository.Find(ctx, querier, companyId, customerId)
	if findError != nil {
		return nil, findError
	}
	if foundCustomer == nil {
		return nil, ErrCustomerNotFound
	}
	return foundCustomer, nil
}

func (service *Service) findPayment(ctx context.Context, querier database.Querier, companyId uuid.UUID, customerId uuid.UUID, paymentId uuid.UUID) (PaymentView, error) {
	foundPayment, findError := service.repository.FindPayment(ctx, querier, companyId, customerId, paymentId)
	if findError != nil {
		return PaymentView{}, findError
	}
	if foundPayment == nil {
		return PaymentView{}, ErrPaymentNotFound
	}
	return *foundPayment, nil
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

func statementRange(fromText string, toText string, companyLocation *time.Location) (time.Time, time.Time, error) {
	nowInCompany := time.Now().In(companyLocation)
	lastDay := time.Date(nowInCompany.Year(), nowInCompany.Month(), nowInCompany.Day(), 0, 0, 0, 0, companyLocation)
	if toText != "" {
		parsedDay, parseError := time.ParseInLocation(dateLayout, toText, companyLocation)
		if parseError != nil {
			return time.Time{}, time.Time{}, ErrInvalidRange
		}
		lastDay = parsedDay
	}

	firstDay := lastDay.AddDate(0, 0, -(defaultStatementRangeDays - 1))
	if fromText != "" {
		parsedDay, parseError := time.ParseInLocation(dateLayout, fromText, companyLocation)
		if parseError != nil {
			return time.Time{}, time.Time{}, ErrInvalidRange
		}
		firstDay = parsedDay
	}

	isBackwards := firstDay.After(lastDay)
	isTooLong := firstDay.AddDate(0, 0, maximumStatementRangeDays).Before(lastDay)
	if isBackwards || isTooLong {
		return time.Time{}, time.Time{}, ErrInvalidRange
	}
	return firstDay, lastDay, nil
}

func applyRequest(customer *Customer, request CustomerRequest) error {
	customerName := strings.TrimSpace(request.Name)
	if customerName == "" {
		return ErrInvalidName
	}

	customer.Name = customerName
	customer.Phone = nil
	phoneText := trimmedOrNil(request.Phone)
	if phoneText != nil {
		normalizedPhone, isValidPhone := NormalizePhone(*phoneText)
		if !isValidPhone {
			return ErrInvalidPhone
		}
		customer.Phone = &normalizedPhone
	}
	customer.Email = trimmedOrNil(request.Email)
	customer.Address = trimmedOrNil(request.Address)
	customer.Tin = trimmedOrNil(request.Tin)
	customer.CreditLimit = request.CreditLimit
	customer.OpeningBalance = request.OpeningBalance
	customer.Notes = trimmedOrNil(request.Notes)
	return nil
}

func toView(customer Customer, standing DebtStanding) CustomerView {
	customerView := CustomerView{
		Id:             customer.Id,
		Name:           customer.Name,
		Phone:          customer.Phone,
		Email:          customer.Email,
		Address:        customer.Address,
		Tin:            customer.Tin,
		CreditLimit:    customer.CreditLimit,
		OpeningBalance: customer.OpeningBalance,
		Notes:          customer.Notes,
		IsActive:       customer.IsActive,
		Balance:        standing.Balance,
		OverdueAmount:  standing.OverdueAmount,
		OldestDebtAt:   standing.OldestDebtAt,
		Aging:          standing.Aging,
		LastVisitAt:    customer.LastVisitAt,
		CreatedAt:      customer.CreatedAt,
		UpdatedAt:      customer.UpdatedAt,
	}
	if customer.CreditLimit != nil {
		availableCredit := max(0, *customer.CreditLimit-standing.Balance)
		customerView.AvailableCredit = &availableCredit
	}
	return customerView
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
