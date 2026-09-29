CREATE TABLE catalog_products (
    id UUID PRIMARY KEY,
    business_type TEXT NOT NULL CHECK (business_type ~ '^[a-z][a-z_]{1,31}$'),
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    name_key TEXT NOT NULL,
    category TEXT NULL,
    sub_category TEXT NULL,
    unit TEXT NOT NULL DEFAULT 'pcs',
    sku_prefix TEXT NOT NULL DEFAULT 'GEN',
    default_price BIGINT NOT NULL DEFAULT 0 CHECK (default_price >= 0),
    metadata JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT catalog_products_business_type_name_key_unique UNIQUE (business_type, name_key)
);

CREATE INDEX idx_catalog_products_business_type_name ON catalog_products (business_type, name);
