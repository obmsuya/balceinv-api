package users

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

const userColumns = `
	u.id, u.company_id, u.role_id, u.name, u.email, u.password_hash, u.locale,
	u.is_active, u.must_change_password, u.created_at, u.updated_at, r.name, r.is_owner
`

func (repository *Repository) Count(ctx context.Context, querier database.Querier, companyId uuid.UUID, searchText string) (int64, error) {
	query := `
		SELECT COUNT(*)
		FROM users u
		WHERE u.company_id = $1
		  AND ($2 = '' OR lower(u.name) LIKE $3 OR u.email LIKE $3)
	`

	userCount := int64(0)
	scanError := querier.QueryRowContext(ctx, query, companyId, searchText, likePattern(searchText)).Scan(&userCount)
	if scanError != nil {
		return 0, fmt.Errorf("failed to count users: %w", scanError)
	}

	return userCount, nil
}

func (repository *Repository) List(ctx context.Context, querier database.Querier, companyId uuid.UUID, searchText string, limit int, offset int) ([]User, error) {
	query := `
		SELECT ` + userColumns + `
		FROM users u
		JOIN roles r ON r.company_id = u.company_id AND r.id = u.role_id
		WHERE u.company_id = $1
		  AND ($2 = '' OR lower(u.name) LIKE $3 OR u.email LIKE $3)
		ORDER BY u.is_active DESC, u.name
		LIMIT $4 OFFSET $5
	`

	userRows, queryError := querier.QueryContext(ctx, query, companyId, searchText, likePattern(searchText), limit, offset)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list users: %w", queryError)
	}
	defer userRows.Close()

	foundUsers := []User{}
	for userRows.Next() {
		foundUser, scanError := scanUser(userRows)
		if scanError != nil {
			return nil, scanError
		}
		foundUsers = append(foundUsers, foundUser)
	}

	return foundUsers, userRows.Err()
}

