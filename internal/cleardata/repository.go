package cleardata

import (
	"context"
	"database/sql"
	"errors"
	"fmt"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/google/uuid"
)

type Repository struct{}

func NewRepository() *Repository {
	return &Repository{}
}

type ownerCheck struct {
	BusinessName string
	PasswordHash string
}

func (repository *Repository) FindOwnerCheck(ctx context.Context, querier database.Querier, companyId uuid.UUID, ownerId uuid.UUID) (ownerCheck, error) {
	query := `
		SELECT c.name, u.password_hash
		FROM companies c
		JOIN users u ON u.company_id = c.id AND u.id = $2
		WHERE c.id = $1
	`

	foundCheck := ownerCheck{}
	scanError := querier.QueryRowContext(ctx, query, companyId, ownerId).Scan(&foundCheck.BusinessName, &foundCheck.PasswordHash)
	if scanError != nil {
		return ownerCheck{}, fmt.Errorf("failed to read the owner check: %w", scanError)
	}
	return foundCheck, nil
}

func (repository *Repository) ShopToKeep(ctx context.Context, querier database.Querier, companyId uuid.UUID, currentShopId *uuid.UUID) (uuid.UUID, error) {
	query := `
		SELECT id FROM shops
		WHERE company_id = $1 AND is_active = TRUE
		ORDER BY CASE WHEN id = $2 THEN 0 ELSE 1 END, created_at, id
		LIMIT 1
	`

	preferredShopId := uuid.Nil
	if currentShopId != nil {
		preferredShopId = *currentShopId
	}

	keptShopId := uuid.Nil
	scanError := querier.QueryRowContext(ctx, query, companyId, preferredShopId).Scan(&keptShopId)
	if errors.Is(scanError, sql.ErrNoRows) {
		return uuid.Nil, ErrNoShopToKeep
	}
	if scanError != nil {
		return uuid.Nil, fmt.Errorf("failed to choose the shop to keep: %w", scanError)
	}
	return keptShopId, nil
}

func (repository *Repository) StoredFileKeys(ctx context.Context, querier database.Querier, companyId uuid.UUID, ownerId uuid.UUID) ([]string, error) {
	query := `
		SELECT image_key FROM products WHERE company_id = $1 AND image_key IS NOT NULL
		UNION SELECT attachment_key FROM purchases WHERE company_id = $1 AND attachment_key IS NOT NULL
		UNION SELECT attachment_key FROM journal_entries WHERE company_id = $1 AND attachment_key IS NOT NULL
		UNION SELECT screenshot_key FROM support_messages WHERE company_id = $1 AND user_id <> $2 AND screenshot_key IS NOT NULL
	`

	keyRows, queryError := querier.QueryContext(ctx, query, companyId, ownerId)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list stored files: %w", queryError)
	}
	defer keyRows.Close()

	fileKeys := []string{}
	for keyRows.Next() {
		fileKey := ""
		scanError := keyRows.Scan(&fileKey)
		if scanError != nil {
			return nil, fmt.Errorf("failed to read a stored file key: %w", scanError)
		}
		fileKeys = append(fileKeys, fileKey)
	}
	rowsError := keyRows.Err()
	if rowsError != nil {
		return nil, fmt.Errorf("failed to list stored files: %w", rowsError)
	}
	return fileKeys, nil
}

func (repository *Repository) AllowJournalDelete(ctx context.Context, querier database.Querier, isPostgres bool, companyId uuid.UUID) error {
	statement := `INSERT INTO journal_clearing (clearing_company_id) VALUES ($1)`
	if isPostgres {
		statement = `SELECT set_config('app.clearing_company_id', $1, true)`
	}

	_, allowError := querier.ExecContext(ctx, statement, companyId.String())
	if allowError != nil {
		return fmt.Errorf("failed to open the journal for clearing: %w", allowError)
	}
	return nil
}

