package backup

import (
	"context"
	"errors"
	"log/slog"
	"time"
)

const (
	automaticBackupInterval   = 6 * time.Hour
	automaticBackupFirstDelay = 2 * time.Minute
	cloudRetryInterval        = 15 * time.Minute
)

type Status struct {
	CloudAvailable     bool          `json:"cloud_available"`
	LastCloudAttemptAt *time.Time    `json:"last_cloud_attempt_at"`
	LastCloudSuccessAt *time.Time    `json:"last_cloud_success_at"`
	LastCloudError     string        `json:"last_cloud_error"`
	RestorePending     bool          `json:"restore_pending"`
	LocalBackups       []LocalBackup `json:"local_backups"`
	BeforeRestoreCopy  *LocalBackup  `json:"before_restore_copy"`
}

func (store *Store) Status() (Status, error) {
	localBackups, listError := store.ListLocalBackups()
	if listError != nil {
		return Status{}, listError
	}
	_, licenseError := loadPaidLicenseState()

	store.cloud.mutex.Lock()
	defer store.cloud.mutex.Unlock()

	backupStatus := Status{
		CloudAvailable:     licenseError == nil,
		LastCloudAttemptAt: store.cloud.lastAttemptAt,
		LastCloudSuccessAt: store.cloud.lastSuccessAt,
		LastCloudError:     store.cloud.lastError,
		RestorePending:     store.IsRestorePending(),
		LocalBackups:       localBackups,
		BeforeRestoreCopy:  store.BeforeRestoreCopy(),
	}
	return backupStatus, nil
}

func (store *Store) StartAutomaticBackups(ctx context.Context) {
	go func() {
		select {
		case <-ctx.Done():
			return
		case <-time.After(automaticBackupFirstDelay):
		}

		for {
			nextBackupAt := time.Now().Add(automaticBackupInterval)

			_, localError := store.WriteLocalBackup(ctx)
			if localError != nil {
				slog.Error("automatic local backup failed", "error", localError)
			}

			_, uploadError := store.UploadCloudBackup(ctx)
			for shouldRetryCloud(uploadError) && time.Now().Add(cloudRetryInterval).Before(nextBackupAt) {
				slog.Warn("automatic cloud backup failed, retrying", "error", uploadError)
				select {
				case <-ctx.Done():
					return
				case <-time.After(cloudRetryInterval):
				}
				_, uploadError = store.UploadCloudBackup(ctx)
			}

			select {
			case <-ctx.Done():
				return
			case <-time.After(time.Until(nextBackupAt)):
			}
		}
	}()
}

func shouldRetryCloud(uploadError error) bool {
	return uploadError != nil && !errors.Is(uploadError, ErrNoPaidLicense)
}
