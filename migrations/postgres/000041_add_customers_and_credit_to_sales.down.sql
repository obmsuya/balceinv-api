DELETE FROM sale_payments WHERE method = 'credit';
ALTER TABLE sale_payments DROP CONSTRAINT sale_payments_method_check;
ALTER TABLE sale_payments ADD CONSTRAINT sale_payments_method_check CHECK (method IN ('cash', 'card', 'mobile'));

DROP INDEX idx_sales_company_id_customer_id_created_at;
ALTER TABLE sales DROP CONSTRAINT sales_company_id_customer_id_fkey;
ALTER TABLE sales DROP COLUMN customer_id;
