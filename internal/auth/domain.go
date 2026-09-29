package auth

import (
	"time"

	"github.com/google/uuid"
)

type Session struct {
	Id         uuid.UUID
	TokenHash  string
	CompanyId  uuid.UUID
	UserId     uuid.UUID
	ShopId     *uuid.UUID
	IpAddress  string
	UserAgent  string
	CreatedAt  time.Time
	LastSeenAt time.Time
	ExpiresAt  time.Time
}

type LoginCandidate struct {
	UserId       uuid.UUID
	CompanyId    uuid.UUID
	PasswordHash string
	IsActive     bool
}

type LoginAttempt struct {
	Id        uuid.UUID
	Email     string
	IpAddress string
	Succeeded bool
	CreatedAt time.Time
}
