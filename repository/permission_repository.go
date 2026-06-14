package repository

import (
	"github.com/chrisostomemataba/balceinv-api/models"
	"gorm.io/gorm"
)

type PermissionRepository struct {
	db *gorm.DB
}

func NewPermissionRepository(db *gorm.DB) *PermissionRepository {
	return &PermissionRepository{db: db}
}

func (r *PermissionRepository) FindAll() ([]models.Permission, error) {
	var perms []models.Permission
	err := r.db.Order("resource ASC, action ASC").Find(&perms).Error
	return perms, err
}

func (r *PermissionRepository) FindByID(id uint) (*models.Permission, error) {
	var perm models.Permission
	err := r.db.First(&perm, id).Error
	if err != nil {
		return nil, err
	}
	return &perm, nil
}

// FindByRoleID returns all permissions assigned to a role via the role_permissions join table.
func (r *PermissionRepository) FindByRoleID(roleID uint) ([]models.Permission, error) {
	var rolePerms []models.RolePermission
	err := r.db.Preload("Permission").Where("role_id = ?", roleID).Find(&rolePerms).Error
	if err != nil {
		return nil, err
	}

	perms := make([]models.Permission, len(rolePerms))
	for i, rp := range rolePerms {
		perms[i] = rp.Permission
	}
	return perms, nil
}

// FindByUserID returns permissions granted directly to a user via user_permissions.
func (r *PermissionRepository) FindByUserID(userID uint) ([]models.Permission, error) {
	var userPerms []models.UserPermission
	err := r.db.Preload("Permission").Where("user_id = ?", userID).Find(&userPerms).Error
	if err != nil {
		return nil, err
	}

	perms := make([]models.Permission, len(userPerms))
	for i, up := range userPerms {
		perms[i] = up.Permission
	}
	return perms, nil
}

// FindUserWithRole returns a user with their role and that role's permissions preloaded.
func (r *PermissionRepository) FindUserWithRole(userID uint) (*models.User, error) {
	var user models.User
	err := r.db.
		Preload("Role").
		Preload("Role.RolePermissions.Permission").
		First(&user, userID).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

// ReplaceRolePermissions deletes all existing permissions for a role and inserts the new set.
func (r *PermissionRepository) ReplaceRolePermissions(roleID uint, permissionIDs []uint) error {
	if err := r.db.Where("role_id = ?", roleID).Delete(&models.RolePermission{}).Error; err != nil {
		return err
	}

	if len(permissionIDs) == 0 {
		return nil
	}

	rows := make([]models.RolePermission, len(permissionIDs))
	for i, pid := range permissionIDs {
		rows[i] = models.RolePermission{RoleID: roleID, PermissionID: pid}
	}
	return r.db.Create(&rows).Error
}

// ReplaceUserPermissions deletes all existing extra permissions for a user and inserts the new set.
func (r *PermissionRepository) ReplaceUserPermissions(userID uint, permissionIDs []uint) error {
	if err := r.db.Where("user_id = ?", userID).Delete(&models.UserPermission{}).Error; err != nil {
		return err
	}

	if len(permissionIDs) == 0 {
		return nil
	}

	rows := make([]models.UserPermission, len(permissionIDs))
	for i, pid := range permissionIDs {
		rows[i] = models.UserPermission{UserID: userID, PermissionID: pid}
	}
	return r.db.Create(&rows).Error
}