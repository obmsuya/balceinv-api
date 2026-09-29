package accounting

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/google/uuid"
)

type Repository struct{}

func NewRepository() *Repository {
	return &Repository{}
}

type queryArguments struct {
	values []any
}

func (arguments *queryArguments) add(value any) string {
	arguments.values = append(arguments.values, value)
	return "$" + strconv.Itoa(len(arguments.values))
}

func (repository *Repository) FindBooks(ctx context.Context, querier database.Querier, companyId uuid.UUID) (Books, error) {
	query := `
		SELECT c.timezone, COALESCE(f.accounting_mode, 'off'), COALESCE(f.vat_registered, FALSE),
		       a.started_on, a.started_at, a.start_mode, a.closed_until
		FROM companies c
		LEFT JOIN company_features f ON f.company_id = c.id
		LEFT JOIN accounting_settings a ON a.company_id = c.id
		WHERE c.id = $1
	`

	timezone := ""
	startedOn := sql.NullString{}
	startedAt := sql.NullTime{}
	startMode := sql.NullString{}
	books := Books{CompanyId: companyId}
	scanError := querier.QueryRowContext(ctx, query, companyId).Scan(
		&timezone,
		&books.Mode,
		&books.VatRegistered,
		&startedOn,
		&startedAt,
		&startMode,
		&books.ClosedUntil,
	)
	if scanError != nil {
		return Books{}, fmt.Errorf("failed to read the books settings: %w", scanError)
	}

	companyLocation, locationError := time.LoadLocation(timezone)
	if locationError != nil {
		companyLocation = time.UTC
	}
	books.Location = companyLocation
	books.IsStarted = startedOn.Valid
	books.StartedOn = startedOn.String
	books.StartedAt = startedAt.Time
	books.StartMode = startMode.String
	return books, nil
}

func (repository *Repository) InsertSettings(ctx context.Context, querier database.Querier, companyId uuid.UUID, startedOn string, startedAt time.Time, startMode string, startedBy uuid.UUID) error {
	query := `
		INSERT INTO accounting_settings (company_id, started_on, started_at, start_mode, started_by, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $6)
	`
	_, insertError := querier.ExecContext(ctx, query, companyId, startedOn, startedAt, startMode, startedBy, time.Now().UTC())
	if database.IsUniqueViolation(insertError) {
		return ErrAlreadyStarted
	}
	if insertError != nil {
		return fmt.Errorf("failed to start the books: %w", insertError)
	}
	return nil
}

func (repository *Repository) SetClosedUntil(ctx context.Context, querier database.Querier, companyId uuid.UUID, closedUntil string) error {
	query := `UPDATE accounting_settings SET closed_until = $2, updated_at = $3 WHERE company_id = $1`
	_, updateError := querier.ExecContext(ctx, query, companyId, closedUntil, time.Now().UTC())
	if updateError != nil {
		return fmt.Errorf("failed to close the period: %w", updateError)
	}
	return nil
}

func (repository *Repository) TakeEntryNumber(ctx context.Context, querier database.Querier, companyId uuid.UUID) (int64, error) {
	query := `
		UPDATE accounting_settings
		SET last_entry_number = last_entry_number + 1
		WHERE company_id = $1
		RETURNING last_entry_number
	`
	entryNumber := int64(0)
	scanError := querier.QueryRowContext(ctx, query, companyId).Scan(&entryNumber)
	if errors.Is(scanError, sql.ErrNoRows) {
		return 0, ErrNotStarted
	}
	if scanError != nil {
		return 0, fmt.Errorf("failed to number the entry: %w", scanError)
	}
	return entryNumber, nil
}

func (repository *Repository) SeedChart(ctx context.Context, querier database.Querier, companyId uuid.UUID) error {
	createdAt := time.Now().UTC()
	for _, chartAccount := range systemChart {
		query := `
			INSERT INTO accounts (id, company_id, code, system_key, type, is_system, is_active, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, TRUE, TRUE, $6, $6)
			ON CONFLICT (company_id, system_key) DO NOTHING
		`
		_, insertError := querier.ExecContext(ctx, query, uuid.Must(uuid.NewV7()), companyId, chartAccount.Code, chartAccount.Key, chartAccount.Type, createdAt)
		if insertError != nil {
			return fmt.Errorf("failed to add the %s account: %w", chartAccount.Key, insertError)
		}
	}
	return nil
}

