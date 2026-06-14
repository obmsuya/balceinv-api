package repository

import (
	"github.com/chrisostomemataba/balceinv-api/models"
	"gorm.io/gorm"
)

type UserRepository struct {
	db *gorm.DB
}

func NewUserRepository(db *gorm.DB) *UserRepository {
	return &UserRepository{db: db}
}


func (r *UserRepository) FindAll() ([]models.User, error) {
	var users []models.User
	err := r.db.Preload("Role").Find(&users).Error
	return users, err
}

func (r *UserRepository) FindByID(id uint) (*models.User, error) {
	var user models.User
	err := r.db.Preload("Role").First(&user, id).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *UserRepository) FindByEmail(email string) (*models.User, error) {
	var user models.User
	err := r.db.Where("email = ?", email).First(&user).Error
	if err != nil {
		return nil, err
	}
	return &user, nil
}

func (r *UserRepository) Create(user *models.User) error {
	return r.db.Create(user).Error
}

func (r *UserRepository) Update(user *models.User) error {
	return r.db.Save(user).Error
}

func (r *UserRepository) UpdatePassword(userID uint, passwordHash string) error {
	if err := r.db.Model(&models.User{}).
		Where("id = ?", userID).
		Update("password_hash", passwordHash).Error; err != nil {
		return err
	}
	return r.db.Where("user_id = ?", userID).Delete(&models.Session{}).Error
}

func (r *UserRepository) Delete(id uint) error {
	r.db.Where("user_id = ?", id).Delete(&models.Session{})
	return r.db.Delete(&models.User{}, id).Error
}