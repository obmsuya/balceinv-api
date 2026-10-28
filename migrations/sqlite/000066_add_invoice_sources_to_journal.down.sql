PRAGMA defer_foreign_keys = ON;

CREATE TABLE journal_entries_copy AS SELECT * FROM journal_entries;

DROP TRIGGER journal_entries_refuse_update;
DROP TRIGGER journal_entries_refuse_delete;
DROP TABLE journal_entries;

CREATE TABLE journal_entries (
    id TEXT PRIMARY KEY,
    company_id TEXT NOT NULL REFERENCES companies (id),
    entry_number BIGINT NOT NULL CHECK (entry_number > 0),
    entry_date TEXT NOT NULL CHECK (entry_date GLOB '[0-9][0-9][0-9][0-9]-[0-9][0-9]-[0-9][0-9]'),
    source_type TEXT NOT NULL CHECK (source_type IN ('sale', 'sale_void', 'stock_adjustment', 'stock_transfer', 'expense', 'owner_in', 'owner_out', 'money_move', 'other_income', 'opening', 'manual', 'reversal', 'purchase', 'purchase_cancel', 'supplier_payment', 'supplier_payment_void', 'supplier_return', 'customer_payment', 'customer_payment_void', 'order_deposit', 'order_refund')),
    source_id TEXT NULL,
    client_ref TEXT NULL CHECK (client_ref IS NULL OR length(client_ref) BETWEEN 8 AND 64),
    memo TEXT NULL CHECK (memo IS NULL OR length(memo) <= 500),
    shop_id TEXT NULL,
    attachment_key TEXT NULL,
    receipt_number TEXT NULL CHECK (receipt_number IS NULL OR length(receipt_number) <= 60),
    supplier_tin TEXT NULL CHECK (supplier_tin IS NULL OR length(supplier_tin) <= 40),
    party_type TEXT NULL CHECK (party_type IN ('customer', 'supplier')),
    party_id TEXT NULL,
    reverses_entry_id TEXT NULL,
    created_by TEXT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    paid_to_user_id TEXT NULL REFERENCES users (id),
    CONSTRAINT journal_entries_company_id_id_unique UNIQUE (company_id, id),
    CONSTRAINT journal_entries_company_id_entry_number_unique UNIQUE (company_id, entry_number),
    CONSTRAINT journal_entries_company_id_source_unique UNIQUE (company_id, source_type, source_id),
    CONSTRAINT journal_entries_company_id_client_ref_unique UNIQUE (company_id, client_ref),
    CONSTRAINT journal_entries_party_is_complete CHECK ((party_type IS NULL) = (party_id IS NULL)),
    FOREIGN KEY (company_id, shop_id) REFERENCES shops (company_id, id),
    FOREIGN KEY (company_id, created_by) REFERENCES users (company_id, id),
    FOREIGN KEY (company_id, reverses_entry_id) REFERENCES journal_entries (company_id, id)
);

INSERT INTO journal_entries (id, company_id, entry_number, entry_date, source_type, source_id, client_ref, memo, shop_id, attachment_key, receipt_number, supplier_tin, party_type, party_id, reverses_entry_id, created_by, created_at, paid_to_user_id)
SELECT id, company_id, entry_number, entry_date, source_type, source_id, client_ref, memo, shop_id, attachment_key, receipt_number, supplier_tin, party_type, party_id, reverses_entry_id, created_by, created_at, paid_to_user_id
FROM journal_entries_copy
WHERE source_type NOT IN ('invoice', 'invoice_cancel', 'supplier_receipt', 'supplier_receipt_void')
ORDER BY company_id, entry_number;

DROP TABLE journal_entries_copy;

CREATE INDEX idx_journal_entries_company_id_entry_date ON journal_entries (company_id, entry_date);
CREATE INDEX idx_journal_entries_company_id_reverses_entry_id ON journal_entries (company_id, reverses_entry_id);
CREATE INDEX idx_journal_entries_company_id_paid_to_user_id ON journal_entries (company_id, paid_to_user_id);

CREATE TRIGGER journal_entries_refuse_update BEFORE UPDATE ON journal_entries
BEGIN
    SELECT RAISE(ABORT, 'journal entries cannot be changed; post a reversal instead');
END;

CREATE TRIGGER journal_entries_refuse_delete BEFORE DELETE ON journal_entries
WHEN NOT EXISTS (SELECT 1 FROM journal_clearing WHERE clearing_company_id = OLD.company_id)
BEGIN
    SELECT RAISE(ABORT, 'journal entries cannot be deleted; post a reversal instead');
END;