func (repository *Repository) SystemAccountIds(ctx context.Context, querier database.Querier, companyId uuid.UUID) (map[string]uuid.UUID, error) {
	query := `SELECT system_key, id FROM accounts WHERE company_id = $1 AND system_key IS NOT NULL AND is_active`

	accountRows, queryError := querier.QueryContext(ctx, query, companyId)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list the built-in accounts: %w", queryError)
	}
	defer accountRows.Close()

	accountIds := map[string]uuid.UUID{}
	for accountRows.Next() {
		systemKey := ""
		accountId := uuid.UUID{}
		scanError := accountRows.Scan(&systemKey, &accountId)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan a built-in account: %w", scanError)
		}
		accountIds[systemKey] = accountId
	}
	return accountIds, accountRows.Err()
}

func (repository *Repository) AreAccountsUsable(ctx context.Context, querier database.Querier, companyId uuid.UUID, accountIds []uuid.UUID) (bool, error) {
	distinctIds := []uuid.UUID{}
	seenIds := map[uuid.UUID]bool{}
	for _, accountId := range accountIds {
		if !seenIds[accountId] {
			seenIds[accountId] = true
			distinctIds = append(distinctIds, accountId)
		}
	}

	query := `SELECT COUNT(*) FROM accounts WHERE company_id = $1 AND is_active AND id IN (` + database.Placeholders(2, len(distinctIds)) + `)`
	arguments := append([]any{companyId}, database.ToArguments(distinctIds)...)
	usableCount := 0
	scanError := querier.QueryRowContext(ctx, query, arguments...).Scan(&usableCount)
	if scanError != nil {
		return false, fmt.Errorf("failed to check the accounts: %w", scanError)
	}
	return usableCount == len(distinctIds), nil
}

func (repository *Repository) ListAccounts(ctx context.Context, querier database.Querier, companyId uuid.UUID) ([]Account, error) {
	query := `SELECT id, code, system_key, name, type, is_system, is_active FROM accounts WHERE company_id = $1 ORDER BY code`

	accountRows, queryError := querier.QueryContext(ctx, query, companyId)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list accounts: %w", queryError)
	}
	defer accountRows.Close()

	accounts := []Account{}
	for accountRows.Next() {
		account := Account{}
		scanError := accountRows.Scan(&account.Id, &account.Code, &account.SystemKey, &account.Name, &account.Type, &account.IsSystem, &account.IsActive)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan an account: %w", scanError)
		}
		accounts = append(accounts, account)
	}
	return accounts, accountRows.Err()
}

func (repository *Repository) InsertAccount(ctx context.Context, querier database.Querier, companyId uuid.UUID, account Account) error {
	query := `
		INSERT INTO accounts (id, company_id, code, name, type, is_system, is_active, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, FALSE, TRUE, $6, $6)
	`
	_, insertError := querier.ExecContext(ctx, query, account.Id, companyId, account.Code, account.Name, account.Type, time.Now().UTC())
	if database.IsUniqueViolation(insertError) {
		return ErrCodeTaken
	}
	if insertError != nil {
		return fmt.Errorf("failed to add the account: %w", insertError)
	}
	return nil
}

func (repository *Repository) UpdateAccount(ctx context.Context, querier database.Querier, companyId uuid.UUID, account Account) error {
	query := `UPDATE accounts SET name = $3, is_active = $4, updated_at = $5 WHERE company_id = $1 AND id = $2`
	_, updateError := querier.ExecContext(ctx, query, companyId, account.Id, account.Name, account.IsActive, time.Now().UTC())
	if updateError != nil {
		return fmt.Errorf("failed to update the account: %w", updateError)
	}
	return nil
}

