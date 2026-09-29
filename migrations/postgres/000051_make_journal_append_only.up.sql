CREATE FUNCTION refuse_journal_change() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION '% on % is not allowed; post a reversal instead', TG_OP, TG_TABLE_NAME;
END;
$$;

CREATE TRIGGER journal_entries_append_only BEFORE UPDATE OR DELETE ON journal_entries
    FOR EACH ROW EXECUTE FUNCTION refuse_journal_change();

CREATE TRIGGER journal_lines_append_only BEFORE UPDATE OR DELETE ON journal_lines
    FOR EACH ROW EXECUTE FUNCTION refuse_journal_change();

CREATE TRIGGER journal_entries_refuse_truncate BEFORE TRUNCATE ON journal_entries
    FOR EACH STATEMENT EXECUTE FUNCTION refuse_journal_change();

CREATE TRIGGER journal_lines_refuse_truncate BEFORE TRUNCATE ON journal_lines
    FOR EACH STATEMENT EXECUTE FUNCTION refuse_journal_change();