func (repository *Repository) Find(ctx context.Context, querier database.Querier, companyId uuid.UUID, userId uuid.UUID) (*User, error) {
	query := `
		SELECT ` + userColumns + `
		FROM users u
		JOIN roles r ON r.company_id = u.company_id AND r.id = u.role_id
		WHERE u.company_id = $1 AND u.id = $2
	`

	userRow := querier.QueryRowContext(ctx, query, companyId, userId)
	foundUser, scanError := scanUser(userRow)
	if scanError != nil {
		if errors.Is(scanError, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, scanError
	}

	return &foundUser, nil
}

func (repository *Repository) Insert(ctx context.Context, querier database.Querier, newUser User) error {
	query := `
		INSERT INTO users (id, company_id, role_id, name, email, password_hash, locale, is_active, must_change_password, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $10, $11)
	`

	_, insertError := querier.ExecContext(ctx, query,
		newUser.Id,
		newUser.CompanyId,
		newUser.RoleId,
		newUser.Name,
		newUser.Email,
		newUser.PasswordHash,
		newUser.Locale,
		newUser.IsActive,
		newUser.MustChangePassword,
		newUser.CreatedAt,
		newUser.UpdatedAt,
	)
	if insertError != nil {
		return fmt.Errorf("failed to insert user: %w", insertError)
	}

	return nil
}

func (repository *Repository) UpdateProfile(ctx context.Context, querier database.Querier, changedUser User) error {
	query := `
		UPDATE users
		SET name = $3, email = $4, role_id = $5, is_active = $6, updated_at = $7
		WHERE company_id = $1 AND id = $2
	`

	_, updateError := querier.ExecContext(ctx, query,
		changedUser.CompanyId,
		changedUser.Id,
		changedUser.Name,
		changedUser.Email,
		changedUser.RoleId,
		changedUser.IsActive,
		time.Now().UTC(),
	)
	if updateError != nil {
		return fmt.Errorf("failed to update user: %w", updateError)
	}

	return nil
}

func (repository *Repository) UpdatePasswordHash(ctx context.Context, querier database.Querier, companyId uuid.UUID, userId uuid.UUID, passwordHash string) error {
	query := `
		UPDATE users
		SET password_hash = $3, must_change_password = FALSE, updated_at = $4
		WHERE company_id = $1 AND id = $2
	`

	_, updateError := querier.ExecContext(ctx, query, companyId, userId, passwordHash, time.Now().UTC())
	if updateError != nil {
		return fmt.Errorf("failed to update password: %w", updateError)
	}

	return nil
}

func (repository *Repository) CountActiveOwners(ctx context.Context, querier database.Querier, companyId uuid.UUID) (int64, error) {
	query := `
		SELECT COUNT(*)
		FROM users u
		JOIN roles r ON r.company_id = u.company_id AND r.id = u.role_id
		WHERE u.company_id = $1 AND u.is_active AND r.is_owner
	`

	ownerCount := int64(0)
	scanError := querier.QueryRowContext(ctx, query, companyId).Scan(&ownerCount)
	if scanError != nil {
		return 0, fmt.Errorf("failed to count owners: %w", scanError)
	}

	return ownerCount, nil
}

func (repository *Repository) ReplaceShops(ctx context.Context, querier database.Querier, companyId uuid.UUID, userId uuid.UUID, shopIds []uuid.UUID) (int64, error) {
	deleteQuery := `DELETE FROM user_shops WHERE company_id = $1 AND user_id = $2`
	_, deleteError := querier.ExecContext(ctx, deleteQuery, companyId, userId)
	if deleteError != nil {
		return 0, fmt.Errorf("failed to clear user shops: %w", deleteError)
	}

	hasNoShops := len(shopIds) == 0
	if hasNoShops {
		return 0, nil
	}

	insertQuery := `
		INSERT INTO user_shops (company_id, user_id, shop_id)
		SELECT s.company_id, $2, s.id
		FROM shops s
		WHERE s.company_id = $1 AND s.id IN (` + database.Placeholders(3, len(shopIds)) + `)
	`
	queryArguments := append([]any{companyId, userId}, database.ToArguments(shopIds)...)
	insertResult, insertError := querier.ExecContext(ctx, insertQuery, queryArguments...)
	if insertError != nil {
		return 0, fmt.Errorf("failed to assign shops: %w", insertError)
	}

	assignedCount, rowsAffectedError := insertResult.RowsAffected()
	if rowsAffectedError != nil {
		return 0, fmt.Errorf("failed to count assigned shops: %w", rowsAffectedError)
	}

	return assignedCount, nil
}

func (repository *Repository) ListShopIds(ctx context.Context, querier database.Querier, companyId uuid.UUID, userIds []uuid.UUID) (map[uuid.UUID][]uuid.UUID, error) {
	shopIdsByUser := map[uuid.UUID][]uuid.UUID{}
	hasNoUsers := len(userIds) == 0
	if hasNoUsers {
		return shopIdsByUser, nil
	}

	query := `
		SELECT us.user_id, us.shop_id
		FROM user_shops us
		JOIN shops s ON s.company_id = us.company_id AND s.id = us.shop_id
		WHERE us.company_id = $1 AND us.user_id IN (` + database.Placeholders(2, len(userIds)) + `)
		ORDER BY s.name
	`

	queryArguments := append([]any{companyId}, database.ToArguments(userIds)...)
	pairRows, queryError := querier.QueryContext(ctx, query, queryArguments...)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list user shops: %w", queryError)
	}
	defer pairRows.Close()

	for pairRows.Next() {
		userId := uuid.UUID{}
		shopId := uuid.UUID{}
		scanError := pairRows.Scan(&userId, &shopId)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan user shop: %w", scanError)
		}
		shopIdsByUser[userId] = append(shopIdsByUser[userId], shopId)
	}

	return shopIdsByUser, pairRows.Err()
}

func (repository *Repository) RevokeSessions(ctx context.Context, querier database.Querier, companyId uuid.UUID, userId uuid.UUID, keptSessionId uuid.UUID) error {
	query := `DELETE FROM sessions WHERE company_id = $1 AND user_id = $2 AND id <> $3`

	_, deleteError := querier.ExecContext(ctx, query, companyId, userId, keptSessionId)
	if deleteError != nil {
		return fmt.Errorf("failed to revoke sessions: %w", deleteError)
	}

	return nil
}

type rowScanner interface {
	Scan(destinations ...any) error
}

func scanUser(row rowScanner) (User, error) {
	scannedUser := User{}
	scanError := row.Scan(
		&scannedUser.Id,
		&scannedUser.CompanyId,
		&scannedUser.RoleId,
		&scannedUser.Name,
		&scannedUser.Email,
		&scannedUser.PasswordHash,
		&scannedUser.Locale,
		&scannedUser.IsActive,
		&scannedUser.MustChangePassword,
		&scannedUser.CreatedAt,
		&scannedUser.UpdatedAt,
		&scannedUser.RoleName,
		&scannedUser.RoleIsOwner,
	)
	if scanError != nil {
		if errors.Is(scanError, sql.ErrNoRows) {
			return scannedUser, scanError
		}
		return scannedUser, fmt.Errorf("failed to scan user: %w", scanError)
	}
	return scannedUser, nil
}

func likePattern(searchText string) string {
	return "%" + strings.ToLower(strings.TrimSpace(searchText)) + "%"
}
