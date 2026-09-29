ALTER TABLE settings ALTER COLUMN receipt_number_format SET DEFAULT '{SHOP}-{DATE}-{COUNTER}';
UPDATE settings SET receipt_number_format = '{SHOP}-{DATE}-{COUNTER}' WHERE receipt_number_format = 'SALE-{DATE}-{COUNTER}';
