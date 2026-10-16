CREATE TABLE journal_clearing (
    clearing_company_id TEXT PRIMARY KEY NOT NULL
);

DROP TRIGGER journal_entries_refuse_delete;
DROP TRIGGER journal_lines_refuse_delete;

CREATE TRIGGER journal_entries_refuse_delete BEFORE DELETE ON journal_entries
WHEN NOT EXISTS (SELECT 1 FROM journal_clearing WHERE clearing_company_id = OLD.company_id)
BEGIN
    SELECT RAISE(ABORT, 'journal entries cannot be deleted; post a reversal instead');
END;

CREATE TRIGGER journal_lines_refuse_delete BEFORE DELETE ON journal_lines
WHEN NOT EXISTS (SELECT 1 FROM journal_clearing WHERE clearing_company_id = OLD.company_id)
BEGIN
    SELECT RAISE(ABORT, 'journal lines cannot be deleted; post a reversal instead');
END;
