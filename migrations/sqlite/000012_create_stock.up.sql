CREATE TABLE shop_stock (
    company_id TEXT NOT NULL,
    shop_id TEXT NOT NULL,
    product_id TEXT NOT NULL,
    quantity INTEGER NOT NULL DEFAULT 0 CHECK (quantity >= 0),
    min_stock INTEGER NOT NULL DEFAULT 5 CHECK (min_stock >= 0),
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (shop_id, product_id),
    FOREIGN KEY (company_id, shop_id) REFERENCES shops (company_id, id),
    FOREIGN KEY (company_id, product_id) REFERENCES products (company_id, id)
);

CREATE INDEX idx_shop_stock_company_id_product_id ON shop_stock (company_id, product_id);

CREATE TABLE stock_movements (
    id TEXT PRIMARY KEY,
    company_id TEXT NOT NULL,
    shop_id TEXT NOT NULL,
    product_id TEXT NOT NULL,
    change INTEGER NOT NULL CHECK (change <> 0),
    quantity_after INTEGER NOT NULL CHECK (quantity_after >= 0),
    reason TEXT NOT NULL CHECK (reason IN ('opening', 'sale', 'return', 'purchase', 'adjustment', 'damage', 'transfer_in', 'transfer_out')),
    reference TEXT NULL,
    user_id TEXT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (company_id, shop_id) REFERENCES shops (company_id, id),
    FOREIGN KEY (company_id, product_id) REFERENCES products (company_id, id),
    FOREIGN KEY (company_id, user_id) REFERENCES users (company_id, id)
);

CREATE INDEX idx_stock_movements_company_id_shop_id_created_at ON stock_movements (company_id, shop_id, created_at);
CREATE INDEX idx_stock_movements_company_id_product_id_created_at ON stock_movements (company_id, product_id, created_at);
