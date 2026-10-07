package sales

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/google/uuid"
)

type FiscalDocument string

const (
	FiscalReceipt    FiscalDocument = "fiscal_receipts"
	FiscalCreditNote FiscalDocument = "fiscal_credit_notes"
)

func (repository *Repository) InsertFiscalPending(ctx context.Context, querier database.Querier, document FiscalDocument, companyId uuid.UUID, saleId uuid.UUID, createdAt time.Time) error {
	query := `
		INSERT INTO ` + string(document) + ` (company_id, sale_id, status, created_at, updated_at)
		VALUES ($1, $2, 'pending', $3, $3)
	`

	_, insertError := querier.ExecContext(ctx, query, companyId, saleId, createdAt)
	if insertError != nil {
		return fmt.Errorf("failed to queue the sale for the EFD: %w", insertError)
	}
	return nil
}

func (repository *Repository) ClaimFiscal(ctx context.Context, querier database.Querier, document FiscalDocument, companyId uuid.UUID, saleId uuid.UUID, claimedAt time.Time, staleBefore time.Time) (bool, error) {
	query := `
		UPDATE ` + string(document) + `
		SET status = 'sending', attempts = attempts + 1, updated_at = $3
		WHERE company_id = $1 AND sale_id = $2
		  AND (status IN ('pending', 'failed') OR (status = 'sending' AND updated_at < $4))
	`

	claimResult, claimError := querier.ExecContext(ctx, query, companyId, saleId, claimedAt, staleBefore)
	if claimError != nil {
		return false, fmt.Errorf("failed to claim the EFD receipt: %w", claimError)
	}
	claimedRows, rowsError := claimResult.RowsAffected()
	if rowsError != nil {
		return false, fmt.Errorf("failed to read the EFD claim: %w", rowsError)
	}
	return claimedRows == 1, nil
}

func (repository *Repository) FindFiscal(ctx context.Context, querier database.Querier, document FiscalDocument, companyId uuid.UUID, saleId uuid.UUID) (*FiscalView, error) {
	query := `
		SELECT status, attempts, verification_code, verification_url, last_error, sent_at
		FROM ` + string(document) + `
		WHERE company_id = $1 AND sale_id = $2
	`

	fiscalView := FiscalView{}
	scanError := querier.QueryRowContext(ctx, query, companyId, saleId).Scan(
		&fiscalView.Status,
		&fiscalView.Attempts,
		&fiscalView.VerificationCode,
		&fiscalView.VerificationUrl,
		&fiscalView.LastError,
		&fiscalView.SentAt,
	)
	if errors.Is(scanError, sql.ErrNoRows) {
		return nil, nil
	}
	if scanError != nil {
		return nil, fmt.Errorf("failed to find the EFD receipt: %w", scanError)
	}
	return &fiscalView, nil
}

func (repository *Repository) RecordFiscalResult(ctx context.Context, querier database.Querier, document FiscalDocument, companyId uuid.UUID, saleId uuid.UUID, fiscalResult FiscalResult, recordedAt time.Time) error {
	query := `
		UPDATE ` + string(document) + `
		SET status = $3, verification_code = $4, verification_url = $5, last_error = $6, sent_at = $7, updated_at = $8
		WHERE company_id = $1 AND sale_id = $2
	`

	_, updateError := querier.ExecContext(ctx, query,
		companyId,
		saleId,
		fiscalResult.Status,
		fiscalResult.VerificationCode,
		fiscalResult.VerificationUrl,
		fiscalResult.LastError,
		fiscalResult.SentAt,
		recordedAt,
	)
	if updateError != nil {
		return fmt.Errorf("failed to record the EFD result: %w", updateError)
	}
	return nil
}

func (repository *Repository) ListWaitingFiscal(ctx context.Context, querier database.Querier, document FiscalDocument, companyId uuid.UUID, staleBefore time.Time, limit int) ([]uuid.UUID, error) {
	query := `
		SELECT sale_id
		FROM ` + string(document) + `
		WHERE company_id = $1
		  AND (status IN ('pending', 'failed') OR (status = 'sending' AND updated_at < $2))
		ORDER BY created_at, sale_id
		LIMIT $3
	`

	waitingRows, queryError := querier.QueryContext(ctx, query, companyId, staleBefore, limit)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list sales waiting for the EFD: %w", queryError)
	}
	defer waitingRows.Close()

	waitingSaleIds := []uuid.UUID{}
	for waitingRows.Next() {
		saleId := uuid.UUID{}
		scanError := waitingRows.Scan(&saleId)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan a sale waiting for the EFD: %w", scanError)
		}
		waitingSaleIds = append(waitingSaleIds, saleId)
	}
	return waitingSaleIds, waitingRows.Err()
}

func (repository *Repository) CountWaitingFiscal(ctx context.Context, querier database.Querier, document FiscalDocument, companyId uuid.UUID) (int64, error) {
	query := `SELECT COUNT(*) FROM ` + string(document) + ` WHERE company_id = $1 AND status <> 'sent'`

	waitingCount := int64(0)
	scanError := querier.QueryRowContext(ctx, query, companyId).Scan(&waitingCount)
	if scanError != nil {
		return 0, fmt.Errorf("failed to count sales waiting for the EFD: %w", scanError)
	}
	return waitingCount, nil
}

func (repository *Repository) DeleteUnsentFiscal(ctx context.Context, querier database.Querier, companyId uuid.UUID, saleId uuid.UUID) error {
	query := `DELETE FROM fiscal_receipts WHERE company_id = $1 AND sale_id = $2 AND status IN ('pending', 'failed')`

	_, deleteError := querier.ExecContext(ctx, query, companyId, saleId)
	if deleteError != nil {
		return fmt.Errorf("failed to drop the unsent EFD receipt: %w", deleteError)
	}
	return nil
}
