CREATE TABLE shops (
    id TEXT PRIMARY KEY,
    company_id TEXT NOT NULL REFERENCES companies (id),
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    address TEXT NULL,
    phone TEXT NULL,
    receipt_prefix TEXT NOT NULL DEFAULT 'SALE',
    next_receipt_number BIGINT NOT NULL DEFAULT 1 CHECK (next_receipt_number > 0),
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT shops_company_id_id_unique UNIQUE (company_id, id),
    CONSTRAINT shops_company_id_name_unique UNIQUE (company_id, name)
);
