package backup

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net/http"
	"os"
	"sync"
	"time"

	"github.com/chrisostomemataba/balceinv-api/license"
)

const (
	licensingRequestTimeout = 20 * time.Second
	cloudTransferTimeout    = 10 * time.Minute
	trialLicenseKey         = "trial"
)

var (
	ErrNoPaidLicense    = errors.New("cloud backup needs an activated license")
	ErrCloudUnreachable = errors.New("could not reach the cloud, check the internet connection")
	ErrNoCloudBackup    = errors.New("there is no cloud backup for that date")
)

type CloudBackup struct {
	Date        string `json:"date"`
	Key         string `json:"key"`
	Size        int64  `json:"size"`
	DownloadURL string `json:"download_url"`
}

type CloudBackupView struct {
	Date string `json:"date"`
	Key  string `json:"key"`
	Size int64  `json:"size"`
}

type cloudStatus struct {
	mutex         sync.Mutex
	lastAttemptAt *time.Time
	lastSuccessAt *time.Time
	lastError     string
}

func UserFacingCloudError(cloudError error) string {
	if errors.Is(cloudError, ErrCloudUnreachable) {
		return ErrCloudUnreachable.Error()
	}
	return cloudError.Error()
}

func loadPaidLicenseState() (*license.LicenseState, error) {
	licenseState, loadError := license.LoadLicenseState()
	if loadError != nil {
		return nil, ErrNoPaidLicense
	}
	isTrial := licenseState.IsTrial || licenseState.LicenseKey == trialLicenseKey
	if isTrial {
		return nil, ErrNoPaidLicense
	}
	return licenseState, nil
}

func postToLicensingServer(ctx context.Context, licensingPath string, licenseState *license.LicenseState, answer any) error {
	payloadBytes, marshalError := json.Marshal(map[string]string{
		"license_key": licenseState.LicenseKey,
		"hardware_id": licenseState.HardwareId,
	})
	if marshalError != nil {
		return marshalError
	}

	licensingRequest, requestError := http.NewRequestWithContext(ctx, http.MethodPost, license.DjangoBaseURL+licensingPath, bytes.NewReader(payloadBytes))
	if requestError != nil {
		return requestError
	}
	licensingRequest.Header.Set("Content-Type", "application/json")

	licensingClient := &http.Client{Timeout: licensingRequestTimeout}
	licensingResponse, sendError := licensingClient.Do(licensingRequest)
	if sendError != nil {
		return fmt.Errorf("%w: licensing server: %v", ErrCloudUnreachable, sendError)
	}
	defer licensingResponse.Body.Close()

	responseBytes, readError := io.ReadAll(io.LimitReader(licensingResponse.Body, 1<<20))
	if readError != nil {
		return readError
	}
	if licensingResponse.StatusCode != http.StatusOK {
		errorAnswer := struct {
			Error string `json:"error"`
		}{}
		json.Unmarshal(responseBytes, &errorAnswer)
		return fmt.Errorf("the licensing server answered %d: %s", licensingResponse.StatusCode, errorAnswer.Error)
	}
	return json.Unmarshal(responseBytes, answer)
}

func putFileToUrl(ctx context.Context, uploadUrl string, filePath string) error {
	uploadFile, openError := os.Open(filePath)
	if openError != nil {
		return openError
	}
	defer uploadFile.Close()

	uploadFileInfo, statError := uploadFile.Stat()
	if statError != nil {
		return statError
	}

	uploadRequest, requestError := http.NewRequestWithContext(ctx, http.MethodPut, uploadUrl, uploadFile)
	if requestError != nil {
		return requestError
	}
	uploadRequest.ContentLength = uploadFileInfo.Size()

	transferClient := &http.Client{Timeout: cloudTransferTimeout}
	uploadResponse, sendError := transferClient.Do(uploadRequest)
	if sendError != nil {
		return fmt.Errorf("%w: upload: %v", ErrCloudUnreachable, sendError)
	}
	defer uploadResponse.Body.Close()
	if uploadResponse.StatusCode != http.StatusOK {
		return fmt.Errorf("storage answered %d on upload", uploadResponse.StatusCode)
	}
	return nil
}

