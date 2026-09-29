package accounting

import (
	"context"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/identity"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/chrisostomemataba/balceinv-api/internal/common/storage"
	"github.com/chrisostomemataba/balceinv-api/internal/media"
	"github.com/google/uuid"
)

const (
	receiptFolder       = "receipts"
	maximumReceiptBytes = 5 * 1024 * 1024
	basisPointsPerWhole = 10000
)

type Service struct {
	repository  *Repository
	ledger      *Ledger
	objectStore storage.Store
}

func NewService(repository *Repository, ledger *Ledger, objectStore storage.Store) *Service {
	return &Service{
		repository:  repository,
		ledger:      ledger,
		objectStore: objectStore,
	}
}

func (service *Service) Mode(ctx context.Context, querier database.Querier, companyId uuid.UUID) (string, error) {
	books, booksError := service.repository.FindBooks(ctx, querier, companyId)
	if booksError != nil {
		return "", booksError
	}
	return books.Mode, nil
}

func (service *Service) Status(ctx context.Context, querier database.Querier, principal *identity.Principal) (StatusView, error) {
	books, booksError := service.repository.FindBooks(ctx, querier, principal.CompanyId)
	if booksError != nil {
		return StatusView{}, booksError
	}
	taxRate, taxError := service.repository.TaxRateBasisPoints(ctx, querier, principal.CompanyId)
	if taxError != nil {
		return StatusView{}, taxError
	}

	statusView := StatusView{
		Mode:               books.Mode,
		VatRegistered:      books.VatRegistered,
		VatRateBasisPoints: taxRate,
		Started:            books.IsStarted,
		ClosedUntil:        books.ClosedUntil,
		Today:              books.Today(),
		SuggestedStartMode: StartToday,
	}
	if books.IsStarted {
		statusView.StartedOn = &books.StartedOn
		statusView.StartMode = &books.StartMode
		unpostedCount, countError := service.ledger.CountUnposted(ctx, querier, books)
		if countError != nil {
			return StatusView{}, countError
		}
		statusView.UnpostedCount = unpostedCount
		return statusView, nil
	}

	firstRecordAt, firstError := service.repository.FirstRecordAt(ctx, querier, principal.CompanyId)
	if firstError != nil {
		return StatusView{}, firstError
	}
	if firstRecordAt != nil {
		firstRecordDate := books.LocalDate(*firstRecordAt)
		statusView.FirstRecordDate = &firstRecordDate
		statusView.SuggestedStartMode = StartHistory
	}
	return statusView, nil
}

func (service *Service) Start(ctx context.Context, querier database.Querier, principal *identity.Principal, request StartRequest) (StatusView, error) {
	books, booksError := service.repository.FindBooks(ctx, querier, principal.CompanyId)
	if booksError != nil {
		return StatusView{}, booksError
	}
	if books.IsStarted {
		return StatusView{}, ErrAlreadyStarted
	}

	firstRecordAt, firstError := service.repository.FirstRecordAt(ctx, querier, principal.CompanyId)
	if firstError != nil {
		return StatusView{}, firstError
	}
	startMode := StartToday
	if firstRecordAt != nil {
		startMode = StartHistory
	}
	if request.Mode != nil {
		startMode = *request.Mode
	}

	startedAt := time.Now().UTC()
	if startMode == StartHistory && firstRecordAt != nil {
		firstLocal := firstRecordAt.In(books.Location)
		startedAt = time.Date(firstLocal.Year(), firstLocal.Month(), firstLocal.Day(), 0, 0, 0, 0, books.Location).UTC()
	}
	startedOn := books.LocalDate(startedAt)

	insertError := service.repository.InsertSettings(ctx, querier, principal.CompanyId, startedOn, startedAt, startMode, principal.UserId)
	if insertError != nil {
		return StatusView{}, insertError
	}
	seedError := service.repository.SeedChart(ctx, querier, principal.CompanyId)
	if seedError != nil {
		return StatusView{}, seedError
	}

	startedBooks, startedBooksError := service.repository.FindBooks(ctx, querier, principal.CompanyId)
	if startedBooksError != nil {
		return StatusView{}, startedBooksError
	}

	openingError := service.postOpening(ctx, querier, principal, startedBooks, request)
	if openingError != nil {
		return StatusView{}, openingError
	}
	_, replayError := service.ledger.PostUnposted(ctx, querier, startedBooks)
	if replayError != nil {
		return StatusView{}, replayError
	}

	return service.Status(ctx, querier, principal)
}

