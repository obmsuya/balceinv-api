ALTER TABLE journal_entries ADD COLUMN paid_to_user_id TEXT NULL REFERENCES users (id);
CREATE INDEX idx_journal_entries_company_id_paid_to_user_id ON journal_entries (company_id, paid_to_user_id);
