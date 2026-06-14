package services

import (
	"errors"

	"github.com/chrisostomemataba/balceinv-api/models"
	"github.com/chrisostomemataba/balceinv-api/repository"
	"github.com/chrisostomemataba/balceinv-api/utils"
	"gorm.io/gorm"
)

type UserService struct {
	repo     *repository.UserRepository
	roleRepo *repository.RoleRepository
}

func NewUserService(repo *repository.UserRepository, roleRepo *repository.RoleRepository) *UserService {
	return &UserService{repo: repo, roleRepo: roleRepo}
}

type CreateUserInput struct {
	Name     string `json:"name"`
	Email    string `json:"email"`
	Password string `json:"password"`
	RoleID   uint   `json:"roleId"`
}

type UpdateUserInput struct {
	Name   string `json:"name"`
	Email  string `json:"email"`
	RoleID uint   `json:"roleId"`
}

func (s *UserService) GetAll() ([]models.User, error) {
	return s.repo.FindAll()
}

func (s *UserService) GetByID(id uint) (*models.User, error) {
	user, err := s.repo.FindByID(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("user not found")
	}
	return user, err
}

func (s *UserService) Create(input CreateUserInput) (*models.User, error) {
	if input.Name == "" || input.Email == "" || input.Password == "" || input.RoleID == 0 {
		return nil, errors.New("name, email, password, and role are required")
	}

	// Reject duplicate email before attempting insert
	if _, err := s.repo.FindByEmail(input.Email); err == nil {
		return nil, errors.New("user already exists")
	}

	// Confirm the role exists before assigning it
	if _, err := s.roleRepo.FindByID(input.RoleID); err != nil {
		return nil, errors.New("role not found")
	}

	hash, err := utils.HashPassword(input.Password)
	if err != nil {
		return nil, err
	}

	user := &models.User{
		Name:         input.Name,
		Email:        input.Email,
		PasswordHash: hash,
		RoleID:       input.RoleID,
	}

	if err := s.repo.Create(user); err != nil {
		return nil, err
	}

	return s.repo.FindByID(user.ID)
}

func (s *UserService) Update(id uint, input UpdateUserInput) (*models.User, error) {
	user, err := s.repo.FindByID(id)
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return nil, errors.New("user not found")
	}
	if err != nil {
		return nil, err
	}

	// Only check email uniqueness if the email is actually changing
	if input.Email != "" && input.Email != user.Email {
		if _, err := s.repo.FindByEmail(input.Email); err == nil {
			return nil, errors.New("email already in use")
		}
	}

	if input.RoleID != 0 {
		if _, err := s.roleRepo.FindByID(input.RoleID); err != nil {
			return nil, errors.New("role not found")
		}
		user.RoleID = input.RoleID
	}

	if input.Name != "" {
		user.Name = input.Name
	}
	if input.Email != "" {
		user.Email = input.Email
	}

	if err := s.repo.Update(user); err != nil {
		return nil, err
	}

	return s.repo.FindByID(user.ID)
}

func (s *UserService) UpdatePassword(userID uint, newPassword string) error {
	if len(newPassword) < 6 {
		return errors.New("password must be at least 6 characters long")
	}

	if _, err := s.repo.FindByID(userID); errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New("user not found")
	}

	hash, err := utils.HashPassword(newPassword)
	if err != nil {
		return err
	}

	return s.repo.UpdatePassword(userID, hash)
}

func (s *UserService) Delete(id uint) error {
	if _, err := s.repo.FindByID(id); errors.Is(err, gorm.ErrRecordNotFound) {
		return errors.New("user not found")
	}
	return s.repo.Delete(id)
}