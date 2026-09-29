CREATE TABLE purchase_orders (
    id TEXT PRIMARY KEY,
    company_id TEXT NOT NULL REFERENCES companies (id),
    order_number TEXT NOT NULL,
    supplier_id TEXT NOT NULL,
    shop_id TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'sent', 'partly_received', 'received', 'cancelled')),
    expected_date TEXT NULL CHECK (expected_date IS NULL OR length(expected_date) = 10),
    note TEXT NULL,
    created_by TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    sent_at DATETIME NULL,
    cancelled_by TEXT NULL,
    cancelled_at DATETIME NULL,
    cancel_reason TEXT NULL,
    CONSTRAINT purchase_orders_company_id_id_unique UNIQUE (company_id, id),
    CONSTRAINT purchase_orders_company_id_order_number_unique UNIQUE (company_id, order_number),
    CONSTRAINT purchase_orders_cancelled_has_time CHECK ((status = 'cancelled') = (cancelled_at IS NOT NULL)),
    FOREIGN KEY (company_id, supplier_id) REFERENCES suppliers (company_id, id),
    FOREIGN KEY (company_id, shop_id) REFERENCES shops (company_id, id),
    FOREIGN KEY (company_id, created_by) REFERENCES users (company_id, id),
    FOREIGN KEY (company_id, cancelled_by) REFERENCES users (company_id, id)
);

CREATE INDEX idx_purchase_orders_company_id_supplier_id ON purchase_orders (company_id, supplier_id);
CREATE INDEX idx_purchase_orders_company_id_created_at ON purchase_orders (company_id, created_at);

CREATE TABLE purchase_order_lines (
    id TEXT PRIMARY KEY,
    company_id TEXT NOT NULL,
    purchase_order_id TEXT NOT NULL,
    position INTEGER NOT NULL CHECK (position >= 0),
    product_id TEXT NOT NULL,
    quantity_ordered INTEGER NOT NULL CHECK (quantity_ordered > 0),
    expected_unit_cost BIGINT NOT NULL DEFAULT 0 CHECK (expected_unit_cost >= 0),
    quantity_received INTEGER NOT NULL DEFAULT 0 CHECK (quantity_received >= 0),
    CONSTRAINT purchase_order_lines_order_product_unique UNIQUE (purchase_order_id, product_id),
    FOREIGN KEY (company_id, purchase_order_id) REFERENCES purchase_orders (company_id, id) ON DELETE CASCADE,
    FOREIGN KEY (company_id, product_id) REFERENCES products (company_id, id)
);

CREATE INDEX idx_purchase_order_lines_company_id_order_id ON purchase_order_lines (company_id, purchase_order_id);
CREATE INDEX idx_purchase_order_lines_company_id_product_id ON purchase_order_lines (company_id, product_id);
