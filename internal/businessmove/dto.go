package businessmove

import (
	"time"

	"github.com/google/uuid"
)

type Manifest struct {
	Format       int       `json:"format"`
	CompanyId    uuid.UUID `json:"company_id"`
	BusinessName string    `json:"business_name"`
	CreatedAt    time.Time `json:"created_at"`
}

type MoveResultView struct {
	BusinessName string `json:"business_name"`
	OwnerEmail   string `json:"owner_email"`
	MediaCount   int    `json:"media_count"`
}
