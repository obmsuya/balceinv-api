DELETE FROM user_permissions WHERE permission_id IN (SELECT id FROM permissions WHERE resource = 'invoices');
DELETE FROM role_permissions WHERE permission_id IN (SELECT id FROM permissions WHERE resource = 'invoices');
DELETE FROM permissions WHERE resource = 'invoices';
ALTER TABLE company_features DROP COLUMN invoices_enabled;
DROP TABLE invoice_counters;
DROP TABLE invoice_payments;
DROP TABLE invoice_lines;
DROP TABLE invoices;
DROP TABLE supplier_receipts;