func (service *Service) postOpening(ctx context.Context, querier database.Querier, principal *identity.Principal, books Books, request StartRequest) error {
	stockValues, stockError := service.repository.StockValueByShopAt(ctx, querier, principal.CompanyId, books.StartedAt)
	if stockError != nil {
		return stockError
	}
	reservedValues, reservedError := service.repository.ReservedStockValueByShopAt(ctx, querier, principal.CompanyId, books.StartedAt)
	if reservedError != nil {
		return reservedError
	}
	for shopIndex := range stockValues {
		stockValues[shopIndex].Value += reservedValues[stockValues[shopIndex].ShopId]
		delete(reservedValues, stockValues[shopIndex].ShopId)
	}
	for shopId, reservedValue := range reservedValues {
		stockValues = append(stockValues, shopValue{ShopId: shopId, Value: reservedValue})
	}

	openingLines := []keyedLine{}
	openingTotal := int64(0)
	for _, stockValue := range stockValues {
		if stockValue.Value <= 0 {
			continue
		}
		shopId := stockValue.ShopId
		openingLines = append(openingLines, keyedLine{Key: KeyInventory, Debit: stockValue.Value, ShopId: &shopId})
		openingTotal += stockValue.Value
	}
	moneyAnswers := []keyedLine{
		{Key: KeyCash, Debit: request.CashInDrawer},
		{Key: KeyMobileMoney, Debit: request.MobileMoney},
		{Key: KeyBank, Debit: request.Bank},
	}
	for _, moneyAnswer := range moneyAnswers {
		openingLines = append(openingLines, moneyAnswer)
		openingTotal += moneyAnswer.Debit
	}
	openingLines = append(openingLines, keyedLine{Key: KeyOwnerCapital, Credit: openingTotal})

	header := Entry{
		SourceType: SourceOpening,
		CreatedBy:  &principal.UserId,
	}
	_, postError := service.ledger.postEventWithBooks(ctx, querier, books, books.StartedAt, header, openingLines)
	return postError
}

func (service *Service) CatchUp(ctx context.Context, querier database.Querier, principal *identity.Principal) (CatchUpView, error) {
	books, booksError := service.startedBooks(ctx, querier, principal.CompanyId)
	if booksError != nil {
		return CatchUpView{}, booksError
	}
	postedCount, postError := service.ledger.PostUnposted(ctx, querier, books)
	if postError != nil {
		return CatchUpView{}, postError
	}
	return CatchUpView{Posted: postedCount}, nil
}

func (service *Service) startedBooks(ctx context.Context, querier database.Querier, companyId uuid.UUID) (Books, error) {
	books, booksError := service.repository.FindBooks(ctx, querier, companyId)
	if booksError != nil {
		return Books{}, booksError
	}
	if books.Mode == ModeOff {
		return Books{}, ErrFeatureOff
	}
	if !books.IsStarted {
		return Books{}, ErrNotStarted
	}
	return books, nil
}

