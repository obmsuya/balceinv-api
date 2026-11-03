package passwordreset

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/google/uuid"
)

type Repository struct{}

func NewRepository() *Repository {
	return &Repository{}
}

func (repository *Repository) CountSince(ctx context.Context, querier database.Querier, companyId uuid.UUID, userId uuid.UUID, since time.Time) (int, error) {
	codeCount := 0
	query := `SELECT COUNT(*) FROM password_reset_codes WHERE company_id = $1 AND user_id = $2 AND created_at >= $3`
	scanError := querier.QueryRowContext(ctx, query, companyId, userId, since).Scan(&codeCount)
	if scanError != nil {
		return 0, fmt.Errorf("failed to count recent reset codes: %w", scanError)
	}
	return codeCount, nil
}

func (repository *Repository) RetireUnused(ctx context.Context, querier database.Querier, companyId uuid.UUID, userId uuid.UUID, retiredAt time.Time) error {
	query := `UPDATE password_reset_codes SET used_at = $3 WHERE company_id = $1 AND user_id = $2 AND used_at IS NULL`
	_, updateError := querier.ExecContext(ctx, query, companyId, userId, retiredAt)
	if updateError != nil {
		return fmt.Errorf("failed to retire old reset codes: %w", updateError)
	}
	return nil
}

func (repository *Repository) Insert(ctx context.Context, querier database.Querier, resetCode ResetCode) error {
	query := `
		INSERT INTO password_reset_codes (id, company_id, user_id, code_hash, attempts, expires_at, created_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7)
	`
	_, insertError := querier.ExecContext(ctx, query, resetCode.Id, resetCode.CompanyId, resetCode.UserId, resetCode.CodeHash,
		resetCode.Attempts, resetCode.ExpiresAt, resetCode.CreatedAt)
	if insertError != nil {
		return fmt.Errorf("failed to save the reset code: %w", insertError)
	}
	return nil
}

func (repository *Repository) FindLive(ctx context.Context, querier database.Querier, companyId uuid.UUID, userId uuid.UUID, checkedAt time.Time) (*ResetCode, error) {
	query := `
		SELECT id, company_id, user_id, code_hash, attempts, expires_at, created_at
		FROM password_reset_codes
		WHERE company_id = $1 AND user_id = $2 AND used_at IS NULL AND expires_at > $3
		ORDER BY created_at DESC
		LIMIT 1
	`
	liveCode := ResetCode{}
	scanError := querier.QueryRowContext(ctx, query, companyId, userId, checkedAt).Scan(
		&liveCode.Id,
		&liveCode.CompanyId,
		&liveCode.UserId,
		&liveCode.CodeHash,
		&liveCode.Attempts,
		&liveCode.ExpiresAt,
		&liveCode.CreatedAt,
	)
	if errors.Is(scanError, sql.ErrNoRows) {
		return nil, nil
	}
	if scanError != nil {
		return nil, fmt.Errorf("failed to find the reset code: %w", scanError)
	}
	return &liveCode, nil
}

func (repository *Repository) CountAttempt(ctx context.Context, querier database.Querier, companyId uuid.UUID, codeId uuid.UUID) error {
	query := `UPDATE password_reset_codes SET attempts = attempts + 1 WHERE company_id = $1 AND id = $2`
	_, updateError := querier.ExecContext(ctx, query, companyId, codeId)
	if updateError != nil {
		return fmt.Errorf("failed to count the reset attempt: %w", updateError)
	}
	return nil
}

func (repository *Repository) MarkUsed(ctx context.Context, querier database.Querier, companyId uuid.UUID, codeId uuid.UUID, usedAt time.Time) error {
	query := `UPDATE password_reset_codes SET used_at = $3 WHERE company_id = $1 AND id = $2`
	_, updateError := querier.ExecContext(ctx, query, companyId, codeId, usedAt)
	if updateError != nil {
		return fmt.Errorf("failed to use up the reset code: %w", updateError)
	}
	return nil
}

func (repository *Repository) FindRecipient(ctx context.Context, querier database.Querier, companyId uuid.UUID, userId uuid.UUID) (Recipient, error) {
	query := `
		SELECT u.name, u.email, c.name, c.phone
		FROM users u
		JOIN companies c ON c.id = u.company_id
		WHERE u.company_id = $1 AND u.id = $2
	`
	recipient := Recipient{}
	scanError := querier.QueryRowContext(ctx, query, companyId, userId).Scan(&recipient.PersonName, &recipient.Email, &recipient.BusinessName, &recipient.BusinessPhone)
	if scanError != nil {
		return Recipient{}, fmt.Errorf("failed to find who gets the reset code: %w", scanError)
	}
	return recipient, nil
}
