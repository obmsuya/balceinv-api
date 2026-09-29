ALTER TABLE support_messages ENABLE ROW LEVEL SECURITY;
ALTER TABLE support_messages FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON support_messages
    USING (
        company_id = nullif(current_setting('app.company_id', true), '')::uuid
        OR current_setting('app.support_outbox', true) = 'on'
    )
    WITH CHECK (company_id = nullif(current_setting('app.company_id', true), '')::uuid);
