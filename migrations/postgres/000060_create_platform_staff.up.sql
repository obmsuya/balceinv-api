CREATE TABLE platform_staff (
    id UUID PRIMARY KEY,
    email TEXT NOT NULL CHECK (email = lower(trim(email))),
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    password_hash TEXT NOT NULL,
    totp_secret TEXT NOT NULL,
    role TEXT NOT NULL DEFAULT 'support' CHECK (role IN ('admin', 'support')),
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    last_signed_in_at TIMESTAMPTZ NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT platform_staff_email_unique UNIQUE (email)
);

CREATE TABLE platform_sessions (
    token_hash TEXT PRIMARY KEY,
    staff_id UUID NOT NULL REFERENCES platform_staff (id) ON DELETE CASCADE,
    expires_at TIMESTAMPTZ NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_platform_sessions_staff_id ON platform_sessions (staff_id);

CREATE TABLE platform_audit (
    id UUID PRIMARY KEY,
    staff_id UUID NOT NULL REFERENCES platform_staff (id),
    action TEXT NOT NULL,
    company_id UUID NULL REFERENCES companies (id),
    details TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_platform_audit_created_at ON platform_audit (created_at);
CREATE INDEX idx_platform_audit_staff_id ON platform_audit (staff_id);
CREATE INDEX idx_platform_audit_company_id ON platform_audit (company_id);

ALTER TABLE support_messages ADD COLUMN handled_at TIMESTAMPTZ NULL;
ALTER TABLE support_messages ADD COLUMN handled_by UUID NULL REFERENCES platform_staff (id);

CREATE INDEX idx_support_messages_handled_by ON support_messages (handled_by);

CREATE POLICY platform_admin_read ON companies FOR SELECT USING (current_setting('app.platform_admin', true) = 'on');
CREATE POLICY platform_admin_read ON users FOR SELECT USING (current_setting('app.platform_admin', true) = 'on');
CREATE POLICY platform_admin_read ON roles FOR SELECT USING (current_setting('app.platform_admin', true) = 'on');
CREATE POLICY platform_admin_read ON shops FOR SELECT USING (current_setting('app.platform_admin', true) = 'on');
CREATE POLICY platform_admin_read ON sales FOR SELECT USING (current_setting('app.platform_admin', true) = 'on');
CREATE POLICY platform_admin_read ON company_subscriptions FOR SELECT USING (current_setting('app.platform_admin', true) = 'on');
CREATE POLICY platform_admin_read ON support_messages FOR SELECT USING (current_setting('app.platform_admin', true) = 'on');
