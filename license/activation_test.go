package license

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func serveDjangoLicenseExpiringAt(t *testing.T, expiresAt *string) *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		json.NewEncoder(responseWriter).Encode(map[string]interface{}{
			"success":     true,
			"license_key": "paid-key",
			"license_data": map[string]interface{}{
				"expires_at":   *expiresAt,
				"max_devices":  1,
				"days_granted": 30,
			},
			"signature": ComputeSignature("paid-key", *expiresAt, 1, 30),
		})
	}))
}

func TestActivationUpgradesTrialsAndNeverShortensAPaidLicense(t *testing.T) {
	testDirectory := t.TempDir()
	t.Setenv("HOME", testDirectory)
	t.Setenv("XDG_CONFIG_HOME", testDirectory)
	t.Setenv("APPDATA", testDirectory)
	LicenseSecret = "test-secret"
	originalDjangoBaseURL := DjangoBaseURL
	defer func() { DjangoBaseURL = originalDjangoBaseURL }()

	currentTime := time.Now().UTC()
	serverExpiresAt := currentTime.Add(30 * 24 * time.Hour).Format(time.RFC3339)
	fakeDjango := serveDjangoLicenseExpiringAt(t, &serverExpiresAt)
	defer fakeDjango.Close()
	DjangoBaseURL = fakeDjango.URL

	SaveLicenseState(&LicenseState{LicenseKey: "trial", IsTrial: true, ExpiresAt: currentTime.Add(60 * 24 * time.Hour).Format(time.RFC3339)})
	if activationError := ActivateFromDjango(); activationError != nil {
		t.Fatal(activationError)
	}
	if CurrentStatus().IsTrial {
		t.Fatal("a paid license from the server must replace a trial even if the trial ends later")
	}

	serverExpiresAt = currentTime.Add(10 * 24 * time.Hour).Format(time.RFC3339)
	ActivateFromDjango()
	if CurrentStatus().DaysRemaining < 29 {
		t.Fatal("an older license from the server must not shorten the paid license")
	}

	serverExpiresAt = currentTime.Add(60 * 24 * time.Hour).Format(time.RFC3339)
	ActivateFromDjango()
	if CurrentStatus().DaysRemaining < 59 {
		t.Fatal("a renewal from the server must be saved")
	}

	DjangoBaseURL = "http://127.0.0.1:1"
	if !errors.Is(ActivateFromDjango(), ErrLicensingServerUnreachable) {
		t.Fatal("an unreachable server must be reported as ErrLicensingServerUnreachable")
	}
}
