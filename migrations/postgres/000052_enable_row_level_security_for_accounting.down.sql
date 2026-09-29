DROP POLICY tenant_isolation ON accounting_settings;
ALTER TABLE accounting_settings DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON accounts;
ALTER TABLE accounts DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON journal_entries;
ALTER TABLE journal_entries DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON journal_lines;
ALTER TABLE journal_lines DISABLE ROW LEVEL SECURITY;
