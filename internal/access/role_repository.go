package access

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

func (repository *Repository) CountRoles(ctx context.Context, querier database.Querier, companyId uuid.UUID) (int64, error) {
	query := `SELECT COUNT(*) FROM roles WHERE company_id = $1`

	roleCount := int64(0)
	scanError := querier.QueryRowContext(ctx, query, companyId).Scan(&roleCount)
	if scanError != nil {
		return 0, fmt.Errorf("failed to count roles: %w", scanError)
	}

	return roleCount, nil
}

func (repository *Repository) ListRoles(ctx context.Context, querier database.Querier, companyId uuid.UUID, limit int, offset int) ([]Role, error) {
	query := `
		SELECT r.id, r.company_id, r.name, r.is_owner, r.created_at, r.updated_at,
		       (SELECT COUNT(*) FROM users u WHERE u.company_id = r.company_id AND u.role_id = r.id AND u.is_active)
		FROM roles r
		WHERE r.company_id = $1
		ORDER BY r.is_owner DESC, r.name
		LIMIT $2 OFFSET $3
	`

	roleRows, queryError := querier.QueryContext(ctx, query, companyId, limit, offset)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list roles: %w", queryError)
	}
	defer roleRows.Close()

	roles := []Role{}
	for roleRows.Next() {
		role := Role{}
		scanError := roleRows.Scan(
			&role.Id,
			&role.CompanyId,
			&role.Name,
			&role.IsOwner,
			&role.CreatedAt,
			&role.UpdatedAt,
			&role.UserCount,
		)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan role: %w", scanError)
		}
		roles = append(roles, role)
	}

	return roles, roleRows.Err()
}

func (repository *Repository) FindRole(ctx context.Context, querier database.Querier, companyId uuid.UUID, roleId uuid.UUID) (*Role, error) {
	query := `
		SELECT r.id, r.company_id, r.name, r.is_owner, r.created_at, r.updated_at,
		       (SELECT COUNT(*) FROM users u WHERE u.company_id = r.company_id AND u.role_id = r.id AND u.is_active)
		FROM roles r
		WHERE r.company_id = $1 AND r.id = $2
	`

	foundRole := Role{}
	scanError := querier.QueryRowContext(ctx, query, companyId, roleId).Scan(
		&foundRole.Id,
		&foundRole.CompanyId,
		&foundRole.Name,
		&foundRole.IsOwner,
		&foundRole.CreatedAt,
		&foundRole.UpdatedAt,
		&foundRole.UserCount,
	)
	if scanError != nil {
		if errors.Is(scanError, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to find role: %w", scanError)
	}

	return &foundRole, nil
}

func (repository *Repository) InsertRole(ctx context.Context, querier database.Querier, newRole Role) error {
	query := `
		INSERT INTO roles (id, company_id, name, is_owner, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6)
	`

	_, insertError := querier.ExecContext(ctx, query,
		newRole.Id,
		newRole.CompanyId,
		newRole.Name,
		newRole.IsOwner,
		newRole.CreatedAt,
		newRole.UpdatedAt,
	)
	if insertError != nil {
		return fmt.Errorf("failed to insert role: %w", insertError)
	}

	return nil
}

func (repository *Repository) RenameRole(ctx context.Context, querier database.Querier, companyId uuid.UUID, roleId uuid.UUID, newName string) error {
	query := `
		UPDATE roles
		SET name = $3, updated_at = $4
		WHERE company_id = $1 AND id = $2
	`

	_, updateError := querier.ExecContext(ctx, query, companyId, roleId, newName, time.Now().UTC())
	if updateError != nil {
		return fmt.Errorf("failed to rename role: %w", updateError)
	}

	return nil
}

func (repository *Repository) DeleteRole(ctx context.Context, querier database.Querier, companyId uuid.UUID, roleId uuid.UUID) error {
	query := `DELETE FROM roles WHERE company_id = $1 AND id = $2`

	_, deleteError := querier.ExecContext(ctx, query, companyId, roleId)
	if deleteError != nil {
		return fmt.Errorf("failed to delete role: %w", deleteError)
	}

	return nil
}

func (repository *Repository) CountAssignedUsers(ctx context.Context, querier database.Querier, companyId uuid.UUID, roleId uuid.UUID) (int64, error) {
	query := `SELECT COUNT(*) FROM users WHERE company_id = $1 AND role_id = $2`

	assignedCount := int64(0)
	scanError := querier.QueryRowContext(ctx, query, companyId, roleId).Scan(&assignedCount)
	if scanError != nil {
		return 0, fmt.Errorf("failed to count users with role: %w", scanError)
	}

	return assignedCount, nil
}

func (repository *Repository) FindUserRole(ctx context.Context, querier database.Querier, companyId uuid.UUID, userId uuid.UUID) (*Role, error) {
	query := `
		SELECT r.id, r.company_id, r.name, r.is_owner, r.created_at, r.updated_at
		FROM users u
		JOIN roles r ON r.company_id = u.company_id AND r.id = u.role_id
		WHERE u.company_id = $1 AND u.id = $2
	`

	foundRole := Role{}
	scanError := querier.QueryRowContext(ctx, query, companyId, userId).Scan(
		&foundRole.Id,
		&foundRole.CompanyId,
		&foundRole.Name,
		&foundRole.IsOwner,
		&foundRole.CreatedAt,
		&foundRole.UpdatedAt,
	)
	if scanError != nil {
		if errors.Is(scanError, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, fmt.Errorf("failed to find user role: %w", scanError)
	}

	return &foundRole, nil
}