func (repository *Repository) InsertEntry(ctx context.Context, querier database.Querier, companyId uuid.UUID, postedEntry PostedEntry, entry Entry, createdAt time.Time) error {
	entryQuery := `
		INSERT INTO journal_entries (id, company_id, entry_number, entry_date, source_type, source_id, client_ref, memo, shop_id,
		                             attachment_key, receipt_number, supplier_tin, party_type, party_id, reverses_entry_id, created_by, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, $13, $14, $15, $16, $17)
	`
	_, entryError := querier.ExecContext(ctx, entryQuery,
		postedEntry.Id,
		companyId,
		postedEntry.EntryNumber,
		entry.EntryDate,
		entry.SourceType,
		entry.SourceId,
		entry.ClientRef,
		entry.Memo,
		entry.ShopId,
		entry.AttachmentKey,
		entry.ReceiptNumber,
		entry.SupplierTin,
		entry.PartyType,
		entry.PartyId,
		entry.ReversesEntryId,
		entry.CreatedBy,
		createdAt,
	)
	if entryError != nil {
		return fmt.Errorf("failed to insert the journal entry: %w", entryError)
	}

	for lineIndex, entryLine := range entry.Lines {
		lineQuery := `
			INSERT INTO journal_lines (company_id, entry_id, line_no, account_id, debit, credit, shop_id)
			VALUES ($1, $2, $3, $4, $5, $6, $7)
		`
		_, lineError := querier.ExecContext(ctx, lineQuery, companyId, postedEntry.Id, lineIndex+1, entryLine.AccountId, entryLine.Debit, entryLine.Credit, entryLine.ShopId)
		if lineError != nil {
			return fmt.Errorf("failed to insert a journal line: %w", lineError)
		}
	}
	return nil
}

func (repository *Repository) FindBySource(ctx context.Context, querier database.Querier, companyId uuid.UUID, sourceType string, sourceId uuid.UUID) (*PostedEntry, error) {
	query := `SELECT id, entry_number FROM journal_entries WHERE company_id = $1 AND source_type = $2 AND source_id = $3`
	return repository.findPosted(ctx, querier, query, companyId, sourceType, sourceId)
}

func (repository *Repository) FindByClientRef(ctx context.Context, querier database.Querier, companyId uuid.UUID, clientRef string) (*PostedEntry, error) {
	query := `SELECT id, entry_number FROM journal_entries WHERE company_id = $1 AND client_ref = $2`
	return repository.findPosted(ctx, querier, query, companyId, clientRef)
}

func (repository *Repository) findPosted(ctx context.Context, querier database.Querier, query string, arguments ...any) (*PostedEntry, error) {
	postedEntry := PostedEntry{}
	scanError := querier.QueryRowContext(ctx, query, arguments...).Scan(&postedEntry.Id, &postedEntry.EntryNumber)
	if errors.Is(scanError, sql.ErrNoRows) {
		return nil, nil
	}
	if scanError != nil {
		return nil, fmt.Errorf("failed to look up the journal entry: %w", scanError)
	}
	return &postedEntry, nil
}

func (repository *Repository) ListEntryLines(ctx context.Context, querier database.Querier, companyId uuid.UUID, entryId uuid.UUID) ([]Line, error) {
	query := `SELECT account_id, debit, credit, shop_id FROM journal_lines WHERE company_id = $1 AND entry_id = $2 ORDER BY line_no`

	lineRows, queryError := querier.QueryContext(ctx, query, companyId, entryId)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list the entry lines: %w", queryError)
	}
	defer lineRows.Close()

	entryLines := []Line{}
	for lineRows.Next() {
		entryLine := Line{}
		scanError := lineRows.Scan(&entryLine.AccountId, &entryLine.Debit, &entryLine.Credit, &entryLine.ShopId)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan an entry line: %w", scanError)
		}
		entryLines = append(entryLines, entryLine)
	}
	return entryLines, lineRows.Err()
}

func (repository *Repository) ShopExists(ctx context.Context, querier database.Querier, companyId uuid.UUID, shopId uuid.UUID) (bool, error) {
	shopCount := 0
	scanError := querier.QueryRowContext(ctx, `SELECT COUNT(*) FROM shops WHERE company_id = $1 AND id = $2`, companyId, shopId).Scan(&shopCount)
	if scanError != nil {
		return false, fmt.Errorf("failed to find the shop: %w", scanError)
	}
	return shopCount == 1, nil
}

func (repository *Repository) TaxRateBasisPoints(ctx context.Context, querier database.Querier, companyId uuid.UUID) (int, error) {
	taxRate := 0
	scanError := querier.QueryRowContext(ctx, `SELECT tax_rate_basis_points FROM settings WHERE company_id = $1`, companyId).Scan(&taxRate)
	if errors.Is(scanError, sql.ErrNoRows) {
		return 0, nil
	}
	if scanError != nil {
		return 0, fmt.Errorf("failed to read the tax rate: %w", scanError)
	}
	return taxRate, nil
}

func joinedPlaceholders(arguments *queryArguments, values []string) string {
	placeholders := make([]string, 0, len(values))
	for _, value := range values {
		placeholders = append(placeholders, arguments.add(value))
	}
	return strings.Join(placeholders, ", ")
}
