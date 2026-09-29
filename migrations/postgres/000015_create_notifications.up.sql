CREATE TABLE notifications (
    id UUID PRIMARY KEY,
    company_id UUID NOT NULL,
    shop_id UUID NOT NULL,
    product_id UUID NOT NULL,
    kind TEXT NOT NULL CHECK (kind IN ('low_stock', 'out_of_stock')),
    quantity INTEGER NOT NULL CHECK (quantity >= 0),
    min_stock INTEGER NOT NULL CHECK (min_stock >= 0),
    is_read BOOLEAN NOT NULL DEFAULT FALSE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    read_at TIMESTAMPTZ NULL,
    FOREIGN KEY (company_id, shop_id) REFERENCES shops (company_id, id),
    FOREIGN KEY (company_id, product_id) REFERENCES products (company_id, id)
);

CREATE INDEX idx_notifications_company_id_shop_id_created_at ON notifications (company_id, shop_id, created_at);
CREATE INDEX idx_notifications_company_id_product_id ON notifications (company_id, product_id);
