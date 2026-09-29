package support

import (
	"context"
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/google/uuid"
)

const messageColumns = `
	id, company_id, shop_id, user_id, topic, message, contact_email, contact_phone, include_details,
	details, screenshot_key, status, attempts, last_error, next_attempt_at, sent_at, created_at
`

type Repository struct{}

func NewRepository() *Repository {
	return &Repository{}
}

func (repository *Repository) FindAuthor(ctx context.Context, querier database.Querier, companyId uuid.UUID, userId uuid.UUID, shopId *uuid.UUID) (Author, error) {
	query := `
		SELECT companies.name, companies.timezone, users.name, COALESCE(users.locale, companies.default_locale), shops.name
		FROM companies
		JOIN users ON users.company_id = companies.id AND users.id = $2
		LEFT JOIN shops ON shops.company_id = companies.id AND shops.id = $3
		WHERE companies.id = $1
	`

	foundAuthor := Author{}
	scanError := querier.QueryRowContext(ctx, query, companyId, userId, shopId).Scan(
		&foundAuthor.CompanyName,
		&foundAuthor.Timezone,
		&foundAuthor.UserName,
		&foundAuthor.Language,
		&foundAuthor.ShopName,
	)
	if errors.Is(scanError, sql.ErrNoRows) {
		return Author{}, ErrUserNotFound
	}
	if scanError != nil {
		return Author{}, fmt.Errorf("failed to find who is writing to support: %w", scanError)
	}
	return foundAuthor, nil
}

func (repository *Repository) CountSince(ctx context.Context, querier database.Querier, companyId uuid.UUID, since time.Time) (int64, error) {
	query := `
		SELECT COUNT(*)
		FROM support_messages
		WHERE company_id = $1 AND created_at > $2
	`

	messageCount := int64(0)
	scanError := querier.QueryRowContext(ctx, query, companyId, since).Scan(&messageCount)
	if scanError != nil {
		return 0, fmt.Errorf("failed to count recent support messages: %w", scanError)
	}
	return messageCount, nil
}

func (repository *Repository) Insert(ctx context.Context, querier database.Querier, newMessage Message) error {
	detailsBytes, marshalError := json.Marshal(newMessage.Details)
	if marshalError != nil {
		return fmt.Errorf("failed to encode the support details: %w", marshalError)
	}

	query := `
		INSERT INTO support_messages (
			id, company_id, shop_id, user_id, topic, message, contact_email, contact_phone, include_details,
			details, screenshot_key, status, attempts, next_attempt_at, created_at, updated_at
		) VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11, $12, 0, $13, $13, $13)
	`

	_, insertError := querier.ExecContext(ctx, query,
		newMessage.Id,
		newMessage.CompanyId,
		newMessage.ShopId,
		newMessage.UserId,
		newMessage.Topic,
		newMessage.Body,
		newMessage.ContactEmail,
		newMessage.ContactPhone,
		newMessage.IncludeDetails,
		string(detailsBytes),
		newMessage.ScreenshotKey,
		newMessage.Status,
		newMessage.CreatedAt,
	)
	if insertError != nil {
		return fmt.Errorf("failed to save the support message: %w", insertError)
	}
	return nil
}

func (repository *Repository) ListForUser(ctx context.Context, querier database.Querier, companyId uuid.UUID, userId uuid.UUID, limit int) ([]Message, error) {
	query := `
		SELECT ` + messageColumns + `
		FROM support_messages
		WHERE company_id = $1 AND user_id = $2
		ORDER BY created_at DESC
		LIMIT $3
	`

	messageRows, queryError := querier.QueryContext(ctx, query, companyId, userId, limit)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list support messages: %w", queryError)
	}
	defer messageRows.Close()

	foundMessages := []Message{}
	for messageRows.Next() {
		foundMessage, scanError := scanMessage(messageRows)
		if scanError != nil {
			return nil, scanError
		}
		foundMessages = append(foundMessages, foundMessage)
	}
	rowsError := messageRows.Err()
	if rowsError != nil {
		return nil, fmt.Errorf("failed to read support messages: %w", rowsError)
	}
	return foundMessages, nil
}

func (repository *Repository) Find(ctx context.Context, querier database.Querier, companyId uuid.UUID, messageId uuid.UUID) (*Message, error) {
	query := `
		SELECT ` + messageColumns + `
		FROM support_messages
		WHERE company_id = $1 AND id = $2
	`

	foundMessage, scanError := scanMessage(querier.QueryRowContext(ctx, query, companyId, messageId))
	if errors.Is(scanError, sql.ErrNoRows) {
		return nil, nil
	}
	if scanError != nil {
		return nil, scanError
	}
	return &foundMessage, nil
}

