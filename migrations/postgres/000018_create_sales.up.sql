CREATE TABLE sales (
    id UUID PRIMARY KEY,
    company_id UUID NOT NULL,
    shop_id UUID NOT NULL,
    user_id UUID NOT NULL,
    client_ref TEXT NOT NULL CHECK (length(client_ref) BETWEEN 8 AND 64),
    request_hash TEXT NOT NULL,
    receipt_number TEXT NOT NULL,
    subtotal BIGINT NOT NULL CHECK (subtotal >= 0),
    discount_total BIGINT NOT NULL DEFAULT 0 CHECK (discount_total >= 0),
    total BIGINT NOT NULL CHECK (total >= 0),
    tax_total BIGINT NOT NULL DEFAULT 0 CHECK (tax_total >= 0),
    tax_rate_basis_points INTEGER NOT NULL CHECK (tax_rate_basis_points BETWEEN 0 AND 10000),
    amount_paid BIGINT NOT NULL CHECK (amount_paid >= 0),
    change_given BIGINT NOT NULL DEFAULT 0 CHECK (change_given >= 0),
    currency_code TEXT NOT NULL CHECK (length(currency_code) = 3),
    currency_decimals SMALLINT NOT NULL CHECK (currency_decimals IN (0, 2)),
    note TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT sales_company_id_id_unique UNIQUE (company_id, id),
    CONSTRAINT sales_company_id_client_ref_unique UNIQUE (company_id, client_ref),
    CONSTRAINT sales_company_id_shop_id_receipt_number_unique UNIQUE (company_id, shop_id, receipt_number),
    CONSTRAINT sales_total_is_subtotal_less_discount CHECK (total = subtotal - discount_total),
    CONSTRAINT sales_paid_covers_total CHECK (amount_paid = total + change_given),
    FOREIGN KEY (company_id, shop_id) REFERENCES shops (company_id, id),
    FOREIGN KEY (company_id, user_id) REFERENCES users (company_id, id)
);

CREATE INDEX idx_sales_company_id_shop_id_created_at ON sales (company_id, shop_id, created_at);
CREATE INDEX idx_sales_company_id_created_at ON sales (company_id, created_at);
CREATE INDEX idx_sales_company_id_user_id ON sales (company_id, user_id);

CREATE TABLE sale_items (
    id UUID PRIMARY KEY,
    company_id UUID NOT NULL,
    sale_id UUID NOT NULL,
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
    addons_unit_total BIGINT NOT NULL DEFAULT 0 CHECK (addons_unit_total >= 0),
    discount_id UUID NULL,
    discount_name TEXT NULL,
    discount_amount BIGINT NOT NULL DEFAULT 0 CHECK (discount_amount >= 0),
    line_total BIGINT NOT NULL CHECK (line_total >= 0),
    CONSTRAINT sale_items_company_id_id_unique UNIQUE (company_id, id),
    CONSTRAINT sale_items_sale_id_position_unique UNIQUE (sale_id, position),
    CONSTRAINT sale_items_line_total_adds_up CHECK (line_total = quantity * (unit_price + addons_unit_total) - discount_amount),
    FOREIGN KEY (company_id, sale_id) REFERENCES sales (company_id, id) ON DELETE CASCADE,
    FOREIGN KEY (company_id, product_id) REFERENCES products (company_id, id),
    FOREIGN KEY (company_id, discount_id) REFERENCES discounts (company_id, id)
);

CREATE INDEX idx_sale_items_company_id_sale_id ON sale_items (company_id, sale_id);
CREATE INDEX idx_sale_items_company_id_product_id ON sale_items (company_id, product_id);
CREATE INDEX idx_sale_items_company_id_discount_id ON sale_items (company_id, discount_id);

CREATE TABLE sale_item_addons (
    company_id UUID NOT NULL,
    sale_item_id UUID NOT NULL,
    addon_id UUID NOT NULL,
    name TEXT NOT NULL,
    unit_price BIGINT NOT NULL CHECK (unit_price >= 0),
    PRIMARY KEY (sale_item_id, addon_id),
    FOREIGN KEY (company_id, sale_item_id) REFERENCES sale_items (company_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_sale_item_addons_company_id_addon_id ON sale_item_addons (company_id, addon_id);

CREATE TABLE sale_payments (
    company_id UUID NOT NULL,
    sale_id UUID NOT NULL,
    method TEXT NOT NULL CHECK (method IN ('cash', 'card', 'mobile')),
    amount BIGINT NOT NULL CHECK (amount > 0),
    PRIMARY KEY (sale_id, method),
    FOREIGN KEY (company_id, sale_id) REFERENCES sales (company_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_sale_payments_company_id_method ON sale_payments (company_id, method);
