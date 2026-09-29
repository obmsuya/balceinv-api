package rates

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
)

type Snapshot struct {
	BaseCurrency      string
	RatesJson         string
	ProviderUpdatedAt *time.Time
	FetchedAt         time.Time
}

type Repository struct{}

func NewRepository() *Repository {
	return &Repository{}
}

func (repository *Repository) FindSnapshot(ctx context.Context, querier database.Querier, baseCurrency string) (*Snapshot, error) {
	query := `
		SELECT base_currency, rates, provider_updated_at, fetched_at
		FROM exchange_rate_snapshots
		WHERE base_currency = $1
	`

	foundSnapshot := Snapshot{}
	scanError := querier.QueryRowContext(ctx, query, baseCurrency).Scan(
		&foundSnapshot.BaseCurrency,
		&foundSnapshot.RatesJson,
		&foundSnapshot.ProviderUpdatedAt,
		&foundSnapshot.FetchedAt,
	)
	if errors.Is(scanError, sql.ErrNoRows) {
		return nil, nil
	}
	if scanError != nil {
		return nil, fmt.Errorf("failed to find exchange rates: %w", scanError)
	}
	return &foundSnapshot, nil
}

func (repository *Repository) SaveSnapshot(ctx context.Context, querier database.Querier, newSnapshot Snapshot) error {
	query := `
		INSERT INTO exchange_rate_snapshots (base_currency, rates, provider_updated_at, fetched_at)
		VALUES ($1, $2, $3, $4)
		ON CONFLICT (base_currency) DO UPDATE
		SET rates = excluded.rates, provider_updated_at = excluded.provider_updated_at, fetched_at = excluded.fetched_at
	`

	_, saveError := querier.ExecContext(ctx, query, newSnapshot.BaseCurrency, newSnapshot.RatesJson, newSnapshot.ProviderUpdatedAt, newSnapshot.FetchedAt)
	if saveError != nil {
		return fmt.Errorf("failed to save exchange rates: %w", saveError)
	}
	return nil
}