func (repository *Repository) OpenOutbox(ctx context.Context, querier database.Querier, isPostgres bool) error {
	if !isPostgres {
		return nil
	}
	_, setFlagError := querier.ExecContext(ctx, `SELECT set_config('app.support_outbox', 'on', true)`)
	if setFlagError != nil {
		return fmt.Errorf("failed to open the support outbox: %w", setFlagError)
	}
	return nil
}

func (repository *Repository) ListDue(ctx context.Context, querier database.Querier, now time.Time, staleBefore time.Time, maximumAttempts int, limit int) ([]dueMessage, error) {
	query := `
		SELECT company_id, id
		FROM support_messages
		WHERE (status IN ('pending', 'failed') AND next_attempt_at <= $1 AND attempts < $3)
		   OR (status = 'sending' AND updated_at < $2)
		ORDER BY created_at
		LIMIT $4
	`

	dueRows, queryError := querier.QueryContext(ctx, query, now, staleBefore, maximumAttempts, limit)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list support messages waiting to be sent: %w", queryError)
	}
	defer dueRows.Close()

	dueMessages := []dueMessage{}
	for dueRows.Next() {
		foundDue := dueMessage{}
		scanError := dueRows.Scan(&foundDue.CompanyId, &foundDue.MessageId)
		if scanError != nil {
			return nil, fmt.Errorf("failed to read a waiting support message: %w", scanError)
		}
		dueMessages = append(dueMessages, foundDue)
	}
	rowsError := dueRows.Err()
	if rowsError != nil {
		return nil, fmt.Errorf("failed to read waiting support messages: %w", rowsError)
	}
	return dueMessages, nil
}

func (repository *Repository) Claim(ctx context.Context, querier database.Querier, companyId uuid.UUID, messageId uuid.UUID, now time.Time, staleBefore time.Time, maximumAttempts int) (bool, error) {
	query := `
		UPDATE support_messages
		SET status = 'sending', updated_at = $3
		WHERE company_id = $1 AND id = $2
		  AND ((status IN ('pending', 'failed') AND next_attempt_at <= $3 AND attempts < $5)
		    OR (status = 'sending' AND updated_at < $4))
	`

	claimResult, claimError := querier.ExecContext(ctx, query, companyId, messageId, now, staleBefore, maximumAttempts)
	if claimError != nil {
		return false, fmt.Errorf("failed to claim the support message: %w", claimError)
	}
	claimedRows, rowsError := claimResult.RowsAffected()
	if rowsError != nil {
		return false, fmt.Errorf("failed to read the support message claim: %w", rowsError)
	}
	return claimedRows == 1, nil
}

func (repository *Repository) RecordAttempt(ctx context.Context, querier database.Querier, attemptedMessage Message, now time.Time) error {
	query := `
		UPDATE support_messages
		SET status = $3, attempts = $4, last_error = $5, next_attempt_at = $6, sent_at = $7, updated_at = $8
		WHERE company_id = $1 AND id = $2
	`

	_, updateError := querier.ExecContext(ctx, query,
		attemptedMessage.CompanyId,
		attemptedMessage.Id,
		attemptedMessage.Status,
		attemptedMessage.Attempts,
		attemptedMessage.LastError,
		attemptedMessage.NextAttemptAt,
		attemptedMessage.SentAt,
		now,
	)
	if updateError != nil {
		return fmt.Errorf("failed to record the support message result: %w", updateError)
	}
	return nil
}

type rowScanner interface {
	Scan(destinations ...any) error
}

func scanMessage(row rowScanner) (Message, error) {
	foundMessage := Message{}
	detailsBytes := []byte{}
	scanError := row.Scan(
		&foundMessage.Id,
		&foundMessage.CompanyId,
		&foundMessage.ShopId,
		&foundMessage.UserId,
		&foundMessage.Topic,
		&foundMessage.Body,
		&foundMessage.ContactEmail,
		&foundMessage.ContactPhone,
		&foundMessage.IncludeDetails,
		&detailsBytes,
		&foundMessage.ScreenshotKey,
		&foundMessage.Status,
		&foundMessage.Attempts,
		&foundMessage.LastError,
		&foundMessage.NextAttemptAt,
		&foundMessage.SentAt,
		&foundMessage.CreatedAt,
	)
	if errors.Is(scanError, sql.ErrNoRows) {
		return Message{}, scanError
	}
	if scanError != nil {
		return Message{}, fmt.Errorf("failed to read the support message: %w", scanError)
	}

	unmarshalError := json.Unmarshal(detailsBytes, &foundMessage.Details)
	if unmarshalError != nil {
		return Message{}, fmt.Errorf("failed to decode the support details: %w", unmarshalError)
	}
	return foundMessage, nil
}
