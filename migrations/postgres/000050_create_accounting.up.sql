CREATE TABLE accounting_settings (
    company_id UUID PRIMARY KEY REFERENCES companies (id),
    started_on TEXT NOT NULL CHECK (started_on ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$'),
    started_at TIMESTAMPTZ NOT NULL,
    start_mode TEXT NOT NULL CHECK (start_mode IN ('today', 'history')),
    closed_until TEXT NULL CHECK (closed_until IS NULL OR closed_until ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$'),
    last_entry_number BIGINT NOT NULL DEFAULT 0 CHECK (last_entry_number >= 0),
    started_by UUID NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE TABLE accounts (
    id UUID PRIMARY KEY,
    company_id UUID NOT NULL REFERENCES companies (id),
    code TEXT NOT NULL CHECK (length(code) BETWEEN 1 AND 10),
    system_key TEXT NULL,
    name TEXT NULL CHECK (name IS NULL OR length(name) BETWEEN 1 AND 80),
    type TEXT NOT NULL CHECK (type IN ('asset', 'liability', 'equity', 'income', 'expense')),
    is_system BOOLEAN NOT NULL DEFAULT FALSE,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT accounts_company_id_id_unique UNIQUE (company_id, id),
    CONSTRAINT accounts_company_id_code_unique UNIQUE (company_id, code),
    CONSTRAINT accounts_company_id_system_key_unique UNIQUE (company_id, system_key),
    CONSTRAINT accounts_system_or_named CHECK ((is_system AND system_key IS NOT NULL) OR (NOT is_system AND system_key IS NULL AND name IS NOT NULL))
);

CREATE TABLE journal_entries (
    id UUID PRIMARY KEY,
    company_id UUID NOT NULL REFERENCES companies (id),
    entry_number BIGINT NOT NULL CHECK (entry_number > 0),
    entry_date TEXT NOT NULL CHECK (entry_date ~ '^[0-9]{4}-[0-9]{2}-[0-9]{2}$'),
    source_type TEXT NOT NULL CHECK (source_type IN ('sale', 'sale_void', 'stock_adjustment', 'stock_transfer', 'expense', 'owner_in', 'owner_out', 'money_move', 'other_income', 'opening', 'manual', 'reversal', 'purchase', 'purchase_cancel', 'supplier_payment', 'supplier_payment_void', 'supplier_return', 'customer_payment', 'customer_payment_void', 'order_deposit', 'order_refund')),
    source_id UUID NULL,
    client_ref TEXT NULL CHECK (client_ref IS NULL OR length(client_ref) BETWEEN 8 AND 64),
    memo TEXT NULL CHECK (memo IS NULL OR length(memo) <= 500),
    shop_id UUID NULL,
    attachment_key TEXT NULL,
    receipt_number TEXT NULL CHECK (receipt_number IS NULL OR length(receipt_number) <= 60),
    supplier_tin TEXT NULL CHECK (supplier_tin IS NULL OR length(supplier_tin) <= 40),
    party_type TEXT NULL CHECK (party_type IN ('customer', 'supplier')),
    party_id UUID NULL,
    reverses_entry_id UUID NULL,
    created_by UUID NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT journal_entries_company_id_id_unique UNIQUE (company_id, id),
    CONSTRAINT journal_entries_company_id_entry_number_unique UNIQUE (company_id, entry_number),
    CONSTRAINT journal_entries_company_id_source_unique UNIQUE (company_id, source_type, source_id),
    CONSTRAINT journal_entries_company_id_client_ref_unique UNIQUE (company_id, client_ref),
    CONSTRAINT journal_entries_party_is_complete CHECK ((party_type IS NULL) = (party_id IS NULL)),
    FOREIGN KEY (company_id, shop_id) REFERENCES shops (company_id, id),
    FOREIGN KEY (company_id, created_by) REFERENCES users (company_id, id),
    FOREIGN KEY (company_id, reverses_entry_id) REFERENCES journal_entries (company_id, id)
);

CREATE INDEX idx_journal_entries_company_id_entry_date ON journal_entries (company_id, entry_date);
CREATE INDEX idx_journal_entries_company_id_reverses_entry_id ON journal_entries (company_id, reverses_entry_id);

CREATE TABLE journal_lines (
    company_id UUID NOT NULL,
    entry_id UUID NOT NULL,
    line_no INTEGER NOT NULL CHECK (line_no > 0),
    account_id UUID NOT NULL,
    debit BIGINT NOT NULL DEFAULT 0 CHECK (debit >= 0),
    credit BIGINT NOT NULL DEFAULT 0 CHECK (credit >= 0),
    shop_id UUID NULL,
    PRIMARY KEY (entry_id, line_no),
    CONSTRAINT journal_lines_one_side CHECK ((debit > 0 AND credit = 0) OR (credit > 0 AND debit = 0)),
    FOREIGN KEY (company_id, entry_id) REFERENCES journal_entries (company_id, id),
    FOREIGN KEY (company_id, account_id) REFERENCES accounts (company_id, id),
    FOREIGN KEY (company_id, shop_id) REFERENCES shops (company_id, id)
);

CREATE INDEX idx_journal_lines_company_id_account_id ON journal_lines (company_id, account_id);
CREATE INDEX idx_journal_lines_company_id_entry_id ON journal_lines (company_id, entry_id);
