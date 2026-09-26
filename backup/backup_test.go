package backup

import (
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chrisostomemataba/balceinv-api/license"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
)

func openTestDatabase(t *testing.T, databasePath string, noteText string) *gorm.DB {
	testDatabase, openError := gorm.Open(sqlite.Open(databasePath), &gorm.Config{})
	if openError != nil {
		t.Fatal(openError)
	}
	testDatabase.Exec("CREATE TABLE IF NOT EXISTS notes (text TEXT)")
	if noteText != "" {
		testDatabase.Exec("INSERT INTO notes (text) VALUES (?)", noteText)
	}
	return testDatabase
}

func TestUploadListRestoreRoundTrip(t *testing.T) {
	testDirectory := t.TempDir()
	t.Setenv("HOME", testDirectory)
	t.Setenv("XDG_CONFIG_HOME", testDirectory)
	t.Setenv("APPDATA", testDirectory)
	license.LicenseSecret = "test-secret"
	license.SaveLicenseState(&license.LicenseState{
		LicenseKey: "paid-key",
		HardwareId: "hardware-1",
		ExpiresAt:  time.Now().Add(24 * time.Hour).UTC().Format(time.RFC3339),
	})

	var storedBackupBytes []byte
	fakeServer := httptest.NewServer(nil)
	defer fakeServer.Close()
	fakeServer.Config.Handler = http.HandlerFunc(func(responseWriter http.ResponseWriter, request *http.Request) {
		switch request.URL.Path {
		case "/balce/backup/upload-url/":
			json.NewEncoder(responseWriter).Encode(map[string]string{"upload_url": fakeServer.URL + "/r2/object", "key": "backups/hardware-1/2026-09-26.db.gz"})
		case "/balce/backup/list/":
			json.NewEncoder(responseWriter).Encode(map[string]interface{}{"data": []map[string]interface{}{
				{"date": "2026-09-26", "key": "backups/hardware-1/2026-09-26.db.gz", "size": len(storedBackupBytes), "download_url": fakeServer.URL + "/r2/object"},
			}})
		case "/r2/object":
			if request.Method == http.MethodPut {
				if request.ContentLength <= 0 {
					t.Error("upload was sent without Content-Length")
				}
				storedBackupBytes, _ = io.ReadAll(request.Body)
				return
			}
			responseWriter.Write(storedBackupBytes)
		}
	})
	license.DjangoBaseURL = fakeServer.URL

	sourceDatabase := openTestDatabase(t, filepath.Join(testDirectory, "source.db"), "sold 3 sodas")
	_, uploadError := UploadCloudBackup(sourceDatabase)
	if uploadError != nil {
		t.Fatal(uploadError)
	}

	targetDatabasePath := filepath.Join(testDirectory, "balce.db")
	targetDatabase := openTestDatabase(t, targetDatabasePath, "")
	targetSqlDatabase, _ := targetDatabase.DB()
	targetSqlDatabase.Close()

	restoreError := StageCloudRestore(targetDatabasePath, "2026-09-26")
	if restoreError != nil {
		t.Fatal(restoreError)
	}
	applyError := ApplyPendingRestore(targetDatabasePath)
	if applyError != nil {
		t.Fatal(applyError)
	}

	restoredDatabase := openTestDatabase(t, targetDatabasePath, "")
	var restoredNote string
	restoredDatabase.Raw("SELECT text FROM notes").Scan(&restoredNote)
	if restoredNote != "sold 3 sodas" {
		t.Fatalf("restored database has %q", restoredNote)
	}
	_, previousStatError := os.Stat(targetDatabasePath + ".before-restore")
	if previousStatError != nil {
		t.Fatal("previous database was not kept")
	}
	if StageCloudRestore(targetDatabasePath, "1999-01-01") == nil {
		t.Fatal("restoring a date with no backup should fail")
	}
}
