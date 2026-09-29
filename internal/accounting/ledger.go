package accounting

import (
	"context"
	"regexp"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/google/uuid"
)

var datePattern = regexp.MustCompile(`^\d{4}-\d{2}-\d{2}$`)

type Ledger struct {
	repository *Repository
}

func NewLedger(repository *Repository) *Ledger {
	return &Ledger{
		repository: repository,
	}
}

func (ledger *Ledger) Books(ctx context.Context, querier database.Querier, companyId uuid.UUID) (Books, error) {
	return ledger.repository.FindBooks(ctx, querier, companyId)
}

func (ledger *Ledger) Post(ctx context.Context, querier database.Querier, companyId uuid.UUID, entry Entry) (*PostedEntry, error) {
	books, booksError := ledger.repository.FindBooks(ctx, querier, companyId)
	if booksError != nil {
		return nil, booksError
	}
	if !books.IsPosting() {
		return nil, nil
	}
	return ledger.post(ctx, querier, books, entry)
}

func (ledger *Ledger) post(ctx context.Context, querier database.Querier, books Books, entry Entry) (*PostedEntry, error) {
	if entry.SourceId != nil {
		existingEntry, findError := ledger.repository.FindBySource(ctx, querier, books.CompanyId, entry.SourceType, *entry.SourceId)
		if findError != nil {
			return nil, findError
		}
		if existingEntry != nil {
			return existingEntry, nil
		}
	}

	validationError := validateEntry(books, entry)
	if validationError != nil {
		return nil, validationError
	}

	accountIds := make([]uuid.UUID, 0, len(entry.Lines))
	for _, entryLine := range entry.Lines {
		accountIds = append(accountIds, entryLine.AccountId)
	}
	allUsable, usableError := ledger.repository.AreAccountsUsable(ctx, querier, books.CompanyId, accountIds)
	if usableError != nil {
		return nil, usableError
	}
	if !allUsable {
		return nil, ErrAccountNotUsable
	}

	entryNumber, numberError := ledger.repository.TakeEntryNumber(ctx, querier, books.CompanyId)
	if numberError != nil {
		return nil, numberError
	}

	postedEntry := PostedEntry{
		Id:          uuid.Must(uuid.NewV7()),
		EntryNumber: entryNumber,
	}
	insertError := ledger.repository.InsertEntry(ctx, querier, books.CompanyId, postedEntry, entry, time.Now().UTC())
	if insertError != nil {
		return nil, insertError
	}
	return &postedEntry, nil
}

func validateEntry(books Books, entry Entry) error {
	if !datePattern.MatchString(entry.EntryDate) {
		return ErrInvalidDate
	}
	_, parseError := time.Parse(dateLayout, entry.EntryDate)
	if parseError != nil {
		return ErrInvalidDate
	}
	if entry.EntryDate < books.StartedOn {
		return ErrBeforeStart
	}
	if books.ClosedUntil != nil && entry.EntryDate <= *books.ClosedUntil {
		return ErrPeriodClosed
	}
	if len(entry.Lines) < 2 {
		return ErrTooFewLines
	}

	debitTotal := int64(0)
	creditTotal := int64(0)
	for _, entryLine := range entry.Lines {
		isOneSided := (entryLine.Debit > 0 && entryLine.Credit == 0) || (entryLine.Credit > 0 && entryLine.Debit == 0)
		if !isOneSided {
			return ErrInvalidLine
		}
		debitTotal += entryLine.Debit
		creditTotal += entryLine.Credit
	}
	if debitTotal != creditTotal {
		return ErrUnbalanced
	}
	return nil
}

func (ledger *Ledger) postEvent(ctx context.Context, querier database.Querier, companyId uuid.UUID, happenedAt time.Time, header Entry, keyedLines []keyedLine) (*PostedEntry, error) {
	books, booksError := ledger.repository.FindBooks(ctx, querier, companyId)
	if booksError != nil {
		return nil, booksError
	}
	if !books.IsPosting() {
		return nil, nil
	}
	return ledger.postEventWithBooks(ctx, querier, books, happenedAt, header, keyedLines)
}

func (ledger *Ledger) postEventWithBooks(ctx context.Context, querier database.Querier, books Books, happenedAt time.Time, header Entry, keyedLines []keyedLine) (*PostedEntry, error) {
	header.EntryDate = firstOpenDate(books, books.LocalDate(happenedAt))

	systemAccountIds, accountsError := ledger.repository.SystemAccountIds(ctx, querier, books.CompanyId)
	if accountsError != nil {
		return nil, accountsError
	}

	entryLines := make([]Line, 0, len(keyedLines))
	for _, keyed := range keyedLines {
		if keyed.Debit == 0 && keyed.Credit == 0 {
			continue
		}
		accountId, isKnown := systemAccountIds[keyed.Key]
		if !isKnown {
			return nil, ErrAccountNotUsable
		}
		entryLines = append(entryLines, Line{AccountId: accountId, Debit: keyed.Debit, Credit: keyed.Credit, ShopId: keyed.ShopId})
	}
	if len(entryLines) == 0 {
		return nil, nil
	}
	header.Lines = entryLines
	return ledger.post(ctx, querier, books, header)
}

func firstOpenDate(books Books, wantedDate string) string {
	openDate := wantedDate
	if openDate < books.StartedOn {
		openDate = books.StartedOn
	}
	if books.ClosedUntil != nil && openDate <= *books.ClosedUntil {
		openDate = nextDate(*books.ClosedUntil)
	}
	return openDate
}

func nextDate(date string) string {
	parsedDate, parseError := time.Parse(dateLayout, date)
	if parseError != nil {
		return date
	}
	return parsedDate.AddDate(0, 0, 1).Format(dateLayout)
}

func moneyKeyFor(method string) (string, error) {
	accountKey, isKnown := accountKeyByPaymentMethod[method]
	if !isKnown {
		return "", ErrUnknownMethod
	}
	return accountKey, nil
}

func methodLines(paidByMethod map[string]int64, isDebit bool, shopId *uuid.UUID) ([]keyedLine, error) {
	for method := range paidByMethod {
		_, keyError := moneyKeyFor(method)
		if keyError != nil {
			return nil, keyError
		}
	}

	collectedLines := []keyedLine{}
	for _, method := range paymentMethodOrder {
		amount, isPaid := paidByMethod[method]
		if !isPaid {
			continue
		}
		accountKey, _ := moneyKeyFor(method)
		if isDebit {
			collectedLines = append(collectedLines, keyedLine{Key: accountKey, Debit: amount, ShopId: shopId})
		} else {
			collectedLines = append(collectedLines, keyedLine{Key: accountKey, Credit: amount, ShopId: shopId})
		}
	}
	return collectedLines, nil
}

func textOrNil(value string) *string {
	if value == "" {
		return nil
	}
	return &value
}

func partyOf(partyType string, partyId *uuid.UUID) (*string, *uuid.UUID) {
	if partyId == nil {
		return nil, nil
	}
	return &partyType, partyId
}
