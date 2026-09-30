package auth

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/google/uuid"
)

type Repository struct{}

func NewRepository() *Repository {
	return &Repository{}
}

func (repository *Repository) FindLoginCandidate(ctx context.Context, querier database.Querier, email string) (*LoginCandidate, error) {
	query := `
		SELECT id, company_id, password_hash, is_active
		FROM users
		WHERE email = $1
	`

	foundCandidate := LoginCandidate{}
	scanError := querier.QueryRowContext(ctx, query, email).Scan(
		&foundCandidate.UserId,
		&foundCandidate.CompanyId,
		&foundCandidate.PasswordHash,
		&foundCandidate.IsActive,
	)
	if scanError != nil {
		if errors.Is(scanError, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to find login candidate: %w", scanError)
	}

	return &foundCandidate, nil
}

func (repository *Repository) InsertLoginAttempt(ctx context.Context, querier database.Querier, attempt LoginAttempt) error {
	query := `
		INSERT INTO login_attempts (id, email, ip_address, succeeded, created_at)
		VALUES ($1, $2, $3, $4, $5)
	`

	_, insertError := querier.ExecContext(ctx, query,
		attempt.Id,
		attempt.Email,
		attempt.IpAddress,
		attempt.Succeeded,
		attempt.CreatedAt,
	)
	if insertError != nil {
		return fmt.Errorf("failed to record login attempt: %w", insertError)
	}

	return nil
}

func (repository *Repository) InsertSession(ctx context.Context, querier database.Querier, newSession Session) error {
	query := `
		INSERT INTO sessions (id, token_hash, company_id, user_id, shop_id, ip_address, user_agent, created_at, last_seen_at, expires_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10)
	`

	_, insertError := querier.ExecContext(ctx, query,
		newSession.Id,
		newSession.TokenHash,
		newSession.CompanyId,
		newSession.UserId,
		newSession.ShopId,
		newSession.IpAddress,
		newSession.UserAgent,
		newSession.CreatedAt,
		newSession.LastSeenAt,
		newSession.ExpiresAt,
	)
	if insertError != nil {
		return fmt.Errorf("failed to insert session: %w", insertError)
	}

	return nil
}

func (repository *Repository) FindSessionByTokenHash(ctx context.Context, querier database.Querier, tokenHash string) (*Session, error) {
	query := `
		SELECT id, token_hash, company_id, user_id, shop_id, ip_address, user_agent, created_at, last_seen_at, expires_at
		FROM sessions
		WHERE token_hash = $1
	`

	foundSession := Session{}
	scanError := querier.QueryRowContext(ctx, query, tokenHash).Scan(
		&foundSession.Id,
		&foundSession.TokenHash,
		&foundSession.CompanyId,
		&foundSession.UserId,
		&foundSession.ShopId,
		&foundSession.IpAddress,
		&foundSession.UserAgent,
		&foundSession.CreatedAt,
		&foundSession.LastSeenAt,
		&foundSession.ExpiresAt,
	)
	if scanError != nil {
		if errors.Is(scanError, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to find session: %w", scanError)
	}

	return &foundSession, nil
}

func (repository *Repository) TouchSession(ctx context.Context, querier database.Querier, companyId uuid.UUID, sessionId uuid.UUID, seenAt time.Time) error {
	query := `UPDATE sessions SET last_seen_at = $3 WHERE company_id = $1 AND id = $2`

	_, updateError := querier.ExecContext(ctx, query, companyId, sessionId, seenAt)
	if updateError != nil {
		return fmt.Errorf("failed to touch session: %w", updateError)
	}

	return nil
}

func (repository *Repository) UpdateSessionShop(ctx context.Context, querier database.Querier, companyId uuid.UUID, sessionId uuid.UUID, shopId uuid.UUID) error {
	query := `UPDATE sessions SET shop_id = $3 WHERE company_id = $1 AND id = $2`

	_, updateError := querier.ExecContext(ctx, query, companyId, sessionId, shopId)
	if updateError != nil {
		return fmt.Errorf("failed to switch session shop: %w", updateError)
	}

	return nil
}

func (repository *Repository) UpdateUserLocale(ctx context.Context, querier database.Querier, companyId uuid.UUID, userId uuid.UUID, locale *string) error {
	query := `UPDATE users SET locale = $3, updated_at = $4 WHERE company_id = $1 AND id = $2`

	_, updateError := querier.ExecContext(ctx, query, companyId, userId, locale, time.Now().UTC())
	if updateError != nil {
		return fmt.Errorf("failed to save user language: %w", updateError)
	}

	return nil
}

func (repository *Repository) FindSeenTours(ctx context.Context, querier database.Querier, companyId uuid.UUID, userId uuid.UUID) ([]string, error) {
	query := `SELECT seen_tours FROM users WHERE company_id = $1 AND id = $2`

	seenToursText := ""
	scanError := querier.QueryRowContext(ctx, query, companyId, userId).Scan(&seenToursText)
	if scanError != nil {
		return nil, fmt.Errorf("failed to read seen tours: %w", scanError)
	}

	seenTours := []string{}
	for _, tourName := range strings.Split(seenToursText, ",") {
		if tourName != "" {
			seenTours = append(seenTours, tourName)
		}
	}
	return seenTours, nil
}

func (repository *Repository) MarkTourSeen(ctx context.Context, querier database.Querier, companyId uuid.UUID, userId uuid.UUID, tourName string) error {
	query := `
		UPDATE users
		SET seen_tours = CASE WHEN seen_tours = '' THEN CAST($3 AS TEXT) ELSE seen_tours || ',' || CAST($3 AS TEXT) END,
		    updated_at = $4
		WHERE company_id = $1 AND id = $2
		  AND (',' || seen_tours || ',') NOT LIKE '%,' || CAST($3 AS TEXT) || ',%'
	`

	_, updateError := querier.ExecContext(ctx, query, companyId, userId, tourName, time.Now().UTC())
	if updateError != nil {
		return fmt.Errorf("failed to mark tour seen: %w", updateError)
	}

	return nil
}

func (repository *Repository) DeleteSession(ctx context.Context, querier database.Querier, companyId uuid.UUID, sessionId uuid.UUID) error {
	query := `DELETE FROM sessions WHERE company_id = $1 AND id = $2`

	_, deleteError := querier.ExecContext(ctx, query, companyId, sessionId)
	if deleteError != nil {
		return fmt.Errorf("failed to delete session: %w", deleteError)
	}

	return nil
}
