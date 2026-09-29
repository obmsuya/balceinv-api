DROP POLICY tenant_isolation ON fiscal_receipts;
ALTER TABLE fiscal_receipts DISABLE ROW LEVEL SECURITY;
