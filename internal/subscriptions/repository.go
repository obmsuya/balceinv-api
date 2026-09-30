package subscriptions

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/license"
	"github.com/google/uuid"
)

type Repository struct{}

func NewRepository() *Repository {
	return &Repository{}
}

func (repository *Repository) Find(ctx context.Context, querier database.Querier, companyId uuid.UUID) (*Subscription, error) {
	query := `
		SELECT company_id, license_key, expires_at, days_granted, max_devices, is_trial
		FROM company_subscriptions
		WHERE company_id = $1
	`

	foundSubscription := Subscription{}
	scanError := querier.QueryRowContext(ctx, query, companyId).Scan(
		&foundSubscription.CompanyId,
		&foundSubscription.LicenseKey,
		&foundSubscription.ExpiresAt,
		&foundSubscription.DaysGranted,
		&foundSubscription.MaxDevices,
		&foundSubscription.IsTrial,
	)
	if scanError != nil {
		if errors.Is(scanError, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to read subscription: %w", scanError)
	}

	return &foundSubscription, nil
}

func (repository *Repository) SavePaid(ctx context.Context, querier database.Querier, paidSubscription Subscription) error {
	query := `
		UPDATE company_subscriptions
		SET license_key = $2, expires_at = $3, days_granted = $4, max_devices = $5, is_trial = FALSE, updated_at = $6
		WHERE company_id = $1
	`

	_, updateError := querier.ExecContext(ctx, query,
		paidSubscription.CompanyId,
		paidSubscription.LicenseKey,
		paidSubscription.ExpiresAt,
		paidSubscription.DaysGranted,
		paidSubscription.MaxDevices,
		time.Now().UTC(),
	)
	if updateError != nil {
		return fmt.Errorf("failed to save subscription: %w", updateError)
	}

	return nil
}

func (repository *Repository) InsertTrialIfMissing(ctx context.Context, querier database.Querier, companyId uuid.UUID, trialEndsAt time.Time, createdAt time.Time) error {
	query := `
		INSERT INTO company_subscriptions (company_id, license_key, expires_at, days_granted, max_devices, is_trial, created_at, updated_at)
		VALUES ($1, 'trial', $2, $3, 1, TRUE, $4, $4)
		ON CONFLICT (company_id) DO NOTHING
	`

	_, insertError := querier.ExecContext(ctx, query, companyId, trialEndsAt, license.TrialDurationDays, createdAt)
	if insertError != nil {
		return fmt.Errorf("failed to start trial subscription: %w", insertError)
	}

	return nil
}
