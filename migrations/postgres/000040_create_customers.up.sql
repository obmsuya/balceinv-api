CREATE TABLE customers (
    id UUID PRIMARY KEY,
    company_id UUID NOT NULL REFERENCES companies (id),
    name TEXT NOT NULL CHECK (length(trim(name)) BETWEEN 1 AND 120),
    phone TEXT NULL CHECK (phone IS NULL OR length(phone) = 10),
    email TEXT NULL,
    address TEXT NULL,
    tin TEXT NULL,
    credit_limit BIGINT NULL CHECK (credit_limit IS NULL OR credit_limit >= 0),
    opening_balance BIGINT NOT NULL DEFAULT 0 CHECK (opening_balance >= 0),
    notes TEXT NULL,
    is_active BOOLEAN NOT NULL DEFAULT TRUE,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    CONSTRAINT customers_company_id_id_unique UNIQUE (company_id, id)
);

CREATE UNIQUE INDEX customers_company_id_active_phone_unique ON customers (company_id, phone) WHERE is_active AND phone IS NOT NULL;
CREATE INDEX idx_customers_company_id_name ON customers (company_id, name);

CREATE TABLE customer_payments (
    id UUID PRIMARY KEY,
    company_id UUID NOT NULL,
    customer_id UUID NOT NULL,
    shop_id UUID NULL,
    amount BIGINT NOT NULL CHECK (amount > 0),
    method TEXT NOT NULL CHECK (method IN ('cash', 'card', 'mobile')),
    reference TEXT NULL,
    received_at TIMESTAMPTZ NOT NULL,
    created_by UUID NOT NULL,
    voided_at TIMESTAMPTZ NULL,
    voided_by UUID NULL,
    void_reason TEXT NULL,
    CONSTRAINT customer_payments_company_id_id_unique UNIQUE (company_id, id),
    CONSTRAINT customer_payments_void_is_complete CHECK ((voided_at IS NULL AND voided_by IS NULL AND void_reason IS NULL) OR (voided_at IS NOT NULL AND voided_by IS NOT NULL AND void_reason IS NOT NULL)),
    FOREIGN KEY (company_id, customer_id) REFERENCES customers (company_id, id),
    FOREIGN KEY (company_id, shop_id) REFERENCES shops (company_id, id),
    FOREIGN KEY (company_id, created_by) REFERENCES users (company_id, id),
    FOREIGN KEY (company_id, voided_by) REFERENCES users (company_id, id)
);

CREATE INDEX idx_customer_payments_company_id_customer_id_received_at ON customer_payments (company_id, customer_id, received_at);
