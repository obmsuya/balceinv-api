ALTER TABLE journal_entries ADD COLUMN paid_to_user_id UUID NULL;
ALTER TABLE journal_entries ADD CONSTRAINT journal_entries_paid_to_user_fkey FOREIGN KEY (company_id, paid_to_user_id) REFERENCES users (company_id, id);
CREATE INDEX idx_journal_entries_company_id_paid_to_user_id ON journal_entries (company_id, paid_to_user_id);
