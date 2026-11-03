CREATE TABLE password_reset_codes (
    id UUID PRIMARY KEY,
    company_id UUID NOT NULL REFERENCES companies (id),
    user_id UUID NOT NULL,
    code_hash TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    expires_at TIMESTAMPTZ NOT NULL,
    used_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (company_id, user_id) REFERENCES users (company_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_password_reset_codes_company_id_user_id_created_at ON password_reset_codes (company_id, user_id, created_at);

ALTER TABLE password_reset_codes ENABLE ROW LEVEL SECURITY;
ALTER TABLE password_reset_codes FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON password_reset_codes
    USING (company_id = nullif(current_setting('app.company_id', true), '')::uuid)
    WITH CHECK (company_id = nullif(current_setting('app.company_id', true), '')::uuid);
