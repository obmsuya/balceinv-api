CREATE TABLE purchase_orders (
    id UUID PRIMARY KEY,
    company_id UUID NOT NULL REFERENCES companies (id),
    order_number TEXT NOT NULL,
    supplier_id UUID NOT NULL,
    shop_id UUID NOT NULL,
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'sent', 'partly_received', 'received', 'cancelled')),
    expected_date TEXT NULL CHECK (expected_date IS NULL OR length(expected_date) = 10),
    note TEXT NULL,
    created_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    sent_at TIMESTAMPTZ NULL,
    cancelled_by UUID NULL,
    cancelled_at TIMESTAMPTZ NULL,
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
    id UUID PRIMARY KEY,
    company_id UUID NOT NULL,
    purchase_order_id UUID NOT NULL,
    position INTEGER NOT NULL CHECK (position >= 0),
    product_id UUID NOT NULL,
    quantity_ordered INTEGER NOT NULL CHECK (quantity_ordered > 0),
    expected_unit_cost BIGINT NOT NULL DEFAULT 0 CHECK (expected_unit_cost >= 0),
    quantity_received INTEGER NOT NULL DEFAULT 0 CHECK (quantity_received >= 0),
    CONSTRAINT purchase_order_lines_order_product_unique UNIQUE (purchase_order_id, product_id),
    FOREIGN KEY (company_id, purchase_order_id) REFERENCES purchase_orders (company_id, id) ON DELETE CASCADE,
    FOREIGN KEY (company_id, product_id) REFERENCES products (company_id, id)
);

CREATE INDEX idx_purchase_order_lines_company_id_order_id ON purchase_order_lines (company_id, purchase_order_id);
CREATE INDEX idx_purchase_order_lines_company_id_product_id ON purchase_order_lines (company_id, product_id);
