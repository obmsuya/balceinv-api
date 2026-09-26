package backup

import (
	"errors"
	"os"
	"sync"
	"time"
)

var cloudStatusMutex sync.Mutex
var lastCloudAttemptAt *time.Time
var lastCloudSuccessAt *time.Time
var lastCloudError string

type BackupStatus struct {
	CloudAvailable     bool          `json:"cloud_available"`
	LastCloudAttemptAt *time.Time    `json:"last_cloud_attempt_at"`
	LastCloudSuccessAt *time.Time    `json:"last_cloud_success_at"`
	LastCloudError     string        `json:"last_cloud_error"`
	RestorePending     bool          `json:"restore_pending"`
	LocalBackups       []LocalBackup `json:"local_backups"`
}

func recordCloudBackupResult(uploadError error) {
	licenseIsMissing := errors.Is(uploadError, ErrNoPaidLicense)
	if licenseIsMissing {
		return
	}

	cloudStatusMutex.Lock()
	defer cloudStatusMutex.Unlock()

	attemptTime := time.Now()
	lastCloudAttemptAt = &attemptTime
	if uploadError != nil {
		lastCloudError = uploadError.Error()
		return
	}
	lastCloudSuccessAt = &attemptTime
	lastCloudError = ""
}

func GetBackupStatus(databasePath string) (BackupStatus, error) {
	localBackups, localListError := ListLocalBackups()
	if localListError != nil {
		return BackupStatus{}, localListError
	}

	_, licenseError := loadPaidLicenseState()
	_, pendingStatError := os.Stat(PendingRestorePath(databasePath))

	cloudStatusMutex.Lock()
	defer cloudStatusMutex.Unlock()

	return BackupStatus{
		CloudAvailable:     licenseError == nil,
		LastCloudAttemptAt: lastCloudAttemptAt,
		LastCloudSuccessAt: lastCloudSuccessAt,
		LastCloudError:     lastCloudError,
		RestorePending:     pendingStatError == nil,
		LocalBackups:       localBackups,
	}, nil
}
