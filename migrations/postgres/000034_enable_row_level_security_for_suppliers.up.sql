ALTER TABLE suppliers ENABLE ROW LEVEL SECURITY;
ALTER TABLE suppliers FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON suppliers
    USING (company_id = nullif(current_setting('app.company_id', true), '')::uuid)
    WITH CHECK (company_id = nullif(current_setting('app.company_id', true), '')::uuid);

ALTER TABLE supplier_document_counters ENABLE ROW LEVEL SECURITY;
ALTER TABLE supplier_document_counters FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supplier_document_counters
    USING (company_id = nullif(current_setting('app.company_id', true), '')::uuid)
    WITH CHECK (company_id = nullif(current_setting('app.company_id', true), '')::uuid);

ALTER TABLE purchase_orders ENABLE ROW LEVEL SECURITY;
ALTER TABLE purchase_orders FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON purchase_orders
    USING (company_id = nullif(current_setting('app.company_id', true), '')::uuid)
    WITH CHECK (company_id = nullif(current_setting('app.company_id', true), '')::uuid);

ALTER TABLE purchase_order_lines ENABLE ROW LEVEL SECURITY;
ALTER TABLE purchase_order_lines FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON purchase_order_lines
    USING (company_id = nullif(current_setting('app.company_id', true), '')::uuid)
    WITH CHECK (company_id = nullif(current_setting('app.company_id', true), '')::uuid);

ALTER TABLE purchases ENABLE ROW LEVEL SECURITY;
ALTER TABLE purchases FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON purchases
    USING (company_id = nullif(current_setting('app.company_id', true), '')::uuid)
    WITH CHECK (company_id = nullif(current_setting('app.company_id', true), '')::uuid);

ALTER TABLE purchase_lines ENABLE ROW LEVEL SECURITY;
ALTER TABLE purchase_lines FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON purchase_lines
    USING (company_id = nullif(current_setting('app.company_id', true), '')::uuid)
    WITH CHECK (company_id = nullif(current_setting('app.company_id', true), '')::uuid);

ALTER TABLE supplier_payments ENABLE ROW LEVEL SECURITY;
ALTER TABLE supplier_payments FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supplier_payments
    USING (company_id = nullif(current_setting('app.company_id', true), '')::uuid)
    WITH CHECK (company_id = nullif(current_setting('app.company_id', true), '')::uuid);

ALTER TABLE supplier_returns ENABLE ROW LEVEL SECURITY;
ALTER TABLE supplier_returns FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supplier_returns
    USING (company_id = nullif(current_setting('app.company_id', true), '')::uuid)
    WITH CHECK (company_id = nullif(current_setting('app.company_id', true), '')::uuid);

ALTER TABLE supplier_return_lines ENABLE ROW LEVEL SECURITY;
ALTER TABLE supplier_return_lines FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON supplier_return_lines
    USING (company_id = nullif(current_setting('app.company_id', true), '')::uuid)
    WITH CHECK (company_id = nullif(current_setting('app.company_id', true), '')::uuid);
