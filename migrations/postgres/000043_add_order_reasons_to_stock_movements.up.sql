ALTER TABLE stock_movements DROP CONSTRAINT stock_movements_reason_check;
ALTER TABLE stock_movements ADD CONSTRAINT stock_movements_reason_check CHECK (reason IN ('opening', 'sale', 'return', 'purchase', 'adjustment', 'damage', 'transfer_in', 'transfer_out', 'order_reserved', 'order_cancelled'));
