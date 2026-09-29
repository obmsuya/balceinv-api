package access

import (
	"context"
	"fmt"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/google/uuid"
)

func (repository *Repository) ListPermissions(ctx context.Context, querier database.Querier) ([]Permission, error) {
	query := `
		SELECT id, resource, action, description
		FROM permissions
		ORDER BY resource, CASE action WHEN 'view' THEN 1 WHEN 'create' THEN 2 WHEN 'edit' THEN 3 ELSE 4 END
	`

	permissionRows, queryError := querier.QueryContext(ctx, query)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list permissions: %w", queryError)
	}
	defer permissionRows.Close()

	permissions := []Permission{}
	for permissionRows.Next() {
		permission := Permission{}
		scanError := permissionRows.Scan(
			&permission.Id,
			&permission.Resource,
			&permission.Action,
			&permission.Description,
		)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan permission: %w", scanError)
		}
		permissions = append(permissions, permission)
	}

	return permissions, permissionRows.Err()
}

func (repository *Repository) CountKnownPermissions(ctx context.Context, querier database.Querier, permissionIds []string) (int, error) {
	hasNoIds := len(permissionIds) == 0
	if hasNoIds {
		return 0, nil
	}

	query := `SELECT COUNT(*) FROM permissions WHERE id IN (` + database.Placeholders(1, len(permissionIds)) + `)`

	knownCount := 0
	scanError := querier.QueryRowContext(ctx, query, database.ToArguments(permissionIds)...).Scan(&knownCount)
	if scanError != nil {
		return 0, fmt.Errorf("failed to count known permissions: %w", scanError)
	}

	return knownCount, nil
}

func (repository *Repository) ListPermissionIdsByRole(ctx context.Context, querier database.Querier, companyId uuid.UUID, roleIds []uuid.UUID) (map[uuid.UUID][]string, error) {
	permissionIdsByRole := map[uuid.UUID][]string{}
	hasNoRoles := len(roleIds) == 0
	if hasNoRoles {
		return permissionIdsByRole, nil
	}

	query := `
		SELECT role_id, permission_id
		FROM role_permissions
		WHERE company_id = $1 AND role_id IN (` + database.Placeholders(2, len(roleIds)) + `)
		ORDER BY permission_id
	`

	queryArguments := append([]any{companyId}, database.ToArguments(roleIds)...)
	pairRows, queryError := querier.QueryContext(ctx, query, queryArguments...)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list role permissions: %w", queryError)
	}
	defer pairRows.Close()

	for pairRows.Next() {
		roleId := uuid.UUID{}
		permissionId := ""
		scanError := pairRows.Scan(&roleId, &permissionId)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan role permission: %w", scanError)
		}
		permissionIdsByRole[roleId] = append(permissionIdsByRole[roleId], permissionId)
	}

	return permissionIdsByRole, pairRows.Err()
}

func (repository *Repository) ReplaceRolePermissions(ctx context.Context, querier database.Querier, companyId uuid.UUID, roleId uuid.UUID, permissionIds []string) error {
	deleteQuery := `DELETE FROM role_permissions WHERE company_id = $1 AND role_id = $2`
	_, deleteError := querier.ExecContext(ctx, deleteQuery, companyId, roleId)
	if deleteError != nil {
		return fmt.Errorf("failed to clear role permissions: %w", deleteError)
	}

	hasNothingToInsert := len(permissionIds) == 0
	if hasNothingToInsert {
		return nil
	}

	insertQuery := `
		INSERT INTO role_permissions (company_id, role_id, permission_id)
		SELECT $1, $2, id FROM permissions WHERE id IN (` + database.Placeholders(3, len(permissionIds)) + `)
	`
	queryArguments := append([]any{companyId, roleId}, database.ToArguments(permissionIds)...)
	_, insertError := querier.ExecContext(ctx, insertQuery, queryArguments...)
	if insertError != nil {
		return fmt.Errorf("failed to insert role permissions: %w", insertError)
	}

	return nil
}

