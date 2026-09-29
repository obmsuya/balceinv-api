CREATE TABLE products (
    id UUID PRIMARY KEY,
    company_id UUID NOT NULL REFERENCES companies (id),
    parent_id UUID NULL,
    sku TEXT NOT NULL CHECK (length(trim(sku)) > 0),
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    variant_label TEXT NOT NULL DEFAULT '',
    price BIGINT NOT NULL CHECK (price >= 0),
    cost_price BIGINT NOT NULL DEFAULT 0 CHECK (cost_price >= 0),
    wholesale_price BIGINT NULL CHECK (wholesale_price >= 0),
    wholesale_min INTEGER NOT NULL DEFAULT 10 CHECK (wholesale_min >= 1),
    category TEXT NULL,
    unit TEXT NOT NULL DEFAULT 'pcs',
    pieces_per_unit INTEGER NOT NULL DEFAULT 1 CHECK (pieces_per_unit >= 1),
    image_key TEXT NULL,
    metadata JSONB NOT NULL DEFAULT '{}',
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT products_company_id_id_unique UNIQUE (company_id, id),
    CONSTRAINT products_company_id_sku_unique UNIQUE (company_id, sku),
    CONSTRAINT products_not_own_parent CHECK (parent_id IS NULL OR parent_id <> id),
    FOREIGN KEY (company_id, parent_id) REFERENCES products (company_id, id)
);

CREATE INDEX idx_products_company_id_name ON products (company_id, name);
CREATE INDEX idx_products_company_id_category ON products (company_id, category);
CREATE INDEX idx_products_company_id_parent_id ON products (company_id, parent_id);

CREATE TABLE barcodes (
    id UUID PRIMARY KEY,
    company_id UUID NOT NULL,
    product_id UUID NOT NULL,
    code TEXT NOT NULL CHECK (length(trim(code)) > 0),
    pack_size INTEGER NOT NULL DEFAULT 1 CHECK (pack_size >= 1),
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT barcodes_company_id_code_unique UNIQUE (company_id, code),
    FOREIGN KEY (company_id, product_id) REFERENCES products (company_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_barcodes_company_id_product_id ON barcodes (company_id, product_id);