func (store *Store) UploadCloudBackup(ctx context.Context) (string, error) {
	store.fileMutex.Lock()
	defer store.fileMutex.Unlock()

	backupKey, uploadError := store.uploadCloudSnapshot(ctx)
	store.recordCloudResult(uploadError)
	return backupKey, uploadError
}

func (store *Store) uploadCloudSnapshot(ctx context.Context) (string, error) {
	licenseState, licenseError := loadPaidLicenseState()
	if licenseError != nil {
		return "", licenseError
	}

	directoryError := store.ensureDirectory()
	if directoryError != nil {
		return "", directoryError
	}
	gzipFilePath := store.backupFilePath("cloud-upload")
	defer os.Remove(gzipFilePath)

	snapshotError := store.writeGzippedSnapshot(ctx, gzipFilePath)
	if snapshotError != nil {
		return "", snapshotError
	}

	uploadLink := struct {
		UploadURL string `json:"upload_url"`
		Key       string `json:"key"`
	}{}
	linkError := postToLicensingServer(ctx, "/balce/backup/upload-url/", licenseState, &uploadLink)
	if linkError != nil {
		return "", linkError
	}
	uploadError := putFileToUrl(ctx, uploadLink.UploadURL, gzipFilePath)
	if uploadError != nil {
		return "", uploadError
	}

	slog.Info("cloud backup uploaded", "key", uploadLink.Key)
	return uploadLink.Key, nil
}

func (store *Store) ListCloudBackups(ctx context.Context) ([]CloudBackup, error) {
	licenseState, licenseError := loadPaidLicenseState()
	if licenseError != nil {
		return nil, licenseError
	}

	backupList := struct {
		Data []CloudBackup `json:"data"`
	}{}
	listError := postToLicensingServer(ctx, "/balce/backup/list/", licenseState, &backupList)
	if listError != nil {
		return nil, listError
	}
	if backupList.Data == nil {
		return []CloudBackup{}, nil
	}
	return backupList.Data, nil
}

func (store *Store) StageCloudRestore(ctx context.Context, backupDate string) error {
	cloudBackups, listError := store.ListCloudBackups(ctx)
	if listError != nil {
		return listError
	}

	var chosenBackup *CloudBackup
	for backupIndex := range cloudBackups {
		if cloudBackups[backupIndex].Date == backupDate {
			chosenBackup = &cloudBackups[backupIndex]
			break
		}
	}
	if chosenBackup == nil {
		return ErrNoCloudBackup
	}

	downloadRequest, requestError := http.NewRequestWithContext(ctx, http.MethodGet, chosenBackup.DownloadURL, nil)
	if requestError != nil {
		return requestError
	}
	transferClient := &http.Client{Timeout: cloudTransferTimeout}
	downloadResponse, sendError := transferClient.Do(downloadRequest)
	if sendError != nil {
		return fmt.Errorf("%w: download: %v", ErrCloudUnreachable, sendError)
	}
	defer downloadResponse.Body.Close()
	if downloadResponse.StatusCode != http.StatusOK {
		return fmt.Errorf("storage answered %d on download", downloadResponse.StatusCode)
	}

	return store.StageGzipRestore(ctx, downloadResponse.Body)
}

func (store *Store) recordCloudResult(uploadError error) {
	if errors.Is(uploadError, ErrNoPaidLicense) {
		return
	}
	store.cloud.mutex.Lock()
	defer store.cloud.mutex.Unlock()

	attemptTime := time.Now().UTC()
	store.cloud.lastAttemptAt = &attemptTime
	if uploadError != nil {
		store.cloud.lastError = UserFacingCloudError(uploadError)
		return
	}
	store.cloud.lastSuccessAt = &attemptTime
	store.cloud.lastError = ""
}
