package services

import (
	"errors"

	"github.com/chrisostomemataba/balceinv-api/models"
	"github.com/chrisostomemataba/balceinv-api/repository"
	"gorm.io/gorm"
)

type PermissionService struct {
	repo     *repository.PermissionRepository
	roleRepo *repository.RoleRepository
	userRepo *repository.UserRepository
}

func NewPermissionService(
	repo *repository.PermissionRepository,
	roleRepo *repository.RoleRepository,
	userRepo *repository.UserRepository,
) *PermissionService {
	return &PermissionService{repo: repo, roleRepo: roleRepo, userRepo: userRepo}
}

func (s *PermissionService) GetAll() ([]models.Permission, error) {
	return s.repo.FindAll()
}

func (s *PermissionService) GetByID(id uint) (*models.Permission, error) {
	perm, err := s.repo.FindByID(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("permission not found")
	}
	return perm, err
}

func (s *PermissionService) GetRolePermissions(roleID uint) ([]models.Permission, error) {
	return s.repo.FindByRoleID(roleID)
}

func (s *PermissionService) GetUserPermissions(userID uint) ([]models.Permission, error) {
	user, err := s.repo.FindUserWithRole(userID)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("user not found")
	}
	if err != nil {
		return nil, err
	}

	// Collect role permissions
	rolePerms := make([]models.Permission, 0)
	for _, rp := range user.Role.RolePermissions {
		rolePerms = append(rolePerms, rp.Permission)
	}

	// Collect individual user permissions
	extraPerms, err := s.repo.FindByUserID(userID)
	if err != nil {
		return nil, err
	}

	// Merge and deduplicate by ID — same as your TypeScript Map deduplication
	seen := map[uint]bool{}
	merged := make([]models.Permission, 0)

	for _, p := range append(rolePerms, extraPerms...) {
		if !seen[p.ID] {
			seen[p.ID] = true
			merged = append(merged, p)
		}
	}

	return merged, nil
}

type AssignRolePermissionsInput struct {
	RoleID        uint   `json:"roleId"`
	PermissionIDs []uint `json:"permissionIds"`
}

type AssignUserPermissionsInput struct {
	UserID        uint   `json:"userId"`
	PermissionIDs []uint `json:"permissionIds"`
}

func (s *PermissionService) AssignToRole(input AssignRolePermissionsInput) error {
	if input.RoleID == 0 || input.PermissionIDs == nil {
		return errors.New("role ID and permission IDs are required")
	}
	return s.repo.ReplaceRolePermissions(input.RoleID, input.PermissionIDs)
}

func (s *PermissionService) AssignToUser(input AssignUserPermissionsInput) error {
	if input.UserID == 0 || input.PermissionIDs == nil {
		return errors.New("user ID and permission IDs are required")
	}
	return s.repo.ReplaceUserPermissions(input.UserID, input.PermissionIDs)
}

// CheckPermission is used server-side to verify if a user can perform an action.
// Your TypeScript middleware called this before protected operations.
func (s *PermissionService) CheckPermission(userID uint, resource, action string) (bool, error) {
	perms, err := s.GetUserPermissions(userID)
	if err != nil {
		return false, err
	}
	for _, p := range perms {
		if p.Resource == resource && p.Action == action {
			return true, nil
		}
	}
	return false, nil
}