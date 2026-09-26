package backup

import (
	"errors"
	"path/filepath"
	"testing"
	"time"

	"github.com/chrisostomemataba/balceinv-api/license"
)

func TestUnreachableCloudIsReportedInPlainWords(t *testing.T) {
	testDirectory := useTemporaryAppData(t)
	lastCloudAttemptAt, lastCloudSuccessAt, lastCloudError = nil, nil, ""
	license.LicenseSecret = "test-secret"
	license.SaveLicenseState(&license.LicenseState{
		LicenseKey: "paid-key",
		HardwareId: "hardware-1",
		ExpiresAt:  time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
	})
	license.DjangoBaseURL = "http://127.0.0.1:1"

	sourceDatabase := openTestDatabase(t, filepath.Join(testDirectory, "source.db"), "sale while offline")
	_, uploadError := UploadCloudBackup(sourceDatabase)
	if !errors.Is(uploadError, ErrCloudUnreachable) {
		t.Fatalf("expected an unreachable-cloud error, got %v", uploadError)
	}

	backupStatus, statusError := GetBackupStatus(filepath.Join(testDirectory, "balce.db"))
	if statusError != nil {
		t.Fatal(statusError)
	}
	if !backupStatus.CloudAvailable {
		t.Fatal("a paid license should make cloud available")
	}
	if backupStatus.LastCloudError != ErrCloudUnreachable.Error() {
		t.Fatalf("status shows %q", backupStatus.LastCloudError)
	}
	if backupStatus.LastCloudSuccessAt != nil {
		t.Fatal("a failed upload must not count as a success")
	}
}
