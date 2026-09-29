ALTER TABLE sales ADD COLUMN customer_id TEXT NULL REFERENCES customers (id);

CREATE INDEX idx_sales_company_id_customer_id_created_at ON sales (company_id, customer_id, created_at);

CREATE TABLE sale_payments_with_credit (
    company_id TEXT NOT NULL,
    sale_id TEXT NOT NULL,
    method TEXT NOT NULL CHECK (method IN ('cash', 'card', 'mobile', 'credit')),
    amount BIGINT NOT NULL CHECK (amount > 0),
    PRIMARY KEY (sale_id, method),
    FOREIGN KEY (company_id, sale_id) REFERENCES sales (company_id, id) ON DELETE CASCADE
);

INSERT INTO sale_payments_with_credit (company_id, sale_id, method, amount)
SELECT company_id, sale_id, method, amount FROM sale_payments;

DROP TABLE sale_payments;

ALTER TABLE sale_payments_with_credit RENAME TO sale_payments;

CREATE INDEX idx_sale_payments_company_id_method ON sale_payments (company_id, method);
