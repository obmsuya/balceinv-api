ALTER TABLE sale_items ADD COLUMN extra_unit_amount BIGINT NOT NULL DEFAULT 0 CHECK (extra_unit_amount >= 0 AND extra_unit_amount <= unit_price);
ALTER TABLE customer_order_lines ADD COLUMN extra_unit_amount BIGINT NOT NULL DEFAULT 0 CHECK (extra_unit_amount >= 0 AND extra_unit_amount <= unit_price);
ALTER TABLE customer_order_lines ADD COLUMN manual_discount_amount BIGINT NOT NULL DEFAULT 0 CHECK (manual_discount_amount >= 0 AND manual_discount_amount <= discount_amount);
