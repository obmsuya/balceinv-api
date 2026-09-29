CREATE TABLE sale_payments_without_credit (
    company_id TEXT NOT NULL,
    sale_id TEXT NOT NULL,
    method TEXT NOT NULL CHECK (method IN ('cash', 'card', 'mobile')),
    amount BIGINT NOT NULL CHECK (amount > 0),
    PRIMARY KEY (sale_id, method),
    FOREIGN KEY (company_id, sale_id) REFERENCES sales (company_id, id) ON DELETE CASCADE
);

INSERT INTO sale_payments_without_credit (company_id, sale_id, method, amount)
SELECT company_id, sale_id, method, amount FROM sale_payments WHERE method <> 'credit';

DROP TABLE sale_payments;

ALTER TABLE sale_payments_without_credit RENAME TO sale_payments;

CREATE INDEX idx_sale_payments_company_id_method ON sale_payments (company_id, method);

DROP INDEX idx_sales_company_id_customer_id_created_at;

ALTER TABLE sales DROP COLUMN customer_id;
