CREATE TABLE sale_winga_lines (
    id TEXT PRIMARY KEY,
    company_id TEXT NOT NULL,
    sale_id TEXT NOT NULL,
    position INTEGER NOT NULL CHECK (position >= 0),
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
    quantity INTEGER NOT NULL CHECK (quantity > 0),
    unit_price BIGINT NOT NULL CHECK (unit_price >= 0),
    line_total BIGINT NOT NULL CHECK (line_total >= 0),
    winga_share BIGINT NOT NULL DEFAULT 0 CHECK (winga_share >= 0),
    CONSTRAINT sale_winga_lines_company_id_id_unique UNIQUE (company_id, id),
    CONSTRAINT sale_winga_lines_sale_id_position_unique UNIQUE (sale_id, position),
    CONSTRAINT sale_winga_lines_line_total_adds_up CHECK (line_total = quantity * unit_price),
    CONSTRAINT sale_winga_lines_share_within_total CHECK (winga_share <= line_total),
    FOREIGN KEY (company_id, sale_id) REFERENCES sales (company_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_sale_winga_lines_company_id_sale_id ON sale_winga_lines (company_id, sale_id);

CREATE TABLE customer_order_winga_lines (
    id TEXT PRIMARY KEY,
    company_id TEXT NOT NULL,
    order_id TEXT NOT NULL,
    position INTEGER NOT NULL CHECK (position >= 0),
    name TEXT NOT NULL CHECK (length(name) BETWEEN 1 AND 120),
    quantity INTEGER NOT NULL CHECK (quantity > 0),
    unit_price BIGINT NOT NULL CHECK (unit_price >= 0),
    line_total BIGINT NOT NULL CHECK (line_total >= 0),
    winga_share BIGINT NOT NULL DEFAULT 0 CHECK (winga_share >= 0),
    CONSTRAINT customer_order_winga_lines_company_id_id_unique UNIQUE (company_id, id),
    CONSTRAINT customer_order_winga_lines_order_id_position_unique UNIQUE (order_id, position),
    CONSTRAINT customer_order_winga_lines_line_total_adds_up CHECK (line_total = quantity * unit_price),
    CONSTRAINT customer_order_winga_lines_share_within_total CHECK (winga_share <= line_total),
    FOREIGN KEY (company_id, order_id) REFERENCES customer_orders (company_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_customer_order_winga_lines_company_id_order_id ON customer_order_winga_lines (company_id, order_id);

ALTER TABLE company_features ADD COLUMN winga_enabled BOOLEAN NOT NULL DEFAULT FALSE;
