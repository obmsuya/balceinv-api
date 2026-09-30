CREATE TABLE company_subscriptions (
    company_id TEXT PRIMARY KEY REFERENCES companies(id) ON DELETE CASCADE,
    license_key TEXT NOT NULL DEFAULT 'trial',
    expires_at DATETIME NOT NULL,
    days_granted INTEGER NOT NULL DEFAULT 14 CHECK (days_granted >= 0),
    max_devices INTEGER NOT NULL DEFAULT 1 CHECK (max_devices >= 0),
    is_trial BOOLEAN NOT NULL DEFAULT TRUE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

INSERT INTO company_subscriptions (company_id, expires_at)
SELECT id, datetime('now', '+14 days') FROM companies;
