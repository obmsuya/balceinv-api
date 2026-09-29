DROP POLICY tenant_isolation ON company_features;
ALTER TABLE company_features DISABLE ROW LEVEL SECURITY;
