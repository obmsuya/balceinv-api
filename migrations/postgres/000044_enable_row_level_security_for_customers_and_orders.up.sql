ALTER TABLE customers ENABLE ROW LEVEL SECURITY;
ALTER TABLE customers FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON customers
    USING (company_id = nullif(current_setting('app.company_id', true), '')::uuid)
    WITH CHECK (company_id = nullif(current_setting('app.company_id', true), '')::uuid);

ALTER TABLE customer_payments ENABLE ROW LEVEL SECURITY;
ALTER TABLE customer_payments FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON customer_payments
    USING (company_id = nullif(current_setting('app.company_id', true), '')::uuid)
    WITH CHECK (company_id = nullif(current_setting('app.company_id', true), '')::uuid);

ALTER TABLE customer_orders ENABLE ROW LEVEL SECURITY;
ALTER TABLE customer_orders FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON customer_orders
    USING (company_id = nullif(current_setting('app.company_id', true), '')::uuid)
    WITH CHECK (company_id = nullif(current_setting('app.company_id', true), '')::uuid);

ALTER TABLE customer_order_lines ENABLE ROW LEVEL SECURITY;
ALTER TABLE customer_order_lines FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON customer_order_lines
    USING (company_id = nullif(current_setting('app.company_id', true), '')::uuid)
    WITH CHECK (company_id = nullif(current_setting('app.company_id', true), '')::uuid);

ALTER TABLE customer_order_payments ENABLE ROW LEVEL SECURITY;
ALTER TABLE customer_order_payments FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON customer_order_payments
    USING (company_id = nullif(current_setting('app.company_id', true), '')::uuid)
    WITH CHECK (company_id = nullif(current_setting('app.company_id', true), '')::uuid);
