CREATE TABLE suppliers (
    id TEXT PRIMARY KEY,
    company_id TEXT NOT NULL REFERENCES companies (id),
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    contact_person TEXT NULL,
    phone TEXT NULL,
    email TEXT NULL,
    tin TEXT NULL,
    vrn TEXT NULL,
    address TEXT NULL,
    payment_terms_days INTEGER NOT NULL DEFAULT 0 CHECK (payment_terms_days BETWEEN 0 AND 365),
    opening_balance BIGINT NOT NULL DEFAULT 0 CHECK (opening_balance >= 0),
    notes TEXT NULL,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_by TEXT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_by TEXT NULL,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT suppliers_company_id_id_unique UNIQUE (company_id, id),
    FOREIGN KEY (company_id, created_by) REFERENCES users (company_id, id),
    FOREIGN KEY (company_id, updated_by) REFERENCES users (company_id, id)
);

CREATE UNIQUE INDEX idx_suppliers_company_id_active_name ON suppliers (company_id, lower(name)) WHERE is_active;
CREATE INDEX idx_suppliers_company_id_name ON suppliers (company_id, name);

CREATE TABLE supplier_document_counters (
    company_id TEXT NOT NULL REFERENCES companies (id),
    document_kind TEXT NOT NULL CHECK (document_kind IN ('purchase', 'purchase_order', 'supplier_payment', 'supplier_return')),
    last_number BIGINT NOT NULL DEFAULT 0 CHECK (last_number >= 0),
    PRIMARY KEY (company_id, document_kind)
);

ALTER TABLE products ADD COLUMN preferred_supplier_id TEXT NULL;
CREATE INDEX idx_products_company_id_preferred_supplier_id ON products (company_id, preferred_supplier_id);
