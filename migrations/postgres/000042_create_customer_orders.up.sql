CREATE TABLE customer_orders (
    id UUID PRIMARY KEY,
    company_id UUID NOT NULL REFERENCES companies (id),
    shop_id UUID NOT NULL,
    customer_id UUID NOT NULL,
    number INTEGER NOT NULL CHECK (number > 0),
    status TEXT NOT NULL DEFAULT 'open' CHECK (status IN ('open', 'ready', 'collected', 'cancelled')),
    due_date TEXT NULL CHECK (due_date IS NULL OR length(due_date) = 10),
    note TEXT NULL,
    subtotal BIGINT NOT NULL CHECK (subtotal >= 0),
    discount_total BIGINT NOT NULL DEFAULT 0 CHECK (discount_total >= 0),
    total BIGINT NOT NULL CHECK (total >= 0),
    tax_total BIGINT NOT NULL DEFAULT 0 CHECK (tax_total >= 0),
    tax_rate_basis_points INTEGER NOT NULL CHECK (tax_rate_basis_points BETWEEN 0 AND 10000),
    sale_id UUID NULL,
    created_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    ready_at TIMESTAMPTZ NULL,
    collected_at TIMESTAMPTZ NULL,
    cancelled_at TIMESTAMPTZ NULL,
    cancelled_by UUID NULL,
    cancel_reason TEXT NULL,
    CONSTRAINT customer_orders_company_id_id_unique UNIQUE (company_id, id),
    CONSTRAINT customer_orders_company_id_number_unique UNIQUE (company_id, number),
    CONSTRAINT customer_orders_total_is_subtotal_less_discount CHECK (total = subtotal - discount_total),
    CONSTRAINT customer_orders_collected_has_sale CHECK ((status = 'collected') = (sale_id IS NOT NULL)),
    CONSTRAINT customer_orders_cancel_is_complete CHECK ((status = 'cancelled') = (cancelled_at IS NOT NULL AND cancelled_by IS NOT NULL AND cancel_reason IS NOT NULL)),
    FOREIGN KEY (company_id, shop_id) REFERENCES shops (company_id, id),
    FOREIGN KEY (company_id, customer_id) REFERENCES customers (company_id, id),
    FOREIGN KEY (company_id, sale_id) REFERENCES sales (company_id, id),
    FOREIGN KEY (company_id, created_by) REFERENCES users (company_id, id),
    FOREIGN KEY (company_id, cancelled_by) REFERENCES users (company_id, id)
);

CREATE INDEX idx_customer_orders_company_id_shop_id_status_created_at ON customer_orders (company_id, shop_id, status, created_at);
CREATE INDEX idx_customer_orders_company_id_customer_id ON customer_orders (company_id, customer_id);

CREATE TABLE customer_order_lines (
    id UUID PRIMARY KEY,
    company_id UUID NOT NULL,
    order_id UUID NOT NULL,
    position INTEGER NOT NULL CHECK (position >= 0),
    product_id UUID NOT NULL,
    product_name TEXT NOT NULL,
    variant_label TEXT NOT NULL DEFAULT '',
    sku TEXT NOT NULL,
    unit TEXT NOT NULL,
    quantity INTEGER NOT NULL CHECK (quantity > 0),
    unit_price BIGINT NOT NULL CHECK (unit_price >= 0),
    unit_cost BIGINT NOT NULL CHECK (unit_cost >= 0),
    is_wholesale BOOLEAN NOT NULL DEFAULT FALSE,
    discount_id UUID NULL,
    discount_name TEXT NULL,
    discount_amount BIGINT NOT NULL DEFAULT 0 CHECK (discount_amount >= 0),
    line_total BIGINT NOT NULL CHECK (line_total >= 0),
    CONSTRAINT customer_order_lines_company_id_id_unique UNIQUE (company_id, id),
    CONSTRAINT customer_order_lines_order_id_position_unique UNIQUE (order_id, position),
    CONSTRAINT customer_order_lines_line_total_adds_up CHECK (line_total = quantity * unit_price - discount_amount),
    FOREIGN KEY (company_id, order_id) REFERENCES customer_orders (company_id, id) ON DELETE CASCADE,
    FOREIGN KEY (company_id, product_id) REFERENCES products (company_id, id),
    FOREIGN KEY (company_id, discount_id) REFERENCES discounts (company_id, id)
);

CREATE INDEX idx_customer_order_lines_company_id_order_id ON customer_order_lines (company_id, order_id);
CREATE INDEX idx_customer_order_lines_company_id_product_id ON customer_order_lines (company_id, product_id);

CREATE TABLE customer_order_payments (
    id UUID PRIMARY KEY,
    company_id UUID NOT NULL,
    order_id UUID NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('deposit', 'refund')),
    method TEXT NOT NULL CHECK (method IN ('cash', 'card', 'mobile')),
    amount BIGINT NOT NULL CHECK (amount > 0),
    created_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT customer_order_payments_company_id_id_unique UNIQUE (company_id, id),
    FOREIGN KEY (company_id, order_id) REFERENCES customer_orders (company_id, id) ON DELETE CASCADE,
    FOREIGN KEY (company_id, created_by) REFERENCES users (company_id, id)
);

CREATE INDEX idx_customer_order_payments_company_id_order_id ON customer_order_payments (company_id, order_id);
