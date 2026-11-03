ALTER TABLE sale_items ADD COLUMN extra_unit_amount BIGINT NOT NULL DEFAULT 0;
ALTER TABLE sale_items ADD CONSTRAINT sale_items_extra_within_price CHECK (extra_unit_amount >= 0 AND extra_unit_amount <= unit_price);
ALTER TABLE customer_order_lines ADD COLUMN extra_unit_amount BIGINT NOT NULL DEFAULT 0;
ALTER TABLE customer_order_lines ADD CONSTRAINT customer_order_lines_extra_within_price CHECK (extra_unit_amount >= 0 AND extra_unit_amount <= unit_price);
ALTER TABLE customer_order_lines ADD COLUMN manual_discount_amount BIGINT NOT NULL DEFAULT 0;
ALTER TABLE customer_order_lines ADD CONSTRAINT customer_order_lines_manual_discount_within_discount CHECK (manual_discount_amount >= 0 AND manual_discount_amount <= discount_amount);
