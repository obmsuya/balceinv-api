package access

import (
	"time"

	"github.com/google/uuid"
)

type Permission struct {
	Id          string
	Resource    string
	Action      string
	Description string
}

type Role struct {
	Id        uuid.UUID
	CompanyId uuid.UUID
	Name      string
	IsOwner   bool
	UserCount int64
	CreatedAt time.Time
	UpdatedAt time.Time
}
