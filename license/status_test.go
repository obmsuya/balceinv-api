package license

import (
	"testing"
	"time"
)

func licenseExpiringAt(expiryTime time.Time, lastKnownTime time.Time) *LicenseState {
	return &LicenseState{
		LicenseKey:    "paid-key",
		ExpiresAt:     expiryTime.UTC().Format(time.RFC3339),
		LastKnownTime: lastKnownTime.UTC().Format(time.RFC3339),
		DaysGranted:   30,
		MaxDevices:    1,
	}
}

func TestStatusAtCoversEveryLicenseState(t *testing.T) {
	currentTime := time.Date(2026, 9, 26, 12, 0, 0, 0, time.UTC)
	hour := time.Hour
	day := 24 * hour

	testCases := []struct {
		name               string
		licenseState       *LicenseState
		licensed           bool
		daysRemaining      int
		isGracePeriod      bool
		graceDaysRemaining int
		lockReason         string
	}{
		{"no license file", nil, false, 0, false, 0, LockReasonMissing},
		{"paid with 10 and a bit days left", licenseExpiringAt(currentTime.Add(10*day+2*hour), currentTime), true, 11, false, 0, ""},
		{"five hours left still counts as a day", licenseExpiringAt(currentTime.Add(5*hour), currentTime), true, 1, false, 0, ""},
		{"expired a day and a bit ago is in grace with 4 days to renew", licenseExpiringAt(currentTime.Add(-(day + 5*hour)), currentTime), true, -1, true, 4, ""},
		{"expired six days ago is locked", licenseExpiringAt(currentTime.Add(-6*day), currentTime), false, -6, false, 0, LockReasonExpired},
		{"clock set back before the last seen time", licenseExpiringAt(currentTime.Add(20*day), currentTime.Add(3*day)), false, 0, false, 0, LockReasonClock},
	}

	for _, testCase := range testCases {
		licenseStatus := statusAt(testCase.licenseState, currentTime)
		if licenseStatus.Licensed != testCase.licensed ||
			licenseStatus.DaysRemaining != testCase.daysRemaining ||
			licenseStatus.IsGracePeriod != testCase.isGracePeriod ||
			licenseStatus.GraceDaysRemaining != testCase.graceDaysRemaining ||
			licenseStatus.LockReason != testCase.lockReason {
			t.Errorf("%s: got %+v", testCase.name, licenseStatus)
		}
	}

	trialStatus := statusAt(&LicenseState{LicenseKey: "trial", ExpiresAt: currentTime.Add(3 * day).Format(time.RFC3339)}, currentTime)
	if !trialStatus.IsTrial || trialStatus.DaysRemaining != 3 {
		t.Errorf("trial: got %+v", trialStatus)
	}
}
