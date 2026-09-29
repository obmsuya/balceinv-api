CREATE TABLE stock_transfers (
    id UUID PRIMARY KEY,
    company_id UUID NOT NULL,
    from_shop_id UUID NOT NULL,
    to_shop_id UUID NOT NULL,
    note TEXT NULL,
    user_id UUID NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT stock_transfers_company_id_id_unique UNIQUE (company_id, id),
    CONSTRAINT stock_transfers_different_shops CHECK (from_shop_id <> to_shop_id),
    FOREIGN KEY (company_id, from_shop_id) REFERENCES shops (company_id, id),
    FOREIGN KEY (company_id, to_shop_id) REFERENCES shops (company_id, id),
    FOREIGN KEY (company_id, user_id) REFERENCES users (company_id, id)
);

CREATE INDEX idx_stock_transfers_company_id_created_at ON stock_transfers (company_id, created_at);
CREATE INDEX idx_stock_transfers_company_id_from_shop_id ON stock_transfers (company_id, from_shop_id);
CREATE INDEX idx_stock_transfers_company_id_to_shop_id ON stock_transfers (company_id, to_shop_id);

CREATE TABLE stock_transfer_items (
    company_id UUID NOT NULL,
    transfer_id UUID NOT NULL,
    product_id UUID NOT NULL,
    quantity INTEGER NOT NULL CHECK (quantity > 0),
    PRIMARY KEY (transfer_id, product_id),
    FOREIGN KEY (company_id, transfer_id) REFERENCES stock_transfers (company_id, id) ON DELETE CASCADE,
    FOREIGN KEY (company_id, product_id) REFERENCES products (company_id, id)
);

CREATE INDEX idx_stock_transfer_items_company_id_product_id ON stock_transfer_items (company_id, product_id);
