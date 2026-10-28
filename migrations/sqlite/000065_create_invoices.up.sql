CREATE TABLE supplier_receipts (
    id TEXT PRIMARY KEY,
    company_id TEXT NOT NULL REFERENCES companies (id),
    supplier_id TEXT NOT NULL,
    shop_id TEXT NULL,
    amount BIGINT NOT NULL CHECK (amount > 0),
    method TEXT NOT NULL CHECK (method IN ('cash', 'bank', 'mobile')),
    reference TEXT NULL CHECK (reference IS NULL OR length(reference) <= 80),
    received_at DATETIME NOT NULL,
    created_by TEXT NOT NULL,
    voided_at DATETIME NULL,
    voided_by TEXT NULL,
    void_reason TEXT NULL,
    CONSTRAINT supplier_receipts_company_id_id_unique UNIQUE (company_id, id),
    CONSTRAINT supplier_receipts_void_is_complete CHECK ((voided_at IS NULL AND voided_by IS NULL AND void_reason IS NULL) OR (voided_at IS NOT NULL AND voided_by IS NOT NULL AND void_reason IS NOT NULL)),
    FOREIGN KEY (company_id, supplier_id) REFERENCES suppliers (company_id, id),
    FOREIGN KEY (company_id, shop_id) REFERENCES shops (company_id, id),
    FOREIGN KEY (company_id, created_by) REFERENCES users (company_id, id),
    FOREIGN KEY (company_id, voided_by) REFERENCES users (company_id, id)
);

CREATE INDEX idx_supplier_receipts_company_id_supplier_id_received_at ON supplier_receipts (company_id, supplier_id, received_at);

CREATE TABLE invoices (
    id TEXT PRIMARY KEY,
    company_id TEXT NOT NULL REFERENCES companies (id),
    shop_id TEXT NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('invoice', 'proforma', 'quotation', 'delivery_note', 'supplier_invoice')),
    number TEXT NOT NULL CHECK (length(number) BETWEEN 1 AND 30),
    status TEXT NOT NULL DEFAULT 'draft' CHECK (status IN ('draft', 'issued', 'accepted', 'cancelled')),
    customer_id TEXT NULL,
    supplier_id TEXT NULL,
    issue_date TEXT NOT NULL CHECK (length(issue_date) = 10),
    due_date TEXT NULL CHECK (due_date IS NULL OR length(due_date) = 10),
    reference TEXT NULL CHECK (reference IS NULL OR length(reference) <= 60),
    notes TEXT NULL CHECK (notes IS NULL OR length(notes) <= 1000),
    terms TEXT NULL CHECK (terms IS NULL OR length(terms) <= 1000),
    subtotal BIGINT NOT NULL DEFAULT 0 CHECK (subtotal >= 0),
    discount_total BIGINT NOT NULL DEFAULT 0 CHECK (discount_total >= 0),
    total BIGINT NOT NULL DEFAULT 0 CHECK (total >= 0),
    tax_total BIGINT NOT NULL DEFAULT 0 CHECK (tax_total >= 0),
    tax_rate_basis_points INTEGER NOT NULL DEFAULT 0 CHECK (tax_rate_basis_points BETWEEN 0 AND 10000),
    source_invoice_id TEXT NULL,
    delivered_at DATETIME NULL,
    delivered_by TEXT NULL,
    cancelled_at DATETIME NULL,
    cancelled_by TEXT NULL,
    cancel_reason TEXT NULL,
    created_by TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT invoices_company_id_id_unique UNIQUE (company_id, id),
    CONSTRAINT invoices_company_id_number_unique UNIQUE (company_id, number),
    CONSTRAINT invoices_total_is_subtotal_less_discount CHECK (total = subtotal - discount_total),
    CONSTRAINT invoices_party_matches_kind CHECK ((kind = 'supplier_invoice' AND supplier_id IS NOT NULL AND customer_id IS NULL) OR (kind <> 'supplier_invoice' AND customer_id IS NOT NULL AND supplier_id IS NULL)),
    CONSTRAINT invoices_delivery_is_complete CHECK ((delivered_at IS NULL) = (delivered_by IS NULL)),
    CONSTRAINT invoices_only_bills_are_delivered CHECK (delivered_at IS NULL OR kind IN ('invoice', 'supplier_invoice')),
    CONSTRAINT invoices_only_quotations_are_accepted CHECK (status <> 'accepted' OR kind IN ('quotation', 'proforma')),
    CONSTRAINT invoices_cancel_is_complete CHECK ((status = 'cancelled') = (cancelled_at IS NOT NULL AND cancelled_by IS NOT NULL AND cancel_reason IS NOT NULL)),
    FOREIGN KEY (company_id, shop_id) REFERENCES shops (company_id, id),
    FOREIGN KEY (company_id, customer_id) REFERENCES customers (company_id, id),
    FOREIGN KEY (company_id, supplier_id) REFERENCES suppliers (company_id, id),
    FOREIGN KEY (company_id, source_invoice_id) REFERENCES invoices (company_id, id),
    FOREIGN KEY (company_id, delivered_by) REFERENCES users (company_id, id),
    FOREIGN KEY (company_id, cancelled_by) REFERENCES users (company_id, id),
    FOREIGN KEY (company_id, created_by) REFERENCES users (company_id, id)
);

