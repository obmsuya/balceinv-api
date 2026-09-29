DROP TRIGGER journal_lines_refuse_truncate ON journal_lines;
DROP TRIGGER journal_entries_refuse_truncate ON journal_entries;
DROP TRIGGER journal_lines_append_only ON journal_lines;
DROP TRIGGER journal_entries_append_only ON journal_entries;
DROP FUNCTION refuse_journal_change();
