CREATE TABLE discounts (
    id TEXT PRIMARY KEY,
    company_id TEXT NOT NULL,
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    product_id TEXT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('percent', 'fixed')),
    value BIGINT NOT NULL,
    starts_at DATETIME NOT NULL,
    ends_at DATETIME NOT NULL,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_by TEXT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT discounts_company_id_id_unique UNIQUE (company_id, id),
    CONSTRAINT discounts_value_fits_kind CHECK (
        (kind = 'percent' AND value BETWEEN 1 AND 10000) OR (kind = 'fixed' AND value > 0)
    ),
    CONSTRAINT discounts_ends_after_start CHECK (ends_at > starts_at),
    FOREIGN KEY (company_id, product_id) REFERENCES products (company_id, id),
    FOREIGN KEY (company_id, created_by) REFERENCES users (company_id, id)
);

CREATE INDEX idx_discounts_company_id_product_id ON discounts (company_id, product_id);
CREATE INDEX idx_discounts_company_id_is_active_ends_at ON discounts (company_id, is_active, ends_at);