func (service *Service) RecordMoney(ctx context.Context, querier database.Querier, principal *identity.Principal, request MoneyRequest) (EntryView, bool, error) {
	books, booksError := service.startedBooks(ctx, querier, principal.CompanyId)
	if booksError != nil {
		return EntryView{}, false, booksError
	}

	existingEntry, existingError := service.existingByClientRef(ctx, querier, principal.CompanyId, request.ClientRef, request.Kind)
	if existingError != nil || existingEntry != nil {
		return derefEntry(existingEntry), false, existingError
	}

	entryDate, dateError := chosenDate(books, request.EntryDate)
	if dateError != nil {
		return EntryView{}, false, dateError
	}
	shopId, shopError := service.optionalShop(ctx, querier, principal.CompanyId, request.ShopId)
	if shopError != nil {
		return EntryView{}, false, shopError
	}
	attachmentError := checkAttachment(principal.CompanyId, request.AttachmentKey)
	if attachmentError != nil {
		return EntryView{}, false, attachmentError
	}

	moneyLines, linesError := service.moneyLines(ctx, querier, books, request, shopId)
	if linesError != nil {
		return EntryView{}, false, linesError
	}

	systemAccountIds, accountsError := service.repository.SystemAccountIds(ctx, querier, principal.CompanyId)
	if accountsError != nil {
		return EntryView{}, false, accountsError
	}
	entryLines := make([]Line, 0, len(moneyLines))
	for _, moneyLine := range moneyLines {
		if moneyLine.Debit == 0 && moneyLine.Credit == 0 {
			continue
		}
		accountId := moneyLine.AccountId
		if accountId == uuid.Nil {
			accountId = systemAccountIds[moneyLine.Key]
		}
		entryLines = append(entryLines, Line{AccountId: accountId, Debit: moneyLine.Debit, Credit: moneyLine.Credit, ShopId: shopId})
	}

	clientRef := request.ClientRef
	moneyEntry := Entry{
		EntryDate:     entryDate,
		SourceType:    request.Kind,
		ClientRef:     &clientRef,
		Memo:          trimmedOrNil(request.Note),
		ShopId:        shopId,
		AttachmentKey: request.AttachmentKey,
		ReceiptNumber: trimmedOrNil(request.ReceiptNumber),
		SupplierTin:   trimmedOrNil(request.SupplierTin),
		CreatedBy:     &principal.UserId,
		Lines:         entryLines,
	}
	postedEntry, postError := service.ledger.post(ctx, querier, books, moneyEntry)
	if postError != nil {
		return EntryView{}, false, postError
	}
	entryView, viewError := service.Entry(ctx, querier, principal, postedEntry.Id)
	return entryView, true, viewError
}

type moneyLine struct {
	Key       string
	AccountId uuid.UUID
	Debit     int64
	Credit    int64
}

func (service *Service) moneyLines(ctx context.Context, querier database.Querier, books Books, request MoneyRequest, shopId *uuid.UUID) ([]moneyLine, error) {
	moneyKey := request.MoneyAccount
	amount := request.Amount

	switch request.Kind {
	case SourceOwnerIn:
		return []moneyLine{{Key: moneyKey, Debit: amount}, {Key: KeyOwnerCapital, Credit: amount}}, nil
	case SourceOwnerOut:
		return []moneyLine{{Key: KeyOwnerDrawings, Debit: amount}, {Key: moneyKey, Credit: amount}}, nil
	case SourceOtherIncome:
		return []moneyLine{{Key: moneyKey, Debit: amount}, {Key: KeyOtherIncome, Credit: amount}}, nil
	case SourceMoneyMove:
		if request.ToMoneyAccount == nil || *request.ToMoneyAccount == moneyKey {
			return nil, ErrSameMoneyAccount
		}
		return []moneyLine{
			{Key: *request.ToMoneyAccount, Debit: amount},
			{Key: KeyMoneyCharges, Debit: request.Fee},
			{Key: moneyKey, Credit: amount + request.Fee},
		}, nil
	}

	if request.ExpenseAccountId == nil {
		return nil, ErrExpenseAccount
	}
	expenseAccount, findError := service.findSpendableAccount(ctx, querier, books.CompanyId, uuid.MustParse(*request.ExpenseAccountId))
	if findError != nil {
		return nil, findError
	}

	vatAmount := int64(0)
	if books.VatRegistered && request.IncludesVat {
		taxRate, taxError := service.repository.TaxRateBasisPoints(ctx, querier, books.CompanyId)
		if taxError != nil {
			return nil, taxError
		}
		vatAmount = IncludedVat(amount, taxRate)
		if request.VatAmount != nil {
			vatAmount = *request.VatAmount
		}
		if vatAmount >= amount {
			return nil, ErrVatTooLarge
		}
	}
	return []moneyLine{
		{AccountId: expenseAccount.Id, Debit: amount - vatAmount},
		{Key: KeyVatInput, Debit: vatAmount},
		{Key: moneyKey, Credit: amount},
	}, nil
}

