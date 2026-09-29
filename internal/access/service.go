package access

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/identity"
	"github.com/chrisostomemataba/balceinv-api/internal/common/response"
	"github.com/google/uuid"
)

var (
	ErrRoleNotFound                = errors.New("role not found")
	ErrUserNotFound                = errors.New("user not found")
	ErrRoleNameTaken               = errors.New("a role with this name already exists")
	ErrOwnerRoleLocked             = errors.New("the owner role always has every permission and cannot be changed or deleted")
	ErrRoleInUse                   = errors.New("this role is still assigned to users")
	ErrUnknownPermission           = errors.New("one or more permissions do not exist")
	ErrCannotGrantUnheldPermission = errors.New("you can only grant permissions you have yourself")
)

type Service struct {
	repository *Repository
}

func NewService(repository *Repository) *Service {
	return &Service{
		repository: repository,
	}
}

func (service *Service) ListPermissions(ctx context.Context, querier database.Querier) ([]PermissionView, error) {
	permissions, listError := service.repository.ListPermissions(ctx, querier)
	if listError != nil {
		return nil, listError
	}
	return toPermissionViews(permissions), nil
}

func (service *Service) ListRoles(ctx context.Context, querier database.Querier, companyId uuid.UUID, limit int, offset int) (response.Page[RoleView], error) {
	emptyPage := response.Page[RoleView]{}

	totalRoles, countError := service.repository.CountRoles(ctx, querier, companyId)
	if countError != nil {
		return emptyPage, countError
	}

	pageRoles, listError := service.repository.ListRoles(ctx, querier, companyId, limit, offset)
	if listError != nil {
		return emptyPage, listError
	}

	pageRoleIds := make([]uuid.UUID, 0, len(pageRoles))
	for _, pageRole := range pageRoles {
		pageRoleIds = append(pageRoleIds, pageRole.Id)
	}

	permissionIdsByRole, permissionsError := service.repository.ListPermissionIdsByRole(ctx, querier, companyId, pageRoleIds)
	if permissionsError != nil {
		return emptyPage, permissionsError
	}

	roleViews := make([]RoleView, 0, len(pageRoles))
	for _, pageRole := range pageRoles {
		roleViews = append(roleViews, toRoleView(pageRole, permissionIdsByRole[pageRole.Id]))
	}

	rolePage := response.Page[RoleView]{
		Items:  roleViews,
		Total:  totalRoles,
		Limit:  limit,
		Offset: offset,
	}

	return rolePage, nil
}

func (service *Service) GetRole(ctx context.Context, querier database.Querier, companyId uuid.UUID, roleId uuid.UUID) (RoleView, error) {
	foundRole, findError := service.repository.FindRole(ctx, querier, companyId, roleId)
	if findError != nil {
		return RoleView{}, findError
	}
	if foundRole == nil {
		return RoleView{}, ErrRoleNotFound
	}

	permissionIdsByRole, permissionsError := service.repository.ListPermissionIdsByRole(ctx, querier, companyId, []uuid.UUID{roleId})
	if permissionsError != nil {
		return RoleView{}, permissionsError
	}

	return toRoleView(*foundRole, permissionIdsByRole[roleId]), nil
}

func (service *Service) CreateRole(ctx context.Context, querier database.Querier, principal *identity.Principal, request CreateRoleRequest) (RoleView, error) {
	grantCheckError := service.checkGrantable(ctx, querier, principal, request.PermissionIds)
	if grantCheckError != nil {
		return RoleView{}, grantCheckError
	}

	createdAt := time.Now().UTC()
	newRole := Role{
		Id:        uuid.Must(uuid.NewV7()),
		CompanyId: principal.CompanyId,
		Name:      strings.TrimSpace(request.Name),
		IsOwner:   false,
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}

	insertError := service.repository.InsertRole(ctx, querier, newRole)
	if insertError != nil {
		if database.IsUniqueViolation(insertError) {
			return RoleView{}, ErrRoleNameTaken
		}
		return RoleView{}, insertError
	}

	replaceError := service.repository.ReplaceRolePermissions(ctx, querier, principal.CompanyId, newRole.Id, request.PermissionIds)
	if replaceError != nil {
		return RoleView{}, replaceError
	}

	return service.GetRole(ctx, querier, principal.CompanyId, newRole.Id)
}

func (service *Service) RenameRole(ctx context.Context, querier database.Querier, companyId uuid.UUID, roleId uuid.UUID, request UpdateRoleRequest) (RoleView, error) {
	foundRole, findError := service.repository.FindRole(ctx, querier, companyId, roleId)
	if findError != nil {
		return RoleView{}, findError
	}
	if foundRole == nil {
		return RoleView{}, ErrRoleNotFound
	}

	renameError := service.repository.RenameRole(ctx, querier, companyId, roleId, strings.TrimSpace(request.Name))
	if renameError != nil {
		if database.IsUniqueViolation(renameError) {
			return RoleView{}, ErrRoleNameTaken
		}
		return RoleView{}, renameError
	}

	return service.GetRole(ctx, querier, companyId, roleId)
}

func (service *Service) DeleteRole(ctx context.Context, querier database.Querier, companyId uuid.UUID, roleId uuid.UUID) error {
	foundRole, findError := service.repository.FindRole(ctx, querier, companyId, roleId)
	if findError != nil {
		return findError
	}
	if foundRole == nil {
		return ErrRoleNotFound
	}
	if foundRole.IsOwner {
		return ErrOwnerRoleLocked
	}

	assignedCount, countError := service.repository.CountAssignedUsers(ctx, querier, companyId, roleId)
	if countError != nil {
		return countError
	}
	isRoleInUse := assignedCount > 0
	if isRoleInUse {
		return ErrRoleInUse
	}

	return service.repository.DeleteRole(ctx, querier, companyId, roleId)
}

