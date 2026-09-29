ALTER TABLE discounts ENABLE ROW LEVEL SECURITY;
ALTER TABLE discounts FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON discounts
    USING (company_id = nullif(current_setting('app.company_id', true), '')::uuid)
    WITH CHECK (company_id = nullif(current_setting('app.company_id', true), '')::uuid);

ALTER TABLE sales ENABLE ROW LEVEL SECURITY;
ALTER TABLE sales FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON sales
    USING (company_id = nullif(current_setting('app.company_id', true), '')::uuid)
    WITH CHECK (company_id = nullif(current_setting('app.company_id', true), '')::uuid);

ALTER TABLE sale_items ENABLE ROW LEVEL SECURITY;
ALTER TABLE sale_items FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON sale_items
    USING (company_id = nullif(current_setting('app.company_id', true), '')::uuid)
    WITH CHECK (company_id = nullif(current_setting('app.company_id', true), '')::uuid);

ALTER TABLE sale_item_addons ENABLE ROW LEVEL SECURITY;
ALTER TABLE sale_item_addons FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON sale_item_addons
    USING (company_id = nullif(current_setting('app.company_id', true), '')::uuid)
    WITH CHECK (company_id = nullif(current_setting('app.company_id', true), '')::uuid);

ALTER TABLE sale_payments ENABLE ROW LEVEL SECURITY;
ALTER TABLE sale_payments FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON sale_payments
    USING (company_id = nullif(current_setting('app.company_id', true), '')::uuid)
    WITH CHECK (company_id = nullif(current_setting('app.company_id', true), '')::uuid);
