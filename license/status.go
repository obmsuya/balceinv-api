package license

import (
	"math"
	"time"
)

const LockReasonMissing = "missing"
const LockReasonExpired = "expired"
const LockReasonClock = "clock"

type Status struct {
	Licensed           bool   `json:"licensed"`
	ExpiresAt          string `json:"expires_at,omitempty"`
	DaysRemaining      int    `json:"days_remaining"`
	GraceDaysRemaining int    `json:"grace_days_remaining"`
	IsGracePeriod      bool   `json:"is_grace_period"`
	IsTrial            bool   `json:"is_trial"`
	Plan               int    `json:"plan"`
	MaxDevices         int    `json:"max_devices"`
	LockReason         string `json:"lock_reason"`
}

func wholeDaysUntil(fromTime time.Time, toTime time.Time) int {
	return int(math.Ceil(toTime.Sub(fromTime).Hours() / 24))
}

func statusAt(licenseStateObject *LicenseState, currentTime time.Time) Status {
	if licenseStateObject == nil {
		return Status{LockReason: LockReasonMissing}
	}

	licenseStatus := Status{
		ExpiresAt:  licenseStateObject.ExpiresAt,
		IsTrial:    licenseStateObject.IsTrial || licenseStateObject.LicenseKey == "trial",
		Plan:       licenseStateObject.DaysGranted,
		MaxDevices: licenseStateObject.MaxDevices,
	}

	lastKnownTime, lastKnownTimeParseError := time.Parse(time.RFC3339, licenseStateObject.LastKnownTime)
	clockWasRolledBack := lastKnownTimeParseError == nil && currentTime.Before(lastKnownTime)
	if clockWasRolledBack {
		licenseStatus.LockReason = LockReasonClock
		return licenseStatus
	}

	expiryTime, expiryTimeParseError := time.Parse(time.RFC3339, licenseStateObject.ExpiresAt)
	if expiryTimeParseError != nil {
		licenseStatus.LockReason = LockReasonExpired
		return licenseStatus
	}

	gracePeriodDeadlineTime := expiryTime.Add(time.Duration(gracePeriodDays) * 24 * time.Hour)
	licenseStatus.DaysRemaining = wholeDaysUntil(currentTime, expiryTime)
	licenseStatus.IsGracePeriod = currentTime.After(expiryTime) && currentTime.Before(gracePeriodDeadlineTime)
	licenseStatus.Licensed = !currentTime.After(gracePeriodDeadlineTime)

	if licenseStatus.IsGracePeriod {
		licenseStatus.GraceDaysRemaining = wholeDaysUntil(currentTime, gracePeriodDeadlineTime)
	}
	if !licenseStatus.Licensed {
		licenseStatus.LockReason = LockReasonExpired
	}
	return licenseStatus
}

func CurrentStatus() Status {
	licenseStateObject, licenseLoadError := LoadLicenseState()
	if licenseLoadError != nil {
		return statusAt(nil, time.Now())
	}
	return statusAt(licenseStateObject, time.Now())
}
