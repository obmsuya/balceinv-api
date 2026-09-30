CREATE TABLE company_subscriptions (
    company_id UUID PRIMARY KEY REFERENCES companies(id) ON DELETE CASCADE,
    license_key TEXT NOT NULL DEFAULT 'trial',
    expires_at TIMESTAMPTZ NOT NULL,
    days_granted INTEGER NOT NULL DEFAULT 14 CHECK (days_granted >= 0),
    max_devices INTEGER NOT NULL DEFAULT 1 CHECK (max_devices >= 0),
    is_trial BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

INSERT INTO company_subscriptions (company_id, expires_at)
SELECT id, now() + INTERVAL '14 days' FROM companies;

ALTER TABLE company_subscriptions ENABLE ROW LEVEL SECURITY;
ALTER TABLE company_subscriptions FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON company_subscriptions
    USING (company_id = nullif(current_setting('app.company_id', true), '')::uuid)
    WITH CHECK (company_id = nullif(current_setting('app.company_id', true), '')::uuid);
