CREATE TABLE fiscal_receipts (
    company_id UUID NOT NULL REFERENCES companies (id),
    sale_id UUID NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sending', 'sent', 'failed')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    verification_code TEXT NULL,
    verification_url TEXT NULL,
    last_error TEXT NULL,
    sent_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (company_id, sale_id),
    FOREIGN KEY (company_id, sale_id) REFERENCES sales (company_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_fiscal_receipts_company_id_status_created_at ON fiscal_receipts (company_id, status, created_at);
