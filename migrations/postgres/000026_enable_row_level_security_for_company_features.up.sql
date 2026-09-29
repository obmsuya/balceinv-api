ALTER TABLE company_features ENABLE ROW LEVEL SECURITY;
ALTER TABLE company_features FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON company_features
    USING (company_id = nullif(current_setting('app.company_id', true), '')::uuid)
    WITH CHECK (company_id = nullif(current_setting('app.company_id', true), '')::uuid);
