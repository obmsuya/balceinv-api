ALTER TABLE sales ADD COLUMN voided_at DATETIME NULL;
ALTER TABLE sales ADD COLUMN voided_by TEXT NULL REFERENCES users (id);
ALTER TABLE sales ADD COLUMN void_reason TEXT NULL;

CREATE INDEX idx_sales_company_id_voided_at ON sales (company_id, voided_at);

CREATE TABLE fiscal_credit_notes (
    company_id TEXT NOT NULL REFERENCES companies (id),
    sale_id TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sending', 'sent', 'failed')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    verification_code TEXT NULL,
    verification_url TEXT NULL,
    last_error TEXT NULL,
    sent_at DATETIME NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (company_id, sale_id),
    FOREIGN KEY (company_id, sale_id) REFERENCES sales (company_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_fiscal_credit_notes_company_id_status_created_at ON fiscal_credit_notes (company_id, status, created_at);
