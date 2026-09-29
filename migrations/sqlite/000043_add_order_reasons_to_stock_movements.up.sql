CREATE TABLE stock_movements_with_order_reasons (
    id TEXT PRIMARY KEY,
    company_id TEXT NOT NULL,
    shop_id TEXT NOT NULL,
    product_id TEXT NOT NULL,
    change INTEGER NOT NULL CHECK (change <> 0),
    quantity_after INTEGER NOT NULL CHECK (quantity_after >= 0),
    reason TEXT NOT NULL CHECK (reason IN ('opening', 'sale', 'return', 'purchase', 'adjustment', 'damage', 'transfer_in', 'transfer_out', 'order_reserved', 'order_cancelled')),
    reference TEXT NULL,
    user_id TEXT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    FOREIGN KEY (company_id, shop_id) REFERENCES shops (company_id, id),
    FOREIGN KEY (company_id, product_id) REFERENCES products (company_id, id),
    FOREIGN KEY (company_id, user_id) REFERENCES users (company_id, id)
);

INSERT INTO stock_movements_with_order_reasons (id, company_id, shop_id, product_id, change, quantity_after, reason, reference, user_id, created_at)
SELECT id, company_id, shop_id, product_id, change, quantity_after, reason, reference, user_id, created_at FROM stock_movements;

DROP TABLE stock_movements;

ALTER TABLE stock_movements_with_order_reasons RENAME TO stock_movements;

CREATE INDEX idx_stock_movements_company_id_shop_id_created_at ON stock_movements (company_id, shop_id, created_at);
CREATE INDEX idx_stock_movements_company_id_product_id_created_at ON stock_movements (company_id, product_id, created_at);
