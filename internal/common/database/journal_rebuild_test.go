package database

import (
	"database/sql"
	"path/filepath"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/config"
	"github.com/google/uuid"
)

func TestJournalRebuildKeepsTheBooksAndTheirGuards(t *testing.T) {
	sqlitePath := filepath.Join(t.TempDir(), "books.db")
	migrator, openError := openMigrator(config.EngineSqlite, "", sqlitePath)
	if openError != nil {
		t.Fatalf("open migrator: %v", openError)
	}
	beforeRebuildError := migrator.Migrate(65)
	if beforeRebuildError != nil {
		t.Fatalf("migrate to 65: %v", beforeRebuildError)
	}

	connection, connectionError := sql.Open("sqlite", sqliteDsn(sqlitePath, false))
	if connectionError != nil {
		t.Fatalf("open sqlite: %v", connectionError)
	}
	mustExec := func(query string, arguments ...any) {
		t.Helper()
		_, execError := connection.Exec(query, arguments...)
		if execError != nil {
			t.Fatalf("%s: %v", query, execError)
		}
	}
	companyId, cashId, salesId := uuid.NewString(), uuid.NewString(), uuid.NewString()
	saleEntryId, reversalEntryId := uuid.NewString(), uuid.NewString()
	mustExec(`INSERT INTO companies (id, name) VALUES ($1, 'Books Shop')`, companyId)
	mustExec(`INSERT INTO accounts (id, company_id, code, system_key, type, is_system) VALUES ($1, $2, '1000', 'cash', 'asset', TRUE)`, cashId, companyId)
	mustExec(`INSERT INTO accounts (id, company_id, code, system_key, type, is_system) VALUES ($1, $2, '4000', 'sales', 'income', TRUE)`, salesId, companyId)
	mustExec(`INSERT INTO journal_entries (id, company_id, entry_number, entry_date, source_type, source_id, memo) VALUES ($1, $2, 1, '2026-10-01', 'sale', $3, 'SALE-1')`, saleEntryId, companyId, uuid.NewString())
	mustExec(`INSERT INTO journal_entries (id, company_id, entry_number, entry_date, source_type, source_id, reverses_entry_id) VALUES ($1, $2, 2, '2026-10-02', 'sale_void', $3, $4)`, reversalEntryId, companyId, uuid.NewString(), saleEntryId)
	for _, entryId := range []string{saleEntryId, reversalEntryId} {
		mustExec(`INSERT INTO journal_lines (company_id, entry_id, line_no, account_id, debit) VALUES ($1, $2, 1, $3, 5000)`, companyId, entryId, cashId)
		mustExec(`INSERT INTO journal_lines (company_id, entry_id, line_no, account_id, credit) VALUES ($1, $2, 2, $3, 5000)`, companyId, entryId, salesId)
	}
	connection.Close()

	upError := migrator.Up()
	if upError != nil {
		t.Fatalf("migrate up over existing books: %v", upError)
	}
	migrator.Close()

	connection, connectionError = sql.Open("sqlite", sqliteDsn(sqlitePath, false))
	if connectionError != nil {
		t.Fatalf("reopen sqlite: %v", connectionError)
	}
	defer connection.Close()

	entryCount, lineCount := 0, 0
	connection.QueryRow(`SELECT COUNT(*) FROM journal_entries`).Scan(&entryCount)
	connection.QueryRow(`SELECT COUNT(*) FROM journal_lines`).Scan(&lineCount)
	if entryCount != 2 || lineCount != 4 {
		t.Fatalf("after the rebuild there are %d entries and %d lines", entryCount, lineCount)
	}
	reversedId := ""
	connection.QueryRow(`SELECT reverses_entry_id FROM journal_entries WHERE id = $1`, reversalEntryId).Scan(&reversedId)
	if reversedId != saleEntryId {
		t.Fatalf("the reversal lost its link: %q", reversedId)
	}

	violationRows, checkError := connection.Query(`PRAGMA foreign_key_check`)
	if checkError != nil {
		t.Fatalf("foreign key check: %v", checkError)
	}
	hasViolation := violationRows.Next()
	violationRows.Close()
	if hasViolation {
		t.Fatal("the rebuild left a broken foreign key")
	}

	_, updateError := connection.Exec(`UPDATE journal_entries SET memo = 'changed' WHERE id = $1`, saleEntryId)
	if updateError == nil {
		t.Fatal("a journal entry could be changed after the rebuild")
	}
	_, deleteError := connection.Exec(`DELETE FROM journal_entries WHERE id = $1`, reversalEntryId)
	if deleteError == nil {
		t.Fatal("a journal entry could be deleted after the rebuild")
	}

	_, invoiceEntryError := connection.Exec(`INSERT INTO journal_entries (id, company_id, entry_number, entry_date, source_type, source_id) VALUES ($1, $2, 3, '2026-10-03', 'invoice', $3)`, uuid.NewString(), companyId, uuid.NewString())
	if invoiceEntryError != nil {
		t.Fatalf("an invoice entry was refused: %v", invoiceEntryError)
	}
	_, unknownSourceError := connection.Exec(`INSERT INTO journal_entries (id, company_id, entry_number, entry_date, source_type) VALUES ($1, $2, 4, '2026-10-03', 'made_up')`, uuid.NewString(), companyId)
	if unknownSourceError == nil {
		t.Fatal("an unknown source type was accepted")
	}

	mustExecOn := func(query string, arguments ...any) error {
		_, execError := connection.Exec(query, arguments...)
		return execError
	}
	clearingError := mustExecOn(`INSERT INTO journal_clearing (clearing_company_id) VALUES ($1)`, companyId)
	if clearingError != nil {
		t.Fatalf("start clearing: %v", clearingError)
	}
	if clearLinesError := mustExecOn(`DELETE FROM journal_lines WHERE company_id = $1`, companyId); clearLinesError != nil {
		t.Fatalf("clearing lines: %v", clearLinesError)
	}
	if clearEntriesError := mustExecOn(`DELETE FROM journal_entries WHERE company_id = $1`, companyId); clearEntriesError != nil {
		t.Fatalf("clearing entries after the rebuild: %v", clearEntriesError)
	}
}
