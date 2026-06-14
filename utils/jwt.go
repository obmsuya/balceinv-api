package utils

import (
	"errors"
	"time"

	"github.com/golang-jwt/jwt/v5"
)

type TokenPayload struct {
	UserID    uint   `json:"userId"`
	CompanyID uint   `json:"companyId"`
	Role      string `json:"role"`
	Email     string `json:"email"`
}

func GenerateAccessToken(payload TokenPayload, secret string) (string, error) {
	claims := jwt.MapClaims{
		"userId":    payload.UserID,
		"companyId": payload.CompanyID,
		"role":      payload.Role,
		"email":     payload.Email,
		"exp":       time.Now().Add(4 * time.Hour).Unix(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

func GenerateRefreshToken(userID uint, secret string) (string, error) {
	claims := jwt.MapClaims{
		"userId": userID,
		"exp":    time.Now().Add(7 * 24 * time.Hour).Unix(),
	}
	return jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString([]byte(secret))
}

func VerifyToken(tokenStr, secret string) (*TokenPayload, error) {
	token, err := jwt.Parse(tokenStr, func(t *jwt.Token) (interface{}, error) {
		if _, ok := t.Method.(*jwt.SigningMethodHMAC); !ok {
			return nil, errors.New("unexpected signing method")
		}
		return []byte(secret), nil
	})

	if err != nil || !token.Valid {
		return nil, errors.New("invalid token")
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return nil, errors.New("invalid claims")
	}

	userIDFloat, ok := claims["userId"].(float64)
	if !ok {
		return nil, errors.New("invalid userId in token")
	}

	companyIDFloat, _ := claims["companyId"].(float64)

	return &TokenPayload{
		UserID:    uint(userIDFloat),
		CompanyID: uint(companyIDFloat),
		Role:      claims["role"].(string),
		Email:     claims["email"].(string),
	}, nil
}