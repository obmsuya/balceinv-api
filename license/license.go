package license

import (
	"bytes"
	"crypto/hmac"
	"crypto/sha256"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"
)

// LicenseSecret is injected at compile time via -ldflags.
// It is never read from a file or environment variable.
var LicenseSecret = ""

const gracePeriodDays = 5
const timestampWriteIntervalMinutes = 30
const djangoBaseURL = "https://backend.wapangaji.com/api/v1/payments"
const appDataDirectoryName = "com.balceinv.app"
const licenseStateFileName = "license.json"
const hardwareIdFileName = "hardware.id"

var licenseStateMutex sync.Mutex

type LicenseState struct {
	LicenseKey    string `json:"license_key"`
	HardwareId    string `json:"hardware_id"`
	ExpiresAt     string `json:"expires_at"`
	MaxDevices    int    `json:"max_devices"`
	DaysGranted   int    `json:"days_granted"`
	Signature     string `json:"signature"`
	LastKnownTime string `json:"last_known_time"`
	IsTrial       bool   `json:"is_trial"`
}

const TrialDurationDays = 14

func IssueTrialLicense() error {
	_, existingLicenseError := LoadLicenseState()
	if existingLicenseError == nil {
		return nil
	}

	hardwareIdString, hardwareIdComputeError := ComputeHardwareId()
	if hardwareIdComputeError != nil {
		return hardwareIdComputeError
	}

	trialExpiryTime := time.Now().Add(TrialDurationDays * 24 * time.Hour)

	trialLicenseState := &LicenseState{
		LicenseKey:    "trial",
		HardwareId:    hardwareIdString,
		ExpiresAt:     trialExpiryTime.UTC().Format(time.RFC3339),
		MaxDevices:    1,
		DaysGranted:   TrialDurationDays,
		LastKnownTime: time.Now().UTC().Format(time.RFC3339),
		IsTrial:       true,
	}

	return SaveLicenseState(trialLicenseState)
}

// GetAppDataDirectory returns the OS-appropriate directory for storing the
// license state file. It creates the directory if it does not exist.
func GetAppDataDirectory() (string, error) {
	var basePath string

	switch runtime.GOOS {
	case "windows":
		basePath = os.Getenv("APPDATA")
	case "darwin":
		basePath = filepath.Join(os.Getenv("HOME"), "Library", "Application Support")
	default:
		basePath = os.Getenv("XDG_CONFIG_HOME")
		if basePath == "" {
			basePath = filepath.Join(os.Getenv("HOME"), ".config")
		}
	}

	fullDirectoryPath := filepath.Join(basePath, appDataDirectoryName)
	appDataDirectoryCreateError := os.MkdirAll(fullDirectoryPath, 0700)
	if appDataDirectoryCreateError != nil {
		return "", appDataDirectoryCreateError
	}

	return fullDirectoryPath, nil
}

// GetLicenseFilePath returns the full path to the license state JSON file.
func GetLicenseFilePath() (string, error) {
	directoryPath, appDataDirectoryError := GetAppDataDirectory()
	if appDataDirectoryError != nil {
		return "", appDataDirectoryError
	}
	return filepath.Join(directoryPath, licenseStateFileName), nil
}

// GetHardwareIdFilePath returns the full path to the hardware ID file.
func GetHardwareIdFilePath() (string, error) {
	directoryPath, appDataDirectoryError := GetAppDataDirectory()
	if appDataDirectoryError != nil {
		return "", appDataDirectoryError
	}
	return filepath.Join(directoryPath, hardwareIdFileName), nil
}

