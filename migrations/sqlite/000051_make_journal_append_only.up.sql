CREATE TRIGGER journal_entries_refuse_update BEFORE UPDATE ON journal_entries
BEGIN
    SELECT RAISE(ABORT, 'journal entries cannot be changed; post a reversal instead');
END;

CREATE TRIGGER journal_entries_refuse_delete BEFORE DELETE ON journal_entries
BEGIN
    SELECT RAISE(ABORT, 'journal entries cannot be deleted; post a reversal instead');
END;

CREATE TRIGGER journal_lines_refuse_update BEFORE UPDATE ON journal_lines
BEGIN
    SELECT RAISE(ABORT, 'journal lines cannot be changed; post a reversal instead');
END;

CREATE TRIGGER journal_lines_refuse_delete BEFORE DELETE ON journal_lines
BEGIN
    SELECT RAISE(ABORT, 'journal lines cannot be deleted; post a reversal instead');
END;
