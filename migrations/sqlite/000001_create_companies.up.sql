CREATE TABLE companies (
    id TEXT PRIMARY KEY,
    name TEXT NOT NULL CHECK (length(trim(name)) > 0),
    business_type TEXT NOT NULL DEFAULT 'general',
    phone TEXT NULL,
    address TEXT NULL,
    tin TEXT NULL,
    logo_key TEXT NULL,
    primary_color TEXT NOT NULL DEFAULT '#5ea500' CHECK (length(primary_color) = 7 AND substr(primary_color, 1, 1) = '#'),
    currency_code TEXT NOT NULL DEFAULT 'TZS' CHECK (length(currency_code) = 3),
    currency_decimals INTEGER NOT NULL DEFAULT 0 CHECK (currency_decimals IN (0, 2)),
    timezone TEXT NOT NULL DEFAULT 'Africa/Dar_es_Salaam',
    default_locale TEXT NOT NULL DEFAULT 'en' CHECK (default_locale IN ('en', 'sw')),
    receipt_header TEXT NULL,
    receipt_footer TEXT NULL,
    created_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP,
    updated_at DATETIME NOT NULL DEFAULT CURRENT_TIMESTAMP
);
