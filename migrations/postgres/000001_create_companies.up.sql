CREATE TABLE companies (
    id UUID PRIMARY KEY,
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    business_type TEXT NOT NULL DEFAULT 'general',
    phone TEXT NULL,
    address TEXT NULL,
    tin TEXT NULL,
    logo_key TEXT NULL,
    primary_color TEXT NOT NULL DEFAULT '#5ea500' CHECK (primary_color ~ '^#[0-9a-fA-F]{6}$'),
    currency_code TEXT NOT NULL DEFAULT 'TZS' CHECK (length(currency_code) = 3),
    currency_decimals SMALLINT NOT NULL DEFAULT 0 CHECK (currency_decimals IN (0, 2)),
    timezone TEXT NOT NULL DEFAULT 'Africa/Dar_es_Salaam',
    default_locale TEXT NOT NULL DEFAULT 'en' CHECK (default_locale IN ('en', 'sw')),
    receipt_header TEXT NULL,
    receipt_footer TEXT NULL,
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);
