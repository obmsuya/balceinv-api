CREATE OR REPLACE FUNCTION refuse_journal_change() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    RAISE EXCEPTION '% on % is not allowed; post a reversal instead', TG_OP, TG_TABLE_NAME;
END;
$$;
