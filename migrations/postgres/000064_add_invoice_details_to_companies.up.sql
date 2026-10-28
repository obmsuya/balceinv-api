ALTER TABLE companies ADD COLUMN email TEXT NULL;
ALTER TABLE companies ADD COLUMN invoice_payment_details TEXT NULL;
ALTER TABLE companies ADD COLUMN invoice_terms TEXT NULL;
ALTER TABLE companies ADD COLUMN invoice_due_days INTEGER NOT NULL DEFAULT 30 CHECK (invoice_due_days BETWEEN 0 AND 365);
