package services

import (
	"errors"
	"time"

	"github.com/chrisostomemataba/balceinv-api/models"
	"github.com/chrisostomemataba/balceinv-api/utils"
	"gorm.io/gorm"
)

type AuthService struct {
	db            *gorm.DB
	accessSecret  string
	refreshSecret string
}

func NewAuthService(db *gorm.DB, accessSecret, refreshSecret string) *AuthService {
	return &AuthService{db: db, accessSecret: accessSecret, refreshSecret: refreshSecret}
}

type LoginInput struct {
	Email    string `json:"email"`
	Password string `json:"password"`
}

type UserData struct {
	ID        uint   `json:"id"`
	Name      string `json:"name"`
	Email     string `json:"email"`
	Role      string `json:"role"`
	CompanyID uint   `json:"company_id"`
}

type LoginResult struct {
	User         UserData `json:"user"`
	AccessToken  string   `json:"access_token"`
	RefreshToken string   `json:"refresh_token"`
}

func (s *AuthService) Login(input LoginInput) (*LoginResult, error) {
	var user models.User
	if err := s.db.Preload("Role").Where("email = ?", input.Email).First(&user).Error; err != nil {
		return nil, errors.New("invalid credentials")
	}

	if !utils.CheckPassword(input.Password, user.PasswordHash) {
		return nil, errors.New("invalid credentials")
	}

	accessToken, err := utils.GenerateAccessToken(utils.TokenPayload{
		UserID:    user.ID,
		CompanyID: user.CompanyID,
		Role:      user.Role.Name,
		Email:     user.Email,
	}, s.accessSecret)
	if err != nil {
		return nil, err
	}

	refreshToken, err := utils.GenerateRefreshToken(user.ID, s.refreshSecret)
	if err != nil {
		return nil, err
	}

	session := models.Session{
		UserID:       user.ID,
		RefreshToken: refreshToken,
		ExpiresAt:    time.Now().Add(7 * 24 * time.Hour),
	}
	s.db.Create(&session)

	s.db.Create(&models.LoginLog{UserID: user.ID})

	return &LoginResult{
		User: UserData{
			ID:        user.ID,
			Name:      user.Name,
			Email:     user.Email,
			Role:      user.Role.Name,
			CompanyID: user.CompanyID,
		},
		AccessToken:  accessToken,
		RefreshToken: refreshToken,
	}, nil
}

func (s *AuthService) Logout(refreshToken string) error {
	s.db.Where("refresh_token = ?", refreshToken).Delete(&models.Session{})
	return nil
}

func (s *AuthService) Refresh(refreshToken string) (string, string, error) {
	var session models.Session
	if err := s.db.Where("refresh_token = ?", refreshToken).First(&session).Error; err != nil {
		return "", "", errors.New("session not found")
	}

	if session.ExpiresAt.Before(time.Now()) {
		s.db.Delete(&session)
		return "", "", errors.New("session expired")
	}

	var user models.User
	if err := s.db.Preload("Role").First(&user, session.UserID).Error; err != nil {
		return "", "", errors.New("user not found")
	}

	newAccess, err := utils.GenerateAccessToken(utils.TokenPayload{
		UserID:    user.ID,
		CompanyID: user.CompanyID,
		Role:      user.Role.Name,
		Email:     user.Email,
	}, s.accessSecret)
	if err != nil {
		return "", "", err
	}

	newRefresh, err := utils.GenerateRefreshToken(user.ID, s.refreshSecret)
	if err != nil {
		return "", "", err
	}

	s.db.Model(&session).Updates(models.Session{
		RefreshToken: newRefresh,
		ExpiresAt:    time.Now().Add(7 * 24 * time.Hour),
	})

	return newAccess, newRefresh, nil
}

func (s *AuthService) GetCurrentUser(userID uint) (*UserData, error) {
	var user models.User
	if err := s.db.Preload("Role").First(&user, userID).Error; err != nil {
		return nil, errors.New("user not found")
	}
	return &UserData{
		ID:        user.ID,
		Name:      user.Name,
		Email:     user.Email,
		Role:      user.Role.Name,
		CompanyID: user.CompanyID,
	}, nil
}