DELETE FROM user_permissions WHERE permission_id = 'till_discounts:create';
DELETE FROM role_permissions WHERE permission_id = 'till_discounts:create';
DELETE FROM permissions WHERE id = 'till_discounts:create';
ALTER TABLE settings DROP COLUMN till_discount_limit_basis_points;
ALTER TABLE sale_items DROP COLUMN manual_discount_amount;
