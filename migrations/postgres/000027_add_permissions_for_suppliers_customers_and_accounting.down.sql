DELETE FROM user_permissions WHERE permission_id IN (SELECT id FROM permissions WHERE resource IN ('suppliers', 'purchases', 'customers', 'orders', 'accounting'));
DELETE FROM role_permissions WHERE permission_id IN (SELECT id FROM permissions WHERE resource IN ('suppliers', 'purchases', 'customers', 'orders', 'accounting'));
DELETE FROM permissions WHERE resource IN ('suppliers', 'purchases', 'customers', 'orders', 'accounting');