func (service *Service) findSpendableAccount(ctx context.Context, querier database.Querier, companyId uuid.UUID, accountId uuid.UUID) (Account, error) {
	accounts, listError := service.repository.ListAccounts(ctx, querier, companyId)
	if listError != nil {
		return Account{}, listError
	}
	for _, account := range accounts {
		if account.Id == accountId && isSpendable(account) {
			return account, nil
		}
	}
	return Account{}, ErrExpenseAccount
}

func isSpendable(account Account) bool {
	isAutomatic := account.SystemKey != nil && automaticExpenseKeys[*account.SystemKey]
	return account.Type == TypeExpense && account.IsActive && !isAutomatic
}

func IncludedVat(amount int64, taxRateBasisPoints int) int64 {
	if taxRateBasisPoints <= 0 || amount <= 0 {
		return 0
	}
	divisor := int64(basisPointsPerWhole + taxRateBasisPoints)
	return (amount*int64(taxRateBasisPoints)*2 + divisor) / (2 * divisor)
}

func (service *Service) existingByClientRef(ctx context.Context, querier database.Querier, companyId uuid.UUID, clientRef string, sourceType string) (*EntryView, error) {
	existingEntry, findError := service.repository.FindByClientRef(ctx, querier, companyId, clientRef)
	if findError != nil || existingEntry == nil {
		return nil, findError
	}
	existingView, viewError := service.repository.FindEntryView(ctx, querier, companyId, existingEntry.Id)
	if viewError != nil {
		return nil, viewError
	}
	if existingView == nil || existingView.SourceType != sourceType {
		return nil, ErrClientRefReused
	}
	return existingView, nil
}

func derefEntry(entryView *EntryView) EntryView {
	if entryView == nil {
		return EntryView{}
	}
	return *entryView
}

func (service *Service) Reverse(ctx context.Context, querier database.Querier, principal *identity.Principal, entryId uuid.UUID, request ReverseRequest) (EntryView, error) {
	books, booksError := service.startedBooks(ctx, querier, principal.CompanyId)
	if booksError != nil {
		return EntryView{}, booksError
	}
	originalEntry, findError := service.repository.FindEntryView(ctx, querier, principal.CompanyId, entryId)
	if findError != nil {
		return EntryView{}, findError
	}
	if originalEntry == nil {
		return EntryView{}, ErrEntryNotFound
	}
	if !reversibleSources[originalEntry.SourceType] {
		return EntryView{}, ErrNotReversible
	}
	if originalEntry.ReversedByEntryId != nil {
		return EntryView{}, ErrAlreadyReversed
	}

	originalLines, linesError := service.repository.ListEntryLines(ctx, querier, principal.CompanyId, entryId)
	if linesError != nil {
		return EntryView{}, linesError
	}
	reason := strings.TrimSpace(request.Reason)
	reversalEntry := Entry{
		EntryDate:       firstOpenDate(books, books.Today()),
		SourceType:      SourceReversal,
		SourceId:        &originalEntry.Id,
		Memo:            &reason,
		ShopId:          originalEntry.ShopId,
		ReversesEntryId: &originalEntry.Id,
		CreatedBy:       &principal.UserId,
		Lines:           swapSides(originalLines),
	}
	postedEntry, postError := service.ledger.post(ctx, querier, books, reversalEntry)
	if postError != nil {
		return EntryView{}, postError
	}
	return service.Entry(ctx, querier, principal, postedEntry.Id)
}

func (service *Service) PostManual(ctx context.Context, querier database.Querier, principal *identity.Principal, request ManualRequest) (EntryView, bool, error) {
	books, booksError := service.startedBooks(ctx, querier, principal.CompanyId)
	if booksError != nil {
		return EntryView{}, false, booksError
	}
	existingEntry, existingError := service.existingByClientRef(ctx, querier, principal.CompanyId, request.ClientRef, SourceManual)
	if existingError != nil || existingEntry != nil {
		return derefEntry(existingEntry), false, existingError
	}

	entryDate, dateError := chosenDate(books, request.EntryDate)
	if dateError != nil {
		return EntryView{}, false, dateError
	}

	entryLines := make([]Line, 0, len(request.Lines))
	for _, lineRequest := range request.Lines {
		shopId, shopError := service.optionalShop(ctx, querier, principal.CompanyId, lineRequest.ShopId)
		if shopError != nil {
			return EntryView{}, false, shopError
		}
		entryLines = append(entryLines, Line{
			AccountId: uuid.MustParse(lineRequest.AccountId),
			Debit:     lineRequest.Debit,
			Credit:    lineRequest.Credit,
			ShopId:    shopId,
		})
	}

	clientRef := request.ClientRef
	reason := strings.TrimSpace(request.Reason)
	manualEntry := Entry{
		EntryDate:  entryDate,
		SourceType: SourceManual,
		ClientRef:  &clientRef,
		Memo:       &reason,
		CreatedBy:  &principal.UserId,
		Lines:      entryLines,
	}
	postedEntry, postError := service.ledger.post(ctx, querier, books, manualEntry)
	if postError != nil {
		return EntryView{}, false, postError
	}
	entryView, viewError := service.Entry(ctx, querier, principal, postedEntry.Id)
	return entryView, true, viewError
}

