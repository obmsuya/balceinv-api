ALTER TABLE sale_items ADD COLUMN manual_discount_amount BIGINT NOT NULL DEFAULT 0 CHECK (manual_discount_amount >= 0 AND manual_discount_amount <= discount_amount);
ALTER TABLE settings ADD COLUMN till_discount_limit_basis_points INTEGER NOT NULL DEFAULT 10000 CHECK (till_discount_limit_basis_points BETWEEN 0 AND 10000);
INSERT INTO permissions (id, resource, action, description) VALUES ('till_discounts:create', 'till_discounts', 'create', 'Give discounts at the till');