func (repository *Repository) GrantEveryPermission(ctx context.Context, querier database.Querier, companyId uuid.UUID, roleId uuid.UUID) error {
	query := `
		INSERT INTO role_permissions (company_id, role_id, permission_id)
		SELECT $1, $2, id FROM permissions
	`

	_, insertError := querier.ExecContext(ctx, query, companyId, roleId)
	if insertError != nil {
		return fmt.Errorf("failed to grant every permission: %w", insertError)
	}

	return nil
}

func (repository *Repository) ReplaceUserPermissions(ctx context.Context, querier database.Querier, companyId uuid.UUID, userId uuid.UUID, permissionIds []string) error {
	deleteQuery := `DELETE FROM user_permissions WHERE company_id = $1 AND user_id = $2`
	_, deleteError := querier.ExecContext(ctx, deleteQuery, companyId, userId)
	if deleteError != nil {
		return fmt.Errorf("failed to clear user permissions: %w", deleteError)
	}

	hasNothingToInsert := len(permissionIds) == 0
	if hasNothingToInsert {
		return nil
	}

	insertQuery := `
		INSERT INTO user_permissions (company_id, user_id, permission_id)
		SELECT $1, $2, id FROM permissions WHERE id IN (` + database.Placeholders(3, len(permissionIds)) + `)
	`
	queryArguments := append([]any{companyId, userId}, database.ToArguments(permissionIds)...)
	_, insertError := querier.ExecContext(ctx, insertQuery, queryArguments...)
	if insertError != nil {
		return fmt.Errorf("failed to insert user permissions: %w", insertError)
	}

	return nil
}

func (repository *Repository) ListEffectivePermissions(ctx context.Context, querier database.Querier, companyId uuid.UUID, userId uuid.UUID, isOwner bool) ([]Permission, error) {
	if isOwner {
		return repository.ListPermissions(ctx, querier)
	}

	query := `
		SELECT p.id, p.resource, p.action, p.description
		FROM permissions p
		WHERE p.id IN (
			SELECT rp.permission_id
			FROM role_permissions rp
			JOIN users u ON u.company_id = rp.company_id AND u.role_id = rp.role_id
			WHERE u.company_id = $1 AND u.id = $2
			UNION
			SELECT up.permission_id
			FROM user_permissions up
			WHERE up.company_id = $1 AND up.user_id = $2
		)
		ORDER BY p.resource, p.action
	`

	permissionRows, queryError := querier.QueryContext(ctx, query, companyId, userId)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list effective permissions: %w", queryError)
	}
	defer permissionRows.Close()

	permissions := []Permission{}
	for permissionRows.Next() {
		permission := Permission{}
		scanError := permissionRows.Scan(
			&permission.Id,
			&permission.Resource,
			&permission.Action,
			&permission.Description,
		)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan effective permission: %w", scanError)
		}
		permissions = append(permissions, permission)
	}

	return permissions, permissionRows.Err()
}

func (repository *Repository) ListRolePermissions(ctx context.Context, querier database.Querier, companyId uuid.UUID, roleId uuid.UUID) ([]Permission, error) {
	query := `
		SELECT p.id, p.resource, p.action, p.description
		FROM permissions p
		JOIN role_permissions rp ON rp.permission_id = p.id
		WHERE rp.company_id = $1 AND rp.role_id = $2
		ORDER BY p.resource, p.action
	`

	permissionRows, queryError := querier.QueryContext(ctx, query, companyId, roleId)
	if queryError != nil {
		return nil, fmt.Errorf("failed to list role permissions: %w", queryError)
	}
	defer permissionRows.Close()

	permissions := []Permission{}
	for permissionRows.Next() {
		permission := Permission{}
		scanError := permissionRows.Scan(
			&permission.Id,
			&permission.Resource,
			&permission.Action,
			&permission.Description,
		)
		if scanError != nil {
			return nil, fmt.Errorf("failed to scan role permission: %w", scanError)
		}
		permissions = append(permissions, permission)
	}

	return permissions, permissionRows.Err()
}