// ComputeHardwareId generates a stable hardware fingerprint for the current
// machine using OS-specific identifiers. Falls back to a stored random value
// if the OS method fails.
func ComputeHardwareId() (string, error) {
	var rawIdentifierString string

	switch runtime.GOOS {
	case "windows":
		registryQueryCommand := exec.Command("reg", "query", "HKEY_LOCAL_MACHINE\\SOFTWARE\\Microsoft\\Cryptography", "/v", "MachineGuid")
		registryQueryOutputBytes, registryQueryError := registryQueryCommand.Output()
		if registryQueryError == nil {
			rawIdentifierString = string(registryQueryOutputBytes)
		}

	case "darwin":
		hardwareProfileCommand := exec.Command("system_profiler", "SPHardwareDataType")
		hardwareProfileBytes, hardwareProfileError := hardwareProfileCommand.Output()
		if hardwareProfileError == nil {
			rawIdentifierString = string(hardwareProfileBytes)
		}

	default:
		machineIdFileBytes, machineIdFileReadError := os.ReadFile("/etc/machine-id")
		if machineIdFileReadError == nil {
			rawIdentifierString = strings.TrimSpace(string(machineIdFileBytes))
		}
	}

	if rawIdentifierString == "" {
		hardwareIdFilePath, hardwareIdFilePathError := GetHardwareIdFilePath()
		if hardwareIdFilePathError != nil {
			return "", hardwareIdFilePathError
		}

		existingHardwareIdBytes, existingHardwareIdReadError := os.ReadFile(hardwareIdFilePath)
		if existingHardwareIdReadError == nil && len(existingHardwareIdBytes) > 0 {
			return strings.TrimSpace(string(existingHardwareIdBytes)), nil
		}

		randomHashBytes := sha256.Sum256([]byte(fmt.Sprintf("%d-%s", time.Now().UnixNano(), "balce-hw")))
		randomHardwareIdString := hex.EncodeToString(randomHashBytes[:])

		hardwareIdFileWriteError := os.WriteFile(hardwareIdFilePath, []byte(randomHardwareIdString), 0600)
		if hardwareIdFileWriteError != nil {
			log.Printf("failed to write hardware id file: %v", hardwareIdFileWriteError)
		}

		return randomHardwareIdString, nil
	}

	identifierHashBytes := sha256.Sum256([]byte(rawIdentifierString))
	hardwareIdString := hex.EncodeToString(identifierHashBytes[:])
	return hardwareIdString, nil
}

// ComputeSignature creates an HMAC-SHA256 hex string of the license data fields.
// The signature covers license_key, expires_at, max_devices, days_granted in that order.
func ComputeSignature(licenseKeyString string, expiresAtString string, maxDevices int, daysGranted int) string {
	signaturePayloadString := fmt.Sprintf("%s|%s|%d|%d", licenseKeyString, expiresAtString, maxDevices, daysGranted)
	hmacWriter := hmac.New(sha256.New, []byte(LicenseSecret))
	hmacWriter.Write([]byte(signaturePayloadString))
	return hex.EncodeToString(hmacWriter.Sum(nil))
}

// LoadLicenseState reads the license state file from the OS app data directory,
// verifies its HMAC-SHA256 signature, and returns the parsed state object.
func LoadLicenseState() (*LicenseState, error) {
	licenseFilePath, licenseFilePathError := GetLicenseFilePath()
	if licenseFilePathError != nil {
		return nil, licenseFilePathError
	}

	licenseFileBytes, licenseFileReadError := os.ReadFile(licenseFilePath)
	licenseFileIsMissing := errors.Is(licenseFileReadError, os.ErrNotExist)
	if licenseFileIsMissing {
		return nil, errors.New("no license file found")
	}
	if licenseFileReadError != nil {
		return nil, licenseFileReadError
	}

	var licenseStateObject LicenseState
	licenseStateJsonUnmarshalError := json.Unmarshal(licenseFileBytes, &licenseStateObject)
	if licenseStateJsonUnmarshalError != nil {
		return nil, licenseStateJsonUnmarshalError
	}

	expectedSignatureHex := ComputeSignature(licenseStateObject.LicenseKey, licenseStateObject.ExpiresAt, licenseStateObject.MaxDevices, licenseStateObject.DaysGranted)
	signatureIsInvalid := !hmac.Equal([]byte(expectedSignatureHex), []byte(licenseStateObject.Signature))
	if signatureIsInvalid {
		return nil, errors.New("license signature is invalid")
	}

	return &licenseStateObject, nil
}

