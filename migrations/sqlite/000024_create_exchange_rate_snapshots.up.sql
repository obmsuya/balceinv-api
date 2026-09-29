CREATE TABLE exchange_rate_snapshots (
    base_currency TEXT PRIMARY KEY CHECK (length(base_currency) = 3),
    rates TEXT NOT NULL,
    provider_updated_at DATETIME NULL,
    fetched_at DATETIME NOT NULL
);
