ALTER TABLE invoice_lines DROP CONSTRAINT invoice_lines_winga_share_within_line;
ALTER TABLE invoice_lines DROP COLUMN winga_share;
ALTER TABLE invoice_lines DROP CONSTRAINT invoice_lines_winga_has_no_product;
ALTER TABLE invoice_lines DROP COLUMN is_winga;
ALTER TABLE invoice_lines DROP CONSTRAINT invoice_lines_extra_within_price;
ALTER TABLE invoice_lines DROP COLUMN extra_unit_amount;
