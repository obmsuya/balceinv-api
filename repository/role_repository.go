package repository

import (
	"github.com/chrisostomemataba/balceinv-api/models"
	"gorm.io/gorm"
)

type RoleRepository struct {
	db *gorm.DB
}

func NewRoleRepository(db *gorm.DB) *RoleRepository {
	return &RoleRepository{db: db}
}

func (r *RoleRepository) FindAll() ([]models.Role, error) {
	var roles []models.Role
	// Preload only the columns we need from users so we do not expose password hashes
	err := r.db.Preload("Users").Find(&roles).Error
	return roles, err
}

func (r *RoleRepository) FindByID(id uint) (*models.Role, error) {
	var role models.Role
	err := r.db.Preload("Users").First(&role, id).Error
	if err != nil {
		return nil, err
	}
	return &role, nil
}

func (r *RoleRepository) FindByName(name string) (*models.Role, error) {
	var role models.Role
	err := r.db.Where("name = ?", name).First(&role).Error
	if err != nil {
		return nil, err
	}
	return &role, nil
}


func (r *RoleRepository) HasUsers(roleID uint) (bool, error) {
	var count int64
	err := r.db.Model(&models.User{}).Where("role_id = ?", roleID).Count(&count).Error
	return count > 0, err
}

func (r *RoleRepository) Create(role *models.Role) error {
	return r.db.Create(role).Error
}

func (r *RoleRepository) Update(role *models.Role) error {
	return r.db.Save(role).Error
}

func (r *RoleRepository) Delete(id uint) error {
	return r.db.Delete(&models.Role{}, id).Error
}

// AssignToUser updates the user's role_id — used by the assign endpoint.
func (r *RoleRepository) AssignToUser(userID, roleID uint) error {
	return r.db.Model(&models.User{}).
		Where("id = ?", userID).
		Update("role_id", roleID).Error
}