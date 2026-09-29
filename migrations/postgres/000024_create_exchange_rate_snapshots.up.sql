CREATE TABLE exchange_rate_snapshots (
    base_currency TEXT PRIMARY KEY CHECK (length(base_currency) = 3),
    rates TEXT NOT NULL,
    provider_updated_at TIMESTAMPTZ NULL,
    fetched_at TIMESTAMPTZ NOT NULL
);
