ALTER TABLE sales ADD COLUMN voided_at TIMESTAMPTZ NULL;
ALTER TABLE sales ADD COLUMN voided_by UUID NULL;
ALTER TABLE sales ADD COLUMN void_reason TEXT NULL;
ALTER TABLE sales ADD CONSTRAINT sales_void_is_complete CHECK ((voided_at IS NULL AND voided_by IS NULL AND void_reason IS NULL) OR (voided_at IS NOT NULL AND voided_by IS NOT NULL AND void_reason IS NOT NULL));
ALTER TABLE sales ADD CONSTRAINT sales_voided_by_fkey FOREIGN KEY (company_id, voided_by) REFERENCES users (company_id, id);

CREATE INDEX idx_sales_company_id_voided_at ON sales (company_id, voided_at);
CREATE INDEX idx_sales_company_id_voided_by ON sales (company_id, voided_by);

CREATE TABLE fiscal_credit_notes (
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

CREATE INDEX idx_fiscal_credit_notes_company_id_status_created_at ON fiscal_credit_notes (company_id, status, created_at);

ALTER TABLE fiscal_credit_notes ENABLE ROW LEVEL SECURITY;
ALTER TABLE fiscal_credit_notes FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON fiscal_credit_notes
    USING (company_id = nullif(current_setting('app.company_id', true), '')::uuid)
    WITH CHECK (company_id = nullif(current_setting('app.company_id', true), '')::uuid);
