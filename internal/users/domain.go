package users

import (
	"time"

	"github.com/google/uuid"
)

type User struct {
	Id                 uuid.UUID
	CompanyId          uuid.UUID
	RoleId             uuid.UUID
	Name               string
	Email              string
	PasswordHash       string
	Locale             *string
	IsActive           bool
	MustChangePassword bool
	CreatedAt          time.Time
	UpdatedAt          time.Time
	RoleName           string
	RoleIsOwner        bool
}
