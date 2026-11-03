CREATE TABLE password_reset_codes (
    id TEXT PRIMARY KEY,
    company_id TEXT NOT NULL REFERENCES companies (id),
    user_id TEXT NOT NULL,
    code_hash TEXT NOT NULL,
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    expires_at DATETIME NOT NULL,
    used_at DATETIME NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (company_id, user_id) REFERENCES users (company_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_password_reset_codes_company_id_user_id_created_at ON password_reset_codes (company_id, user_id, created_at);
