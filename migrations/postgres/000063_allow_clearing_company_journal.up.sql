CREATE OR REPLACE FUNCTION refuse_journal_change() RETURNS trigger
LANGUAGE plpgsql AS $$
BEGIN
    IF TG_OP = 'DELETE' AND OLD.company_id::text = current_setting('app.clearing_company_id', true) THEN
        RETURN OLD;
    END IF;
    RAISE EXCEPTION '% on % is not allowed; post a reversal instead', TG_OP, TG_TABLE_NAME;
END;
$$;