// SaveLicenseState writes the license state object to the license file and
// computes a fresh signature before writing.
func SaveLicenseState(licenseStateObject *LicenseState) error {
	licenseStateMutex.Lock()
	defer licenseStateMutex.Unlock()

	licenseStateObject.Signature = ComputeSignature(licenseStateObject.LicenseKey, licenseStateObject.ExpiresAt, licenseStateObject.MaxDevices, licenseStateObject.DaysGranted)

	licenseStateJsonBytes, licenseStateJsonMarshalError := json.MarshalIndent(licenseStateObject, "", "  ")
	if licenseStateJsonMarshalError != nil {
		return licenseStateJsonMarshalError
	}

	licenseFilePath, licenseFilePathError := GetLicenseFilePath()
	if licenseFilePathError != nil {
		return licenseFilePathError
	}

	licenseFileWriteError := os.WriteFile(licenseFilePath, licenseStateJsonBytes, 0600)
	return licenseFileWriteError
}

// Check validates the local license state. It returns nil if the license is
// valid or within the grace period. It returns an error describing the failure
// if the license is missing, tampered, clock-rolled-back, or hard-expired.
func Check() error {
	licenseStateObject, licenseLoadError := LoadLicenseState()
	if licenseLoadError != nil {
		return licenseLoadError
	}

	lastKnownTime, lastKnownTimeParseError := time.Parse(time.RFC3339, licenseStateObject.LastKnownTime)
	if lastKnownTimeParseError != nil {
		lastKnownTime = time.Time{}
	}

	currentSystemTime := time.Now()

	lastKnownTimeIsSet := !lastKnownTime.IsZero()
	clockWasRolledBack := lastKnownTimeIsSet && currentSystemTime.Before(lastKnownTime)
	if clockWasRolledBack {
		return errors.New("system clock tampered")
	}

	expiryTime, expiryTimeParseError := time.Parse(time.RFC3339, licenseStateObject.ExpiresAt)
	if expiryTimeParseError != nil {
		return expiryTimeParseError
	}

	gracePeriodDeadlineTime := expiryTime.Add(time.Duration(gracePeriodDays) * 24 * time.Hour)
	gracePeriodHasExpired := currentSystemTime.After(gracePeriodDeadlineTime)
	if gracePeriodHasExpired {
		return fmt.Errorf("license expired on %s", expiryTime.Format("2 Jan 2006"))
	}

	return nil
}

// UpdateLastKnownTime reads the current license state, sets LastKnownTime to
// now, saves the file, and returns. It is called by the background goroutine.
func UpdateLastKnownTime() error {
	licenseStateObject, licenseLoadError := LoadLicenseState()
	if licenseLoadError != nil {
		return licenseLoadError
	}
	licenseStateObject.LastKnownTime = time.Now().UTC().Format(time.RFC3339)
	return SaveLicenseState(licenseStateObject)
}

// StartTimestampWriter launches a background goroutine that writes the current
// time to the license state file every 30 minutes. This enables clock rollback
// detection on the next startup. It runs for the lifetime of the process.
func StartTimestampWriter() {
	go func() {
		timestampWriteTicker := time.NewTicker(time.Duration(timestampWriteIntervalMinutes) * time.Minute)
		defer timestampWriteTicker.Stop()

		for range timestampWriteTicker.C {
			timestampUpdateError := UpdateLastKnownTime()
			if timestampUpdateError != nil {
				log.Printf("license timestamp update failed: %v", timestampUpdateError)
			}
		}
	}()
}

