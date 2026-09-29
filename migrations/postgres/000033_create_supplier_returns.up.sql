CREATE TABLE supplier_returns (
    id UUID PRIMARY KEY,
    company_id UUID NOT NULL REFERENCES companies (id),
    return_number TEXT NOT NULL,
    supplier_id UUID NOT NULL,
    shop_id UUID NOT NULL,
    total BIGINT NOT NULL CHECK (total >= 0),
    note TEXT NULL,
    returned_at TIMESTAMPTZ NOT NULL,
    created_by UUID NOT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT supplier_returns_company_id_id_unique UNIQUE (company_id, id),
    CONSTRAINT supplier_returns_company_id_return_number_unique UNIQUE (company_id, return_number),
    FOREIGN KEY (company_id, supplier_id) REFERENCES suppliers (company_id, id),
    FOREIGN KEY (company_id, shop_id) REFERENCES shops (company_id, id),
    FOREIGN KEY (company_id, created_by) REFERENCES users (company_id, id)
);

CREATE INDEX idx_supplier_returns_company_id_supplier_id_returned_at ON supplier_returns (company_id, supplier_id, returned_at);

CREATE TABLE supplier_return_lines (
    id UUID PRIMARY KEY,
    company_id UUID NOT NULL,
    return_id UUID NOT NULL,
    position INTEGER NOT NULL CHECK (position >= 0),
    product_id UUID NOT NULL,
    quantity INTEGER NOT NULL CHECK (quantity > 0),
    unit_cost BIGINT NOT NULL CHECK (unit_cost >= 0),
    line_total BIGINT NOT NULL CHECK (line_total >= 0),
    CONSTRAINT supplier_return_lines_return_product_unique UNIQUE (return_id, product_id),
    CONSTRAINT supplier_return_lines_total_adds_up CHECK (line_total = quantity * unit_cost),
    FOREIGN KEY (company_id, return_id) REFERENCES supplier_returns (company_id, id) ON DELETE CASCADE,
    FOREIGN KEY (company_id, product_id) REFERENCES products (company_id, id)
);

CREATE INDEX idx_supplier_return_lines_company_id_return_id ON supplier_return_lines (company_id, return_id);
CREATE INDEX idx_supplier_return_lines_company_id_product_id ON supplier_return_lines (company_id, product_id);
