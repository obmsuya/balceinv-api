DROP TABLE fiscal_credit_notes;
DROP INDEX idx_sales_company_id_voided_at;
ALTER TABLE sales DROP COLUMN void_reason;
ALTER TABLE sales DROP COLUMN voided_by;
ALTER TABLE sales DROP COLUMN voided_at;