func (service *Service) ListRolePermissions(ctx context.Context, querier database.Querier, companyId uuid.UUID, roleId uuid.UUID) ([]PermissionView, error) {
	foundRole, findError := service.repository.FindRole(ctx, querier, companyId, roleId)
	if findError != nil {
		return nil, findError
	}
	if foundRole == nil {
		return nil, ErrRoleNotFound
	}

	if foundRole.IsOwner {
		return service.ListPermissions(ctx, querier)
	}

	rolePermissions, listError := service.repository.ListRolePermissions(ctx, querier, companyId, roleId)
	if listError != nil {
		return nil, listError
	}

	return toPermissionViews(rolePermissions), nil
}

func (service *Service) ListUserPermissions(ctx context.Context, querier database.Querier, companyId uuid.UUID, userId uuid.UUID) ([]PermissionView, error) {
	userRole, findRoleError := service.repository.FindUserRole(ctx, querier, companyId, userId)
	if findRoleError != nil {
		return nil, findRoleError
	}
	if userRole == nil {
		return nil, ErrUserNotFound
	}

	effectivePermissions, listError := service.repository.ListEffectivePermissions(ctx, querier, companyId, userId, userRole.IsOwner)
	if listError != nil {
		return nil, listError
	}

	return toPermissionViews(effectivePermissions), nil
}

func (service *Service) AssignRolePermissions(ctx context.Context, querier database.Querier, principal *identity.Principal, roleId uuid.UUID, permissionIds []string) error {
	foundRole, findError := service.repository.FindRole(ctx, querier, principal.CompanyId, roleId)
	if findError != nil {
		return findError
	}
	if foundRole == nil {
		return ErrRoleNotFound
	}
	if foundRole.IsOwner {
		return ErrOwnerRoleLocked
	}

	grantCheckError := service.checkGrantable(ctx, querier, principal, permissionIds)
	if grantCheckError != nil {
		return grantCheckError
	}

	return service.repository.ReplaceRolePermissions(ctx, querier, principal.CompanyId, roleId, permissionIds)
}

func (service *Service) AssignUserPermissions(ctx context.Context, querier database.Querier, principal *identity.Principal, userId uuid.UUID, permissionIds []string) error {
	userRole, findRoleError := service.repository.FindUserRole(ctx, querier, principal.CompanyId, userId)
	if findRoleError != nil {
		return findRoleError
	}
	if userRole == nil {
		return ErrUserNotFound
	}

	grantCheckError := service.checkGrantable(ctx, querier, principal, permissionIds)
	if grantCheckError != nil {
		return grantCheckError
	}

	return service.repository.ReplaceUserPermissions(ctx, querier, principal.CompanyId, userId, permissionIds)
}

func (service *Service) SetUpOwnerRole(ctx context.Context, querier database.Querier, companyId uuid.UUID, createdAt time.Time) (uuid.UUID, error) {
	ownerRole := Role{
		Id:        uuid.Must(uuid.NewV7()),
		CompanyId: companyId,
		Name:      "Owner",
		IsOwner:   true,
		CreatedAt: createdAt,
		UpdatedAt: createdAt,
	}

	insertError := service.repository.InsertRole(ctx, querier, ownerRole)
	if insertError != nil {
		return uuid.Nil, insertError
	}

	grantError := service.repository.GrantEveryPermission(ctx, querier, companyId, ownerRole.Id)
	if grantError != nil {
		return uuid.Nil, grantError
	}

	return ownerRole.Id, nil
}

func (service *Service) checkGrantable(ctx context.Context, querier database.Querier, principal *identity.Principal, permissionIds []string) error {
	uniquePermissionIds := uniqueStrings(permissionIds)

	knownCount, countError := service.repository.CountKnownPermissions(ctx, querier, uniquePermissionIds)
	if countError != nil {
		return fmt.Errorf("failed to check permissions: %w", countError)
	}
	hasUnknownPermission := knownCount != len(uniquePermissionIds)
	if hasUnknownPermission {
		return ErrUnknownPermission
	}

	for _, permissionId := range uniquePermissionIds {
		isHeldByGranter := principal.Can(permissionId)
		if !isHeldByGranter {
			return ErrCannotGrantUnheldPermission
		}
	}

	return nil
}

func uniqueStrings(values []string) []string {
	seenValues := map[string]bool{}
	uniqueValues := make([]string, 0, len(values))
	for _, value := range values {
		if seenValues[value] {
			continue
		}
		seenValues[value] = true
		uniqueValues = append(uniqueValues, value)
	}
	return uniqueValues
}

func toPermissionViews(permissions []Permission) []PermissionView {
	permissionViews := make([]PermissionView, 0, len(permissions))
	for _, permission := range permissions {
		permissionViews = append(permissionViews, PermissionView{
			Id:          permission.Id,
			Resource:    permission.Resource,
			Action:      permission.Action,
			Description: permission.Description,
		})
	}
	return permissionViews
}

func toRoleView(role Role, permissionIds []string) RoleView {
	hasNoPermissionIds := permissionIds == nil
	if hasNoPermissionIds {
		permissionIds = []string{}
	}

	return RoleView{
		Id:            role.Id,
		Name:          role.Name,
		IsOwner:       role.IsOwner,
		UserCount:     role.UserCount,
		PermissionIds: permissionIds,
		CreatedAt:     role.CreatedAt,
		UpdatedAt:     role.UpdatedAt,
	}
}
