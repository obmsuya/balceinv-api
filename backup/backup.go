package backup

import (
	"bytes"
	"compress/gzip"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/chrisostomemataba/balceinv-api/license"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

const djangoRequestTimeoutSeconds = 20
const cloudTransferTimeoutMinutes = 10
const automaticBackupIntervalHours = 6
const automaticBackupFirstDelayMinutes = 2
const cloudRetryIntervalMinutes = 15
const trialLicenseKey = "trial"
const sqliteFileHeader = "SQLite format 3\x00"

var cloudBackupMutex sync.Mutex

var ErrNoPaidLicense = errors.New("cloud backup needs an activated license")

type CloudBackup struct {
	Date        string `json:"date"`
	Key         string `json:"key"`
	Size        int64  `json:"size"`
	DownloadURL string `json:"download_url"`
}

type djangoErrorResponse struct {
	Error string `json:"error"`
}

// PendingRestorePath is where a downloaded backup waits until the next start,
// because the live database cannot be swapped while the app holds it open.
func PendingRestorePath(databasePath string) string {
	return databasePath + ".restore"
}

func loadPaidLicenseState() (*license.LicenseState, error) {
	licenseStateObject, licenseLoadError := license.LoadLicenseState()
	if licenseLoadError != nil {
		return nil, ErrNoPaidLicense
	}
	licenseIsTrial := licenseStateObject.IsTrial || licenseStateObject.LicenseKey == trialLicenseKey
	if licenseIsTrial {
		return nil, ErrNoPaidLicense
	}
	return licenseStateObject, nil
}

func postToDjango(djangoPath string, licenseStateObject *license.LicenseState, responseObject interface{}) error {
	requestPayloadBytes, requestPayloadMarshalError := json.Marshal(map[string]string{
		"license_key": licenseStateObject.LicenseKey,
		"hardware_id": licenseStateObject.HardwareId,
	})
	if requestPayloadMarshalError != nil {
		return requestPayloadMarshalError
	}

	djangoURL := license.DjangoBaseURL + djangoPath
	httpClientObject := &http.Client{Timeout: djangoRequestTimeoutSeconds * time.Second}
	djangoHttpResponse, djangoHttpNetworkError := httpClientObject.Post(djangoURL, "application/json", bytes.NewReader(requestPayloadBytes))
	if djangoHttpNetworkError != nil {
		return fmt.Errorf("licensing server unreachable: %w", djangoHttpNetworkError)
	}
	defer djangoHttpResponse.Body.Close()

	djangoResponseBodyBytes, djangoResponseBodyReadError := io.ReadAll(djangoHttpResponse.Body)
	if djangoResponseBodyReadError != nil {
		return djangoResponseBodyReadError
	}

	djangoResponseWasSuccessful := djangoHttpResponse.StatusCode == http.StatusOK
	if !djangoResponseWasSuccessful {
		var djangoErrorObject djangoErrorResponse
		json.Unmarshal(djangoResponseBodyBytes, &djangoErrorObject)
		return fmt.Errorf("licensing server returned %d: %s", djangoHttpResponse.StatusCode, djangoErrorObject.Error)
	}

	return json.Unmarshal(djangoResponseBodyBytes, responseObject)
}

func writeGzippedSnapshot(database *gorm.DB, gzipFilePath string) error {
	snapshotFilePath := gzipFilePath + ".snapshot"
	os.Remove(snapshotFilePath)
	defer os.Remove(snapshotFilePath)

	// VACUUM INTO gives a consistent copy even while sales are being written.
	snapshotError := database.Exec("VACUUM INTO ?", snapshotFilePath).Error
	if snapshotError != nil {
		return fmt.Errorf("snapshot failed: %w", snapshotError)
	}

	snapshotFile, snapshotOpenError := os.Open(snapshotFilePath)
	if snapshotOpenError != nil {
		return snapshotOpenError
	}
	defer snapshotFile.Close()

	gzipFile, gzipCreateError := os.Create(gzipFilePath)
	if gzipCreateError != nil {
		return gzipCreateError
	}
	defer gzipFile.Close()

	gzipWriter := gzip.NewWriter(gzipFile)
	_, gzipCopyError := io.Copy(gzipWriter, snapshotFile)
	if gzipCopyError != nil {
		return gzipCopyError
	}
	return gzipWriter.Close()
}

func putFileToURL(uploadURL string, filePath string) error {
	uploadFile, uploadFileOpenError := os.Open(filePath)
	if uploadFileOpenError != nil {
		return uploadFileOpenError
	}
	defer uploadFile.Close()

	uploadFileInfo, uploadFileStatError := uploadFile.Stat()
	if uploadFileStatError != nil {
		return uploadFileStatError
	}

	uploadRequest, uploadRequestBuildError := http.NewRequest(http.MethodPut, uploadURL, uploadFile)
	if uploadRequestBuildError != nil {
		return uploadRequestBuildError
	}
	// R2 refuses chunked uploads on a signed link, so the length must be sent up front.
	uploadRequest.ContentLength = uploadFileInfo.Size()

	httpClientObject := &http.Client{Timeout: cloudTransferTimeoutMinutes * time.Minute}
	uploadResponse, uploadNetworkError := httpClientObject.Do(uploadRequest)
	if uploadNetworkError != nil {
		return fmt.Errorf("upload failed: %w", uploadNetworkError)
	}
	defer uploadResponse.Body.Close()

	uploadWasSuccessful := uploadResponse.StatusCode == http.StatusOK
	if !uploadWasSuccessful {
		return fmt.Errorf("storage returned %d on upload", uploadResponse.StatusCode)
	}
	return nil
}

// UploadCloudBackup snapshots the database, gzips it and uploads it as today's backup.
func UploadCloudBackup(database *gorm.DB) (string, error) {
	cloudBackupMutex.Lock()
	defer cloudBackupMutex.Unlock()

	backupKey, uploadError := uploadCloudSnapshot(database)
	recordCloudBackupResult(uploadError)
	return backupKey, uploadError
}

func uploadCloudSnapshot(database *gorm.DB) (string, error) {
	licenseStateObject, licenseError := loadPaidLicenseState()
	if licenseError != nil {
		return "", licenseError
	}

	appDataDirectory, appDataDirectoryError := license.GetAppDataDirectory()
	if appDataDirectoryError != nil {
		return "", appDataDirectoryError
	}
	gzipFilePath := appDataDirectory + string(os.PathSeparator) + "cloud-backup.db.gz"
	defer os.Remove(gzipFilePath)

	snapshotError := writeGzippedSnapshot(database, gzipFilePath)
	if snapshotError != nil {
		return "", snapshotError
	}

	var uploadLinkObject struct {
		UploadURL string `json:"upload_url"`
		Key       string `json:"key"`
	}
	uploadLinkError := postToDjango("/balce/backup/upload-url/", licenseStateObject, &uploadLinkObject)
	if uploadLinkError != nil {
		return "", uploadLinkError
	}

	uploadError := putFileToURL(uploadLinkObject.UploadURL, gzipFilePath)
	if uploadError != nil {
		return "", uploadError
	}

	log.Printf("cloud backup uploaded key=%s", uploadLinkObject.Key)
	return uploadLinkObject.Key, nil
}

// ListCloudBackups returns this device's backups, newest first.
func ListCloudBackups() ([]CloudBackup, error) {
	licenseStateObject, licenseError := loadPaidLicenseState()
	if licenseError != nil {
		return nil, licenseError
	}

	var backupListObject struct {
		Data []CloudBackup `json:"data"`
	}
	backupListError := postToDjango("/balce/backup/list/", licenseStateObject, &backupListObject)
	if backupListError != nil {
		return nil, backupListError
	}
	cloudBackups := backupListObject.Data
	return cloudBackups, nil
}

func unzipToFile(gzipSource io.Reader, targetFilePath string) error {
	gzipReader, gzipReaderError := gzip.NewReader(gzipSource)
	if gzipReaderError != nil {
		return fmt.Errorf("backup is not a valid gzip file: %w", gzipReaderError)
	}
	defer gzipReader.Close()

	targetFile, targetCreateError := os.Create(targetFilePath)
	if targetCreateError != nil {
		return targetCreateError
	}
	defer targetFile.Close()

	_, unzipCopyError := io.Copy(targetFile, gzipReader)
	return unzipCopyError
}

func stageRestoreFromGzip(databasePath string, gzipSource io.Reader) error {
	pendingRestorePath := PendingRestorePath(databasePath)
	downloadingFilePath := pendingRestorePath + ".downloading"
	defer os.Remove(downloadingFilePath)

	unzipError := unzipToFile(gzipSource, downloadingFilePath)
	if unzipError != nil {
		return unzipError
	}

	sqliteCheckError := checkSqliteFile(downloadingFilePath)
	if sqliteCheckError != nil {
		return sqliteCheckError
	}

	return os.Rename(downloadingFilePath, pendingRestorePath)
}

func checkSqliteFile(databaseFilePath string) error {
	headerBytes := make([]byte, len(sqliteFileHeader))
	databaseFile, databaseOpenError := os.Open(databaseFilePath)
	if databaseOpenError != nil {
		return databaseOpenError
	}
	_, headerReadError := io.ReadFull(databaseFile, headerBytes)
	databaseFile.Close()
	fileHasSqliteHeader := headerReadError == nil && string(headerBytes) == sqliteFileHeader
	if !fileHasSqliteHeader {
		return errors.New("backup is not a Balce database")
	}

	checkDatabase, checkOpenError := gorm.Open(sqlite.Open(databaseFilePath), &gorm.Config{})
	if checkOpenError != nil {
		return checkOpenError
	}
	checkSqlDatabase, checkSqlDatabaseError := checkDatabase.DB()
	if checkSqlDatabaseError != nil {
		return checkSqlDatabaseError
	}
	defer checkSqlDatabase.Close()

	var quickCheckResult string
	quickCheckError := checkDatabase.Raw("PRAGMA quick_check").Scan(&quickCheckResult).Error
	if quickCheckError != nil {
		return quickCheckError
	}
	databaseIsHealthy := quickCheckResult == "ok"
	if !databaseIsHealthy {
		return fmt.Errorf("backup database is damaged: %s", quickCheckResult)
	}
	return nil
}

// StageCloudRestore downloads the backup for backupDate and leaves it next to the
// live database; ApplyPendingRestore swaps it in on the next start.
func StageCloudRestore(databasePath string, backupDate string) error {
	cloudBackups, backupListError := ListCloudBackups()
	if backupListError != nil {
		return backupListError
	}

	var chosenBackup *CloudBackup
	for backupIndex := range cloudBackups {
		backupMatchesDate := cloudBackups[backupIndex].Date == backupDate
		if backupMatchesDate {
			chosenBackup = &cloudBackups[backupIndex]
			break
		}
	}
	if chosenBackup == nil {
		return fmt.Errorf("no cloud backup found for %s", backupDate)
	}

	httpClientObject := &http.Client{Timeout: cloudTransferTimeoutMinutes * time.Minute}
	downloadResponse, downloadNetworkError := httpClientObject.Get(chosenBackup.DownloadURL)
	if downloadNetworkError != nil {
		return fmt.Errorf("download failed: %w", downloadNetworkError)
	}
	defer downloadResponse.Body.Close()

	downloadWasSuccessful := downloadResponse.StatusCode == http.StatusOK
	if !downloadWasSuccessful {
		return fmt.Errorf("storage returned %d on download", downloadResponse.StatusCode)
	}

	stageError := stageRestoreFromGzip(databasePath, downloadResponse.Body)
	if stageError != nil {
		return stageError
	}

	log.Printf("cloud restore staged date=%s", backupDate)
	return nil
}

// ApplyPendingRestore runs before the database is opened. The replaced database is
// kept as <db>.before-restore so a wrong restore can still be undone by hand.
func ApplyPendingRestore(databasePath string) error {
	pendingRestorePath := PendingRestorePath(databasePath)
	_, pendingStatError := os.Stat(pendingRestorePath)
	restoreIsPending := pendingStatError == nil
	if !restoreIsPending {
		return nil
	}

	previousDatabasePath := databasePath + ".before-restore"
	for _, fileSuffix := range []string{"", "-wal", "-shm"} {
		os.Remove(previousDatabasePath + fileSuffix)
		moveError := os.Rename(databasePath+fileSuffix, previousDatabasePath+fileSuffix)
		moveFailed := moveError != nil && !errors.Is(moveError, os.ErrNotExist)
		if moveFailed {
			return fmt.Errorf("could not set aside current database: %w", moveError)
		}
	}

	restoreMoveError := os.Rename(pendingRestorePath, databasePath)
	if restoreMoveError != nil {
		return restoreMoveError
	}
	log.Printf("cloud restore applied, previous database kept at %s", previousDatabasePath)
	return nil
}

// StartAutomaticCloudBackup uploads shortly after start, then every few hours.
// Re-uploads on the same day overwrite that day's backup.
func StartAutomaticCloudBackup(database *gorm.DB) {
	go func() {
		time.Sleep(automaticBackupFirstDelayMinutes * time.Minute)

		for {
			nextScheduledBackupAt := time.Now().Add(automaticBackupIntervalHours * time.Hour)

			_, localBackupError := WriteLocalBackup(database)
			if localBackupError != nil {
				log.Printf("automatic local backup failed: %v", localBackupError)
			}

			_, uploadError := UploadCloudBackup(database)
			for cloudUploadShouldRetry(uploadError) && time.Now().Add(cloudRetryIntervalMinutes*time.Minute).Before(nextScheduledBackupAt) {
				log.Printf("automatic cloud backup failed, retrying in %d minutes: %v", cloudRetryIntervalMinutes, uploadError)
				time.Sleep(cloudRetryIntervalMinutes * time.Minute)
				_, uploadError = UploadCloudBackup(database)
			}

			time.Sleep(time.Until(nextScheduledBackupAt))
		}
	}()
}

func cloudUploadShouldRetry(uploadError error) bool {
	licenseIsMissing := errors.Is(uploadError, ErrNoPaidLicense)
	return uploadError != nil && !licenseIsMissing
}