func (repository *Repository) EndJournalDelete(ctx context.Context, querier database.Querier, isPostgres bool, companyId uuid.UUID) error {
	if isPostgres {
		return nil
	}

	_, endError := querier.ExecContext(ctx, `DELETE FROM journal_clearing WHERE clearing_company_id = $1`, companyId.String())
	if endError != nil {
		return fmt.Errorf("failed to close the journal after clearing: %w", endError)
	}
	return nil
}

func (repository *Repository) DeleteBusinessData(ctx context.Context, querier database.Querier, companyId uuid.UUID) (map[string]int64, error) {
	removedByTable := map[string]int64{}
	for _, tableName := range ClearedTables {
		deleteResult, deleteError := querier.ExecContext(ctx, `DELETE FROM `+tableName+` WHERE company_id = $1`, companyId)
		if deleteError != nil {
			return nil, fmt.Errorf("failed to clear %s: %w", tableName, deleteError)
		}
		removedCount, countError := deleteResult.RowsAffected()
		if countError != nil {
			return nil, fmt.Errorf("failed to count cleared %s: %w", tableName, countError)
		}
		removedByTable[tableName] = removedCount
	}
	return removedByTable, nil
}

func (repository *Repository) RemoveOtherShops(ctx context.Context, querier database.Querier, companyId uuid.UUID, keptShopId uuid.UUID) (int64, error) {
	statements := []string{
		`UPDATE sessions SET shop_id = $2 WHERE company_id = $1 AND (shop_id IS NULL OR shop_id <> $2)`,
		`UPDATE support_messages SET shop_id = NULL WHERE company_id = $1 AND shop_id <> $2`,
		`DELETE FROM user_shops WHERE company_id = $1 AND shop_id <> $2`,
	}
	for _, statement := range statements {
		_, execError := querier.ExecContext(ctx, statement, companyId, keptShopId)
		if execError != nil {
			return 0, fmt.Errorf("failed to detach the other shops: %w", execError)
		}
	}

	deleteResult, deleteError := querier.ExecContext(ctx, `DELETE FROM shops WHERE company_id = $1 AND id <> $2`, companyId, keptShopId)
	if deleteError != nil {
		return 0, fmt.Errorf("failed to remove the other shops: %w", deleteError)
	}
	removedCount, countError := deleteResult.RowsAffected()
	if countError != nil {
		return 0, fmt.Errorf("failed to count removed shops: %w", countError)
	}
	return removedCount, nil
}

func (repository *Repository) RemoveStaff(ctx context.Context, querier database.Querier, companyId uuid.UUID, ownerId uuid.UUID) (int64, error) {
	statements := []string{
		`UPDATE settings SET updated_by = NULL WHERE company_id = $1 AND updated_by <> $2`,
		`UPDATE company_features SET updated_by = NULL WHERE company_id = $1 AND updated_by <> $2`,
		`DELETE FROM support_messages WHERE company_id = $1 AND user_id <> $2`,
	}
	for _, statement := range statements {
		_, execError := querier.ExecContext(ctx, statement, companyId, ownerId)
		if execError != nil {
			return 0, fmt.Errorf("failed to detach staff records: %w", execError)
		}
	}

	deleteResult, deleteError := querier.ExecContext(ctx, `DELETE FROM users WHERE company_id = $1 AND id <> $2`, companyId, ownerId)
	if deleteError != nil {
		return 0, fmt.Errorf("failed to remove staff accounts: %w", deleteError)
	}
	removedCount, countError := deleteResult.RowsAffected()
	if countError != nil {
		return 0, fmt.Errorf("failed to count removed staff: %w", countError)
	}
	return removedCount, nil
}

func (repository *Repository) RestartKeptShop(ctx context.Context, querier database.Querier, companyId uuid.UUID, keptShopId uuid.UUID) error {
	_, updateError := querier.ExecContext(ctx, `UPDATE shops SET next_receipt_number = 1 WHERE company_id = $1 AND id = $2`, companyId, keptShopId)
	if updateError != nil {
		return fmt.Errorf("failed to restart receipt numbers: %w", updateError)
	}
	return nil
}
