CREATE TABLE catalog_products (
    id TEXT PRIMARY KEY,
    business_type TEXT NOT NULL CHECK (length(business_type) BETWEEN 2 AND 32 AND business_type = lower(business_type)),
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    name_key TEXT NOT NULL,
    category TEXT NULL,
    sub_category TEXT NULL,
    unit TEXT NOT NULL DEFAULT 'pcs',
    sku_prefix TEXT NOT NULL DEFAULT 'GEN',
    default_price BIGINT NOT NULL DEFAULT 0 CHECK (default_price >= 0),
    metadata TEXT NOT NULL DEFAULT '{}',
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT catalog_products_business_type_name_key_unique UNIQUE (business_type, name_key)
);

CREATE INDEX idx_catalog_products_business_type_name ON catalog_products (business_type, name);
