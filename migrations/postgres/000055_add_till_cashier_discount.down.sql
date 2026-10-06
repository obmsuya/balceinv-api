DELETE FROM user_permissions WHERE permission_id = 'till_discounts:create';
DELETE FROM role_permissions WHERE permission_id = 'till_discounts:create';
DELETE FROM permissions WHERE id = 'till_discounts:create';
ALTER TABLE settings DROP CONSTRAINT settings_till_discount_limit_range;
ALTER TABLE settings DROP COLUMN till_discount_limit_basis_points;
ALTER TABLE sale_items DROP CONSTRAINT sale_items_manual_discount_within_discount;
ALTER TABLE sale_items DROP COLUMN manual_discount_amount;
