CREATE TABLE purchases (
    id TEXT PRIMARY KEY,
    company_id TEXT NOT NULL REFERENCES companies (id),
    purchase_number TEXT NOT NULL,
    shop_id TEXT NOT NULL,
    supplier_id TEXT NULL,
    supplier_invoice_number TEXT NULL,
    invoice_date TEXT NULL CHECK (invoice_date IS NULL OR length(invoice_date) = 10),
    received_at DATETIME NOT NULL,
    status TEXT NOT NULL DEFAULT 'received' CHECK (status IN ('received', 'cancelled')),
    prices_include_vat BOOLEAN NOT NULL DEFAULT FALSE,
    subtotal BIGINT NOT NULL CHECK (subtotal >= 0),
    vat_total BIGINT NOT NULL DEFAULT 0 CHECK (vat_total >= 0),
    total BIGINT NOT NULL CHECK (total >= 0),
    note TEXT NULL,
    attachment_key TEXT NULL,
    purchase_order_id TEXT NULL,
    client_ref TEXT NOT NULL CHECK (length(client_ref) BETWEEN 8 AND 64),
    created_by TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    cancelled_by TEXT NULL,
    cancelled_at DATETIME NULL,
    cancel_reason TEXT NULL,
    CONSTRAINT purchases_company_id_id_unique UNIQUE (company_id, id),
    CONSTRAINT purchases_company_id_purchase_number_unique UNIQUE (company_id, purchase_number),
    CONSTRAINT purchases_company_id_client_ref_unique UNIQUE (company_id, client_ref),
    CONSTRAINT purchases_total_adds_up CHECK (total = subtotal + vat_total),
    CONSTRAINT purchases_cancelled_has_time CHECK ((status = 'cancelled') = (cancelled_at IS NOT NULL)),
    FOREIGN KEY (company_id, shop_id) REFERENCES shops (company_id, id),
    FOREIGN KEY (company_id, supplier_id) REFERENCES suppliers (company_id, id),
    FOREIGN KEY (company_id, purchase_order_id) REFERENCES purchase_orders (company_id, id),
    FOREIGN KEY (company_id, created_by) REFERENCES users (company_id, id),
    FOREIGN KEY (company_id, cancelled_by) REFERENCES users (company_id, id)
);

CREATE INDEX idx_purchases_company_id_received_at ON purchases (company_id, received_at);
CREATE INDEX idx_purchases_company_id_supplier_id_received_at ON purchases (company_id, supplier_id, received_at);
CREATE INDEX idx_purchases_company_id_purchase_order_id ON purchases (company_id, purchase_order_id);

CREATE TABLE purchase_lines (
    id TEXT PRIMARY KEY,
    company_id TEXT NOT NULL,
    purchase_id TEXT NOT NULL,
    position INTEGER NOT NULL CHECK (position >= 0),
    product_id TEXT NOT NULL,
    quantity INTEGER NOT NULL CHECK (quantity > 0),
    unit_cost BIGINT NOT NULL CHECK (unit_cost >= 0),
    vat_amount BIGINT NOT NULL DEFAULT 0 CHECK (vat_amount >= 0),
    line_total BIGINT NOT NULL CHECK (line_total >= 0),
    CONSTRAINT purchase_lines_purchase_product_unique UNIQUE (purchase_id, product_id),
    CONSTRAINT purchase_lines_vat_within_total CHECK (vat_amount <= line_total),
    FOREIGN KEY (company_id, purchase_id) REFERENCES purchases (company_id, id) ON DELETE CASCADE,
    FOREIGN KEY (company_id, product_id) REFERENCES products (company_id, id)
);

CREATE INDEX idx_purchase_lines_company_id_purchase_id ON purchase_lines (company_id, purchase_id);
CREATE INDEX idx_purchase_lines_company_id_product_id ON purchase_lines (company_id, product_id);

CREATE TABLE supplier_payments (
    id TEXT PRIMARY KEY,
    company_id TEXT NOT NULL REFERENCES companies (id),
    payment_number TEXT NOT NULL,
    supplier_id TEXT NULL,
    purchase_id TEXT NULL,
    shop_id TEXT NOT NULL,
    amount BIGINT NOT NULL CHECK (amount > 0),
    method TEXT NOT NULL CHECK (method IN ('cash', 'bank', 'mobile')),
    reference TEXT NULL,
    paid_at DATETIME NOT NULL,
    created_by TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    voided_by TEXT NULL,
    voided_at DATETIME NULL,
    void_reason TEXT NULL,
    CONSTRAINT supplier_payments_company_id_id_unique UNIQUE (company_id, id),
    CONSTRAINT supplier_payments_company_id_payment_number_unique UNIQUE (company_id, payment_number),
    CONSTRAINT supplier_payments_for_supplier_or_purchase CHECK (supplier_id IS NOT NULL OR purchase_id IS NOT NULL),
    CONSTRAINT supplier_payments_void_has_reason CHECK ((voided_at IS NULL) = (void_reason IS NULL)),
    FOREIGN KEY (company_id, supplier_id) REFERENCES suppliers (company_id, id),
    FOREIGN KEY (company_id, purchase_id) REFERENCES purchases (company_id, id),
    FOREIGN KEY (company_id, shop_id) REFERENCES shops (company_id, id),
    FOREIGN KEY (company_id, created_by) REFERENCES users (company_id, id),
    FOREIGN KEY (company_id, voided_by) REFERENCES users (company_id, id)
);

CREATE INDEX idx_supplier_payments_company_id_supplier_id_paid_at ON supplier_payments (company_id, supplier_id, paid_at);
CREATE INDEX idx_supplier_payments_company_id_purchase_id ON supplier_payments (company_id, purchase_id);
CREATE INDEX idx_supplier_payments_company_id_paid_at ON supplier_payments (company_id, paid_at);
