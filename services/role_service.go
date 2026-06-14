package services

import (
	"errors"

	"github.com/chrisostomemataba/balceinv-api/models"
	"github.com/chrisostomemataba/balceinv-api/repository"
	"gorm.io/gorm"
)

type RoleService struct {
	repo     *repository.RoleRepository
	userRepo *repository.UserRepository
}

func NewRoleService(repo *repository.RoleRepository, userRepo *repository.UserRepository) *RoleService {
	return &RoleService{repo: repo, userRepo: userRepo}
}

func (s *RoleService) GetAll() ([]models.Role, error) {
	return s.repo.FindAll()
}

func (s *RoleService) GetByID(id uint) (*models.Role, error) {
	role, err := s.repo.FindByID(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("role not found")
	}
	return role, err
}

func (s *RoleService) Create(name string) (*models.Role, error) {
	if name == "" {
		return nil, errors.New("role name is required")
	}

	if _, err := s.repo.FindByName(name); err == nil {
		return nil, errors.New("role already exists")
	}

	role := &models.Role{Name: name}
	if err := s.repo.Create(role); err != nil {
		return nil, err
	}
	return role, nil
}

func (s *RoleService) Update(id uint, name string) (*models.Role, error) {
	role, err := s.repo.FindByID(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("role not found")
	}
	if err != nil {
		return nil, err
	}

	role.Name = name
	if err := s.repo.Update(role); err != nil {
		return nil, err
	}
	return role, nil
}

func (s *RoleService) Delete(id uint) error {
	if _, err := s.repo.FindByID(id); errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New("role not found")
	}

	// Protect against orphaning users who are assigned this role
	hasUsers, err := s.repo.HasUsers(id)
	if err != nil {
		return err
	}
	if hasUsers {
		return errors.New("cannot delete role with assigned users")
	}

	return s.repo.Delete(id)
}

func (s *RoleService) AssignRole(userID, roleID uint) error {
	if _, err := s.userRepo.FindByID(userID); errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New("user not found")
	}
	if _, err := s.repo.FindByID(roleID); errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New("role not found")
	}
	return s.repo.AssignToUser(userID, roleID)
}