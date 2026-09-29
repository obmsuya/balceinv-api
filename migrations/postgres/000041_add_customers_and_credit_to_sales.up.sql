ALTER TABLE sales ADD COLUMN customer_id UUID NULL;
ALTER TABLE sales ADD CONSTRAINT sales_company_id_customer_id_fkey FOREIGN KEY (company_id, customer_id) REFERENCES customers (company_id, id);

CREATE INDEX idx_sales_company_id_customer_id_created_at ON sales (company_id, customer_id, created_at);

ALTER TABLE sale_payments DROP CONSTRAINT sale_payments_method_check;
ALTER TABLE sale_payments ADD CONSTRAINT sale_payments_method_check CHECK (method IN ('cash', 'card', 'mobile', 'credit'));