func (service *Service) ClosePeriod(ctx context.Context, querier database.Querier, principal *identity.Principal, request CloseRequest) (StatusView, error) {
	books, booksError := service.startedBooks(ctx, querier, principal.CompanyId)
	if booksError != nil {
		return StatusView{}, booksError
	}
	if books.ClosedUntil != nil && request.ClosedUntil <= *books.ClosedUntil {
		return StatusView{}, ErrCloseBackwards
	}
	if request.ClosedUntil >= books.Today() {
		return StatusView{}, ErrCloseTooRecent
	}
	if request.ClosedUntil < books.StartedOn {
		return StatusView{}, ErrBeforeStart
	}

	closeError := service.repository.SetClosedUntil(ctx, querier, principal.CompanyId, request.ClosedUntil)
	if closeError != nil {
		return StatusView{}, closeError
	}
	return service.Status(ctx, querier, principal)
}

func (service *Service) Accounts(ctx context.Context, querier database.Querier, principal *identity.Principal) ([]AccountView, error) {
	_, booksError := service.startedBooks(ctx, querier, principal.CompanyId)
	if booksError != nil {
		return nil, booksError
	}
	accounts, listError := service.repository.ListAccounts(ctx, querier, principal.CompanyId)
	if listError != nil {
		return nil, listError
	}
	accountViews := make([]AccountView, 0, len(accounts))
	for _, account := range accounts {
		accountViews = append(accountViews, toAccountView(account))
	}
	return accountViews, nil
}

func (service *Service) CreateAccount(ctx context.Context, querier database.Querier, principal *identity.Principal, request AccountRequest) (AccountView, error) {
	_, booksError := service.startedBooks(ctx, querier, principal.CompanyId)
	if booksError != nil {
		return AccountView{}, booksError
	}
	accountName := strings.TrimSpace(request.Name)
	newAccount := Account{
		Id:       uuid.Must(uuid.NewV7()),
		Code:     strings.TrimSpace(request.Code),
		Name:     &accountName,
		Type:     request.Type,
		IsActive: true,
	}
	insertError := service.repository.InsertAccount(ctx, querier, principal.CompanyId, newAccount)
	if insertError != nil {
		return AccountView{}, insertError
	}
	return toAccountView(newAccount), nil
}

func (service *Service) UpdateAccount(ctx context.Context, querier database.Querier, principal *identity.Principal, accountId uuid.UUID, request AccountUpdateRequest) (AccountView, error) {
	_, booksError := service.startedBooks(ctx, querier, principal.CompanyId)
	if booksError != nil {
		return AccountView{}, booksError
	}
	accounts, listError := service.repository.ListAccounts(ctx, querier, principal.CompanyId)
	if listError != nil {
		return AccountView{}, listError
	}

	for _, account := range accounts {
		if account.Id != accountId {
			continue
		}
		if account.IsSystem {
			return AccountView{}, ErrSystemAccount
		}
		if request.Name != nil {
			trimmedName := strings.TrimSpace(*request.Name)
			account.Name = &trimmedName
		}
		if request.IsActive != nil {
			account.IsActive = *request.IsActive
		}
		updateError := service.repository.UpdateAccount(ctx, querier, principal.CompanyId, account)
		if updateError != nil {
			return AccountView{}, updateError
		}
		return toAccountView(account), nil
	}
	return AccountView{}, ErrAccountNotFound
}

