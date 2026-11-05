ALTER TABLE invoice_lines ADD COLUMN extra_unit_amount BIGINT NOT NULL DEFAULT 0 CHECK (extra_unit_amount >= 0 AND extra_unit_amount <= unit_price);
ALTER TABLE invoice_lines ADD COLUMN is_winga BOOLEAN NOT NULL DEFAULT FALSE CHECK (NOT is_winga OR product_id IS NULL);
ALTER TABLE invoice_lines ADD COLUMN winga_share BIGINT NOT NULL DEFAULT 0 CHECK (winga_share >= 0 AND winga_share <= line_total AND (is_winga OR winga_share = 0));
