CREATE TABLE roles (
    id TEXT PRIMARY KEY,
    company_id TEXT NOT NULL REFERENCES companies (id),
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    is_owner BOOLEAN NOT NULL DEFAULT FALSE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT roles_company_id_id_unique UNIQUE (company_id, id),
    CONSTRAINT roles_company_id_name_unique UNIQUE (company_id, name)
);

CREATE UNIQUE INDEX idx_roles_one_owner_role_per_company ON roles (company_id) WHERE is_owner;

CREATE TABLE role_permissions (
    company_id TEXT NOT NULL,
    role_id TEXT NOT NULL,
    permission_id TEXT NOT NULL REFERENCES permissions (id),
    PRIMARY KEY (role_id, permission_id),
    FOREIGN KEY (company_id, role_id) REFERENCES roles (company_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_role_permissions_company_id_role_id ON role_permissions (company_id, role_id);
CREATE INDEX idx_role_permissions_permission_id ON role_permissions (permission_id);