// SyncWithDjango calls the Django license verify endpoint with the local
// license key and hardware ID. If the response is valid and the signature
// checks out, it overwrites the local license state file with the fresh data.
// It fails silently if the network is unavailable.
func SyncWithDjango() {
	licenseStateObject, licenseLoadError := LoadLicenseState()
	if licenseLoadError != nil {
		log.Printf("license sync skipped: cannot load local state: %v", licenseLoadError)
		return
	}

	syncRequestPayloadMap := map[string]string{"license_key": licenseStateObject.LicenseKey, "hardware_id": licenseStateObject.HardwareId}
	syncRequestPayloadBytes, syncPayloadMarshalError := json.Marshal(syncRequestPayloadMap)
	if syncPayloadMarshalError != nil {
		log.Printf("license sync failed: cannot marshal payload: %v", syncPayloadMarshalError)
		return
	}

	djangoVerifyURL := djangoBaseURL + "/balce/license/verify/"
	djangoHttpRequest, djangoHttpRequestBuildError := http.NewRequest(http.MethodPost, djangoVerifyURL, bytes.NewReader(syncRequestPayloadBytes))
	if djangoHttpRequestBuildError != nil {
		log.Printf("license sync failed: cannot build request: %v", djangoHttpRequestBuildError)
		return
	}
	djangoHttpRequest.Header.Set("Content-Type", "application/json")

	httpClientObject := &http.Client{Timeout: 15 * time.Second}
	djangoHttpResponse, djangoHttpNetworkError := httpClientObject.Do(djangoHttpRequest)
	if djangoHttpNetworkError != nil {
		log.Printf("license sync skipped: network unavailable: %v", djangoHttpNetworkError)
		return
	}
	defer djangoHttpResponse.Body.Close()

	djangoResponseBodyBytes, djangoResponseBodyReadError := io.ReadAll(djangoHttpResponse.Body)
	if djangoResponseBodyReadError != nil {
		log.Printf("license sync failed: cannot read response body: %v", djangoResponseBodyReadError)
		return
	}

	djangoResponseWasSuccessful := djangoHttpResponse.StatusCode == http.StatusOK
	if !djangoResponseWasSuccessful {
		log.Printf("license sync failed: django returned status %d", djangoHttpResponse.StatusCode)
		return
	}

	var djangoSyncResponseObject struct {
		Success     bool   `json:"success"`
		LicenseKey  string `json:"license_key"`
		LicenseData struct {
			ExpiresAt   string `json:"expires_at"`
			MaxDevices  int    `json:"max_devices"`
			DaysGranted int    `json:"days_granted"`
		} `json:"license_data"`
		Signature string `json:"signature"`
	}
	djangoSyncResponseUnmarshalError := json.Unmarshal(djangoResponseBodyBytes, &djangoSyncResponseObject)
	if djangoSyncResponseUnmarshalError != nil {
		log.Printf("license sync failed: cannot parse response: %v", djangoSyncResponseUnmarshalError)
		return
	}

	djangoReportedSuccess := djangoSyncResponseObject.Success
	if !djangoReportedSuccess {
		log.Printf("license sync failed: django reported failure")
		return
	}

	expectedSignatureHex := ComputeSignature(djangoSyncResponseObject.LicenseKey, djangoSyncResponseObject.LicenseData.ExpiresAt, djangoSyncResponseObject.LicenseData.MaxDevices, djangoSyncResponseObject.LicenseData.DaysGranted)
	djangoSignatureIsValid := hmac.Equal([]byte(expectedSignatureHex), []byte(djangoSyncResponseObject.Signature))
	if !djangoSignatureIsValid {
		log.Printf("license sync aborted: Django returned invalid signature")
		return
	}

	updatedLicenseStateObject := &LicenseState{
		LicenseKey:    djangoSyncResponseObject.LicenseKey,
		HardwareId:    licenseStateObject.HardwareId,
		ExpiresAt:     djangoSyncResponseObject.LicenseData.ExpiresAt,
		MaxDevices:    djangoSyncResponseObject.LicenseData.MaxDevices,
		DaysGranted:   djangoSyncResponseObject.LicenseData.DaysGranted,
		LastKnownTime: time.Now().UTC().Format(time.RFC3339),
	}

	licenseStateSaveError := SaveLicenseState(updatedLicenseStateObject)
	if licenseStateSaveError != nil {
		log.Printf("license sync: failed to save updated state: %v", licenseStateSaveError)
		return
	}

	log.Printf("license sync successful: expires %s", djangoSyncResponseObject.LicenseData.ExpiresAt)
}

