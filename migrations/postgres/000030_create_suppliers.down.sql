DROP INDEX idx_products_company_id_preferred_supplier_id;
ALTER TABLE products DROP CONSTRAINT products_preferred_supplier_fk;
ALTER TABLE products DROP COLUMN preferred_supplier_id;
DROP TABLE supplier_document_counters;
DROP TABLE suppliers;
