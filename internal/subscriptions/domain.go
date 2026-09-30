package subscriptions

import (
	"time"

	"github.com/google/uuid"
)

type Subscription struct {
	CompanyId   uuid.UUID
	LicenseKey  string
	ExpiresAt   time.Time
	DaysGranted int
	MaxDevices  int
	IsTrial     bool
}
