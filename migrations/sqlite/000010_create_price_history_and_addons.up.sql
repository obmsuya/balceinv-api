CREATE TABLE price_history (
    id TEXT PRIMARY KEY,
    company_id TEXT NOT NULL,
    product_id TEXT NOT NULL,
    old_price BIGINT NOT NULL,
    new_price BIGINT NOT NULL,
    changed_by TEXT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (company_id, product_id) REFERENCES products (company_id, id) ON DELETE CASCADE,
    FOREIGN KEY (company_id, changed_by) REFERENCES users (company_id, id)
);

CREATE INDEX idx_price_history_company_id_product_id_created_at ON price_history (company_id, product_id, created_at);

CREATE TABLE product_addons (
    id TEXT PRIMARY KEY,
    company_id TEXT NOT NULL,
    product_id TEXT NOT NULL,
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    price BIGINT NOT NULL DEFAULT 0 CHECK (price >= 0),
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT product_addons_product_name_unique UNIQUE (company_id, product_id, name),
    FOREIGN KEY (company_id, product_id) REFERENCES products (company_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_product_addons_company_id_product_id ON product_addons (company_id, product_id);
