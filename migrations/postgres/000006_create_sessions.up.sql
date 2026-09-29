CREATE TABLE sessions (
    id UUID PRIMARY KEY,
    token_hash TEXT NOT NULL,
    company_id UUID NOT NULL,
    user_id UUID NOT NULL,
    shop_id UUID NULL,
    ip_address TEXT NOT NULL DEFAULT '',
    user_agent TEXT NOT NULL DEFAULT '',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_seen_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    expires_at TIMESTAMPTZ NOT NULL,
    CONSTRAINT sessions_token_hash_unique UNIQUE (token_hash),
    FOREIGN KEY (company_id, user_id) REFERENCES users (company_id, id) ON DELETE CASCADE,
    FOREIGN KEY (company_id, shop_id) REFERENCES shops (company_id, id)
);

CREATE INDEX idx_sessions_company_id_user_id ON sessions (company_id, user_id);
CREATE INDEX idx_sessions_expires_at ON sessions (expires_at);

CREATE TABLE login_attempts (
    id UUID PRIMARY KEY,
    email TEXT NOT NULL,
    ip_address TEXT NOT NULL DEFAULT '',
    succeeded BOOLEAN NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX idx_login_attempts_email_created_at ON login_attempts (email, created_at);
CREATE INDEX idx_login_attempts_created_at ON login_attempts (created_at);
