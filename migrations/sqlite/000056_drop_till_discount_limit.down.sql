ALTER TABLE settings ADD COLUMN till_discount_limit_basis_points INTEGER NOT NULL DEFAULT 10000 CHECK (till_discount_limit_basis_points BETWEEN 0 AND 10000);