func ActivateFromDjango() error {
	hardwareIdString, hardwareIdComputeError := ComputeHardwareId()
	if hardwareIdComputeError != nil {
		return hardwareIdComputeError
	}

	activateURL := djangoBaseURL + "/balce/license/by-hardware/" + hardwareIdString + "/"
	httpClientObject := &http.Client{Timeout: 10 * time.Second}

	djangoHttpResponse, djangoHttpNetworkError := httpClientObject.Get(activateURL)
	if djangoHttpNetworkError != nil {
		return fmt.Errorf("licensing server unreachable: %w", djangoHttpNetworkError)
	}
	defer djangoHttpResponse.Body.Close()

	if djangoHttpResponse.StatusCode != http.StatusOK {
		return errors.New("no license found for this device yet")
	}

	djangoResponseBodyBytes, djangoResponseBodyReadError := io.ReadAll(djangoHttpResponse.Body)
	if djangoResponseBodyReadError != nil {
		return djangoResponseBodyReadError
	}

	var djangoActivateResponseObject struct {
		Success     bool   `json:"success"`
		LicenseKey  string `json:"license_key"`
		LicenseData struct {
			ExpiresAt   string `json:"expires_at"`
			MaxDevices  int    `json:"max_devices"`
			DaysGranted int    `json:"days_granted"`
		} `json:"license_data"`
		Signature string `json:"signature"`
	}
	if err := json.Unmarshal(djangoResponseBodyBytes, &djangoActivateResponseObject); err != nil {
		return err
	}
	if !djangoActivateResponseObject.Success {
		return errors.New("django reported failure")
	}

	expectedSignatureHex := ComputeSignature(djangoActivateResponseObject.LicenseKey, djangoActivateResponseObject.LicenseData.ExpiresAt, djangoActivateResponseObject.LicenseData.MaxDevices, djangoActivateResponseObject.LicenseData.DaysGranted)
	if !hmac.Equal([]byte(expectedSignatureHex), []byte(djangoActivateResponseObject.Signature)) {
		return errors.New("django returned invalid signature")
	}

	newLicenseStateObject := &LicenseState{
		LicenseKey:    djangoActivateResponseObject.LicenseKey,
		HardwareId:    hardwareIdString,
		ExpiresAt:     djangoActivateResponseObject.LicenseData.ExpiresAt,
		MaxDevices:    djangoActivateResponseObject.LicenseData.MaxDevices,
		DaysGranted:   djangoActivateResponseObject.LicenseData.DaysGranted,
		LastKnownTime: time.Now().UTC().Format(time.RFC3339),
	}

	return SaveLicenseState(newLicenseStateObject)
}

// IsInGracePeriod returns true if the license is expired but still within the
// 5-day grace period. Returns false if the license is valid or hard-expired.
func IsInGracePeriod() bool {
	licenseStateObject, licenseLoadError := LoadLicenseState()
	if licenseLoadError != nil {
		return false
	}

	expiryTime, expiryTimeParseError := time.Parse(time.RFC3339, licenseStateObject.ExpiresAt)
	if expiryTimeParseError != nil {
		return false
	}

	currentSystemTime := time.Now()
	licenseIsExpired := currentSystemTime.After(expiryTime)
	gracePeriodDeadlineTime := expiryTime.Add(time.Duration(gracePeriodDays) * 24 * time.Hour)
	isWithinGracePeriod := currentSystemTime.Before(gracePeriodDeadlineTime)

	return licenseIsExpired && isWithinGracePeriod
}

// GetDaysRemaining returns the number of days until the license expires.
// Returns a negative number if already expired.
func GetDaysRemaining() int {
	licenseStateObject, licenseLoadError := LoadLicenseState()
	if licenseLoadError != nil {
		return -999
	}

	expiryTime, expiryTimeParseError := time.Parse(time.RFC3339, licenseStateObject.ExpiresAt)
	if expiryTimeParseError != nil {
		return -999
	}

	durationUntilExpiry := time.Until(expiryTime)
	return int(durationUntilExpiry.Hours() / 24)
}
