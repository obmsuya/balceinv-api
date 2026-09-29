DROP POLICY tenant_isolation ON support_messages;
ALTER TABLE support_messages DISABLE ROW LEVEL SECURITY;