func toAccountView(account Account) AccountView {
	isMoney := false
	for _, moneyKey := range moneyKeys {
		isMoney = isMoney || (account.SystemKey != nil && *account.SystemKey == moneyKey)
	}
	return AccountView{
		Id:          account.Id,
		Code:        account.Code,
		SystemKey:   account.SystemKey,
		Name:        account.Name,
		Type:        account.Type,
		IsSystem:    account.IsSystem,
		IsActive:    account.IsActive,
		IsMoney:     isMoney,
		IsSpendable: isSpendable(account),
	}
}

func (service *Service) Entries(ctx context.Context, querier database.Querier, principal *identity.Principal, filter EntryFilter, limit int, offset int) (response.Page[EntryView], error) {
	_, booksError := service.startedBooks(ctx, querier, principal.CompanyId)
	if booksError != nil {
		return response.Page[EntryView]{}, booksError
	}
	totalEntries, countError := service.repository.CountEntries(ctx, querier, principal.CompanyId, filter)
	if countError != nil {
		return response.Page[EntryView]{}, countError
	}
	entryViews, listError := service.repository.ListEntries(ctx, querier, principal.CompanyId, filter, limit, offset)
	if listError != nil {
		return response.Page[EntryView]{}, listError
	}
	entryPage := response.Page[EntryView]{
		Items:  entryViews,
		Total:  totalEntries,
		Limit:  limit,
		Offset: offset,
	}
	return entryPage, nil
}

func (service *Service) Entry(ctx context.Context, querier database.Querier, principal *identity.Principal, entryId uuid.UUID) (EntryView, error) {
	entryView, findError := service.repository.FindEntryView(ctx, querier, principal.CompanyId, entryId)
	if findError != nil {
		return EntryView{}, findError
	}
	if entryView == nil {
		return EntryView{}, ErrEntryNotFound
	}
	return *entryView, nil
}

func (service *Service) UploadReceipt(ctx context.Context, principal *identity.Principal, imageBytes []byte) (ReceiptUploadView, error) {
	attachmentKey, storeError := media.StoreImage(ctx, service.objectStore, receiptFolder, principal.CompanyId, imageBytes, maximumReceiptBytes)
	if storeError != nil {
		return ReceiptUploadView{}, storeError
	}
	return ReceiptUploadView{AttachmentKey: attachmentKey}, nil
}

func (service *Service) Receipt(ctx context.Context, querier database.Querier, principal *identity.Principal, entryId uuid.UUID) (*storage.Object, error) {
	attachmentKey, findError := service.repository.FindAttachmentKey(ctx, querier, principal.CompanyId, entryId)
	if findError != nil {
		return nil, findError
	}
	if attachmentKey == nil {
		return nil, ErrEntryNotFound
	}
	return service.objectStore.Get(ctx, *attachmentKey)
}

func (service *Service) optionalShop(ctx context.Context, querier database.Querier, companyId uuid.UUID, rawShopId *string) (*uuid.UUID, error) {
	if rawShopId == nil || *rawShopId == "" {
		return nil, nil
	}
	shopId, parseError := uuid.Parse(*rawShopId)
	if parseError != nil {
		return nil, ErrShopNotFound
	}
	shopExists, existsError := service.repository.ShopExists(ctx, querier, companyId, shopId)
	if existsError != nil {
		return nil, existsError
	}
	if !shopExists {
		return nil, ErrShopNotFound
	}
	return &shopId, nil
}

func checkAttachment(companyId uuid.UUID, attachmentKey *string) error {
	if attachmentKey == nil {
		return nil
	}
	isOwnReceipt := strings.HasPrefix(*attachmentKey, receiptFolder+"/"+companyId.String()+"/")
	keyError := storage.ValidateKey(*attachmentKey)
	if !isOwnReceipt || keyError != nil {
		return ErrInvalidAttachment
	}
	return nil
}

func chosenDate(books Books, requestedDate string) (string, error) {
	if requestedDate == "" {
		return books.Today(), nil
	}
	_, parseError := time.Parse(dateLayout, requestedDate)
	if parseError != nil {
		return "", ErrInvalidDate
	}
	if requestedDate > books.Today() {
		return "", ErrFutureDate
	}
	return requestedDate, nil
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
