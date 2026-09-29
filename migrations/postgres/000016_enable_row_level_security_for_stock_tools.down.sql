DROP POLICY tenant_isolation ON notifications;
ALTER TABLE notifications DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON stock_transfer_items;
ALTER TABLE stock_transfer_items DISABLE ROW LEVEL SECURITY;
DROP POLICY tenant_isolation ON stock_transfers;
ALTER TABLE stock_transfers DISABLE ROW LEVEL SECURITY;
