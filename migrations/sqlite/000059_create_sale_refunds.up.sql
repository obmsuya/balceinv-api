CREATE TABLE sale_refunds (
    id TEXT PRIMARY KEY,
    company_id TEXT NOT NULL REFERENCES companies (id),
    sale_id TEXT NOT NULL,
    client_ref TEXT NOT NULL,
    method TEXT NOT NULL CHECK (method IN ('cash', 'card', 'mobile', 'credit')),
    amount BIGINT NOT NULL CHECK (amount > 0),
    tax_amount BIGINT NOT NULL DEFAULT 0 CHECK (tax_amount >= 0 AND tax_amount <= amount),
    cost_amount BIGINT NOT NULL DEFAULT 0 CHECK (cost_amount >= 0),
    restocked BOOLEAN NOT NULL DEFAULT FALSE,
    reason TEXT NOT NULL,
    created_by TEXT NOT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    CONSTRAINT sale_refunds_company_id_id_unique UNIQUE (company_id, id),
    CONSTRAINT sale_refunds_company_id_client_ref_unique UNIQUE (company_id, client_ref),
    FOREIGN KEY (company_id, sale_id) REFERENCES sales (company_id, id),
    FOREIGN KEY (company_id, created_by) REFERENCES users (company_id, id)
);

CREATE INDEX idx_sale_refunds_company_id_sale_id ON sale_refunds (company_id, sale_id);
CREATE INDEX idx_sale_refunds_company_id_created_at ON sale_refunds (company_id, created_at);
CREATE INDEX idx_sale_refunds_company_id_created_by ON sale_refunds (company_id, created_by);

CREATE TABLE sale_refund_lines (
    company_id TEXT NOT NULL,
    refund_id TEXT NOT NULL,
    sale_item_id TEXT NOT NULL,
    quantity INTEGER NOT NULL CHECK (quantity > 0),
    amount BIGINT NOT NULL CHECK (amount >= 0),
    cost_amount BIGINT NOT NULL DEFAULT 0 CHECK (cost_amount >= 0),
    PRIMARY KEY (company_id, refund_id, sale_item_id),
    FOREIGN KEY (company_id, refund_id) REFERENCES sale_refunds (company_id, id) ON DELETE CASCADE,
    FOREIGN KEY (company_id, sale_item_id) REFERENCES sale_items (company_id, id)
);

CREATE INDEX idx_sale_refund_lines_company_id_sale_item_id ON sale_refund_lines (company_id, sale_item_id);

CREATE TABLE fiscal_refund_notes (
    company_id TEXT NOT NULL REFERENCES companies (id),
    refund_id TEXT NOT NULL,
    status TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'sending', 'sent', 'failed')),
    attempts INTEGER NOT NULL DEFAULT 0 CHECK (attempts >= 0),
    verification_code TEXT NULL,
    verification_url TEXT NULL,
    last_error TEXT NULL,
    sent_at DATETIME NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    PRIMARY KEY (company_id, refund_id),
    FOREIGN KEY (company_id, refund_id) REFERENCES sale_refunds (company_id, id) ON DELETE CASCADE
);

CREATE INDEX idx_fiscal_refund_notes_company_id_status_created_at ON fiscal_refund_notes (company_id, status, created_at);