CREATE INDEX idx_invoices_company_id_kind_issue_date ON invoices (company_id, kind, issue_date);
CREATE INDEX idx_invoices_company_id_customer_id ON invoices (company_id, customer_id);
CREATE INDEX idx_invoices_company_id_supplier_id ON invoices (company_id, supplier_id);

CREATE TABLE invoice_lines (
    id TEXT PRIMARY KEY,
    company_id TEXT NOT NULL,
    invoice_id TEXT NOT NULL,
    position INTEGER NOT NULL CHECK (position >= 0),
    product_id TEXT NULL,
    description TEXT NOT NULL CHECK (length(description) BETWEEN 1 AND 300),
    unit TEXT NOT NULL DEFAULT '' CHECK (length(unit) <= 20),
    quantity INTEGER NOT NULL CHECK (quantity > 0),
    unit_price BIGINT NOT NULL CHECK (unit_price >= 0),
    discount_amount BIGINT NOT NULL DEFAULT 0 CHECK (discount_amount >= 0),
    line_total BIGINT NOT NULL CHECK (line_total >= 0),
    unit_cost BIGINT NOT NULL DEFAULT 0 CHECK (unit_cost >= 0),
    CONSTRAINT invoice_lines_company_id_id_unique UNIQUE (company_id, id),
    CONSTRAINT invoice_lines_invoice_id_position_unique UNIQUE (invoice_id, position),
    CONSTRAINT invoice_lines_line_total_adds_up CHECK (line_total = quantity * unit_price - discount_amount),
    FOREIGN KEY (company_id, invoice_id) REFERENCES invoices (company_id, id) ON DELETE CASCADE,
    FOREIGN KEY (company_id, product_id) REFERENCES products (company_id, id)
);

CREATE INDEX idx_invoice_lines_company_id_invoice_id ON invoice_lines (company_id, invoice_id);
CREATE INDEX idx_invoice_lines_company_id_product_id ON invoice_lines (company_id, product_id);

CREATE TABLE invoice_payments (
    id TEXT PRIMARY KEY,
    company_id TEXT NOT NULL,
    invoice_id TEXT NOT NULL,
    method TEXT NOT NULL CHECK (method IN ('cash', 'card', 'mobile', 'bank', 'offset')),
    amount BIGINT NOT NULL CHECK (amount > 0),
    customer_payment_id TEXT NULL,
    supplier_receipt_id TEXT NULL,
    created_by TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    voided_at DATETIME NULL,
    voided_by TEXT NULL,
    CONSTRAINT invoice_payments_company_id_id_unique UNIQUE (company_id, id),
    CONSTRAINT invoice_payments_one_source CHECK ((method = 'offset' AND customer_payment_id IS NULL AND supplier_receipt_id IS NULL) OR (method <> 'offset' AND (customer_payment_id IS NULL) <> (supplier_receipt_id IS NULL))),
    CONSTRAINT invoice_payments_void_is_complete CHECK ((voided_at IS NULL) = (voided_by IS NULL)),
    FOREIGN KEY (company_id, invoice_id) REFERENCES invoices (company_id, id) ON DELETE CASCADE,
    FOREIGN KEY (company_id, customer_payment_id) REFERENCES customer_payments (company_id, id),
    FOREIGN KEY (company_id, supplier_receipt_id) REFERENCES supplier_receipts (company_id, id),
    FOREIGN KEY (company_id, created_by) REFERENCES users (company_id, id),
    FOREIGN KEY (company_id, voided_by) REFERENCES users (company_id, id)
);

CREATE INDEX idx_invoice_payments_company_id_invoice_id ON invoice_payments (company_id, invoice_id);

CREATE TABLE invoice_counters (
    company_id TEXT NOT NULL REFERENCES companies (id),
    prefix TEXT NOT NULL CHECK (prefix IN ('INV', 'PRO', 'QUO', 'DN', 'SINV')),
    year INTEGER NOT NULL CHECK (year BETWEEN 2000 AND 9999),
    last_number BIGINT NOT NULL DEFAULT 0 CHECK (last_number >= 0),
    PRIMARY KEY (company_id, prefix, year)
);

ALTER TABLE company_features ADD COLUMN invoices_enabled BOOLEAN NOT NULL DEFAULT FALSE;

WITH actions (action) AS (
    VALUES ('view'), ('create'), ('edit'), ('delete')
)
INSERT INTO permissions (id, resource, action, description)
SELECT
    'invoices:' || action,
    'invoices',
    action,
    upper(substr(action, 1, 1)) || substr(action, 2) || ' invoices'
FROM actions;
