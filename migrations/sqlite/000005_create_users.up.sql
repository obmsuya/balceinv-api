CREATE TABLE users (
    id TEXT PRIMARY KEY,
    company_id TEXT NOT NULL REFERENCES companies (id),
    role_id TEXT NOT NULL,
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    email TEXT NOT NULL CHECK (email = lower(trim(email)) AND instr(email, '@') > 1),
    password_hash TEXT NOT NULL,
    locale TEXT NULL CHECK (locale IN ('en', 'sw')),
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    must_change_password BOOLEAN NOT NULL DEFAULT FALSE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT users_email_unique UNIQUE (email),
    CONSTRAINT users_company_id_id_unique UNIQUE (company_id, id),
    FOREIGN KEY (company_id, role_id) REFERENCES roles (company_id, id)
);

CREATE INDEX idx_users_company_id_name ON users (company_id, name);
CREATE INDEX idx_users_company_id_role_id ON users (company_id, role_id);

CREATE TABLE user_permissions (
    company_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    permission_id TEXT NOT NULL REFERENCES permissions (id),
    PRIMARY KEY (user_id, permission_id),
    FOREIGN KEY (company_id, user_id) REFERENCES users (company_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_user_permissions_company_id_user_id ON user_permissions (company_id, user_id);
CREATE INDEX idx_user_permissions_permission_id ON user_permissions (permission_id);

CREATE TABLE user_shops (
    company_id TEXT NOT NULL,
    user_id TEXT NOT NULL,
    shop_id TEXT NOT NULL,
    PRIMARY KEY (user_id, shop_id),
    FOREIGN KEY (company_id, user_id) REFERENCES users (company_id, id) ON DELETE CASCADE,
    FOREIGN KEY (company_id, shop_id) REFERENCES shops (company_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_user_shops_company_id_user_id ON user_shops (company_id, user_id);
CREATE INDEX idx_user_shops_company_id_shop_id ON user_shops (company_id, shop_id);
