CREATE TABLE platform_staff (
    id TEXT PRIMARY KEY,
    email TEXT NOT NULL CHECK (email = lower(trim(email))),
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    password_hash TEXT NOT NULL,
    totp_secret TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'support' CHECK (role IN ('admin', 'support')),
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    last_signed_in_at DATETIME NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT platform_staff_email_unique UNIQUE (email)
);

CREATE TABLE platform_sessions (
    token_hash TEXT PRIMARY KEY,
    staff_id TEXT NOT NULL REFERENCES platform_staff (id) ON DELETE CASCADE,
    expires_at DATETIME NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_platform_sessions_staff_id ON platform_sessions (staff_id);

CREATE TABLE platform_audit (
    id TEXT PRIMARY KEY,
    staff_id TEXT NOT NULL REFERENCES platform_staff (id),
    action TEXT NOT NULL,
    company_id TEXT NULL REFERENCES companies (id),
    details TEXT NOT NULL DEFAULT '',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX idx_platform_audit_created_at ON platform_audit (created_at);
CREATE INDEX idx_platform_audit_staff_id ON platform_audit (staff_id);
CREATE INDEX idx_platform_audit_company_id ON platform_audit (company_id);

ALTER TABLE support_messages ADD COLUMN handled_at DATETIME NULL;
ALTER TABLE support_messages ADD COLUMN handled_by TEXT NULL;
