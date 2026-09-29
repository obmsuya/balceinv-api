CREATE TABLE company_features (
    company_id UUID PRIMARY KEY REFERENCES companies (id),
    suppliers_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    purchase_orders_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    customers_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    credit_sales_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    customer_orders_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    accounting_mode TEXT NOT NULL DEFAULT 'off' CHECK (accounting_mode IN ('off', 'simple', 'full')),
    vat_registered BOOLEAN NOT NULL DEFAULT FALSE,
    vat_number TEXT NULL,
    updated_by UUID NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
