package passwordreset

import (
	"errors"
	"time"

	"github.com/google/uuid"
)

const (
	codeLifetime    = 15 * time.Minute
	codeMaxAttempts = 5
	codesPerHour    = 3
)

var (
	ErrInvalidCode     = errors.New("the code is wrong or has expired; ask for a new one")
	ErrPasswordTooLong = errors.New("the password is too long")
	errCodeQuotaUsed   = errors.New("this person already asked for several codes in the last hour")
)

type ResetCode struct {
	Id        uuid.UUID
	CompanyId uuid.UUID
	UserId    uuid.UUID
	CodeHash  string
	Attempts  int
	ExpiresAt time.Time
	CreatedAt time.Time
}

type Recipient struct {
	PersonName    string
	Email         string
	BusinessName  string
	BusinessPhone *string
}
