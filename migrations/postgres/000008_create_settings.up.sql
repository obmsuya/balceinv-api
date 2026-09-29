CREATE TABLE settings (
    company_id UUID PRIMARY KEY REFERENCES companies (id),
    tax_rate_basis_points INTEGER NOT NULL DEFAULT 1800 CHECK (tax_rate_basis_points BETWEEN 0 AND 10000),
    date_format TEXT NOT NULL DEFAULT 'DD/MM/YYYY' CHECK (date_format IN ('DD/MM/YYYY', 'MM/DD/YYYY', 'YYYY-MM-DD')),
    receipt_number_format TEXT NOT NULL DEFAULT 'SALE-{DATE}-{COUNTER}',
    receipt_language TEXT NOT NULL DEFAULT 'en' CHECK (receipt_language IN ('en', 'sw')),
    efd_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    efd_endpoint TEXT NULL,
    efd_api_key TEXT NULL,
    low_stock_threshold INTEGER NOT NULL DEFAULT 5 CHECK (low_stock_threshold >= 0),
    email_notifications_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    notification_email TEXT NULL,
    alert_sound_enabled BOOLEAN NOT NULL DEFAULT TRUE,
    alert_on_low_stock BOOLEAN NOT NULL DEFAULT TRUE,
    alert_on_out_of_stock BOOLEAN NOT NULL DEFAULT TRUE,
    alert_on_dead_stock BOOLEAN NOT NULL DEFAULT FALSE,
    dead_stock_days INTEGER NOT NULL DEFAULT 30 CHECK (dead_stock_days BETWEEN 1 AND 3650),
    print_receipt_automatically BOOLEAN NOT NULL DEFAULT FALSE,
    show_tax_on_receipt BOOLEAN NOT NULL DEFAULT TRUE,
    show_barcodes_on_receipt BOOLEAN NOT NULL DEFAULT FALSE,
    printer_enabled BOOLEAN NOT NULL DEFAULT FALSE,
    printer_port TEXT NOT NULL DEFAULT '',
    printer_model TEXT NOT NULL DEFAULT '',
    printer_baud_rate INTEGER NOT NULL DEFAULT 9600 CHECK (printer_baud_rate > 0),
    printer_paper_width INTEGER NOT NULL DEFAULT 80 CHECK (printer_paper_width IN (58, 80)),
    open_cash_drawer BOOLEAN NOT NULL DEFAULT FALSE,
    updated_by UUID NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    FOREIGN KEY (company_id, updated_by) REFERENCES users (company_id, id)
);

ALTER TABLE settings ENABLE ROW LEVEL SECURITY;
ALTER TABLE settings FORCE ROW LEVEL SECURITY;
CREATE POLICY tenant_isolation ON settings
    USING (company_id = nullif(current_setting('app.company_id', true), '')::uuid)
    WITH CHECK (company_id = nullif(current_setting('app.company_id', true), '')::uuid);
