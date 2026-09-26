package backup

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/chrisostomemataba/balceinv-api/license"
	"gorm.io/gorm"
)

const localBackupsToKeep = 7
const localBackupFolderName = "backups"
const localBackupFilePrefix = "balce-"
const localBackupFileSuffix = ".db.gz"
const backupDateLayout = "2006-01-02"

var localBackupMutex sync.Mutex

type LocalBackup struct {
	Date      string    `json:"date"`
	FileName  string    `json:"file_name"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
}

func localBackupDirectory() (string, error) {
	appDataDirectory, appDataDirectoryError := license.GetAppDataDirectory()
	if appDataDirectoryError != nil {
		return "", appDataDirectoryError
	}
	backupDirectory := filepath.Join(appDataDirectory, localBackupFolderName)
	directoryCreateError := os.MkdirAll(backupDirectory, 0o755)
	if directoryCreateError != nil {
		return "", fmt.Errorf("could not create backup folder: %w", directoryCreateError)
	}
	return backupDirectory, nil
}

func localBackupFilePath(backupDirectory string, backupDate string) string {
	return filepath.Join(backupDirectory, localBackupFilePrefix+backupDate+localBackupFileSuffix)
}

func localBackupFromFile(backupDirectory string, backupDate string) (LocalBackup, error) {
	backupFilePath := localBackupFilePath(backupDirectory, backupDate)
	backupFileInfo, backupStatError := os.Stat(backupFilePath)
	if backupStatError != nil {
		return LocalBackup{}, backupStatError
	}
	return LocalBackup{
		Date:      backupDate,
		FileName:  filepath.Base(backupFilePath),
		Size:      backupFileInfo.Size(),
		CreatedAt: backupFileInfo.ModTime(),
	}, nil
}

func WriteLocalBackup(database *gorm.DB) (LocalBackup, error) {
	localBackupMutex.Lock()
	defer localBackupMutex.Unlock()

	backupDirectory, backupDirectoryError := localBackupDirectory()
	if backupDirectoryError != nil {
		return LocalBackup{}, backupDirectoryError
	}

	backupDate := time.Now().Format(backupDateLayout)
	backupFilePath := localBackupFilePath(backupDirectory, backupDate)
	writingFilePath := backupFilePath + ".writing"
	defer os.Remove(writingFilePath)

	snapshotError := writeGzippedSnapshot(database, writingFilePath)
	if snapshotError != nil {
		return LocalBackup{}, snapshotError
	}

	renameError := os.Rename(writingFilePath, backupFilePath)
	if renameError != nil {
		return LocalBackup{}, fmt.Errorf("could not save backup file: %w", renameError)
	}

	pruneLocalBackups(backupDirectory)
	return localBackupFromFile(backupDirectory, backupDate)
}

func listLocalBackupDates(backupDirectory string) ([]string, error) {
	directoryEntries, readDirectoryError := os.ReadDir(backupDirectory)
	if readDirectoryError != nil {
		return nil, readDirectoryError
	}

	backupDates := []string{}
	for _, directoryEntry := range directoryEntries {
		entryName := directoryEntry.Name()
		entryIsBackup := strings.HasPrefix(entryName, localBackupFilePrefix) && strings.HasSuffix(entryName, localBackupFileSuffix)
		if !entryIsBackup {
			continue
		}
		backupDate := strings.TrimSuffix(strings.TrimPrefix(entryName, localBackupFilePrefix), localBackupFileSuffix)
		_, dateParseError := time.Parse(backupDateLayout, backupDate)
		if dateParseError != nil {
			continue
		}
		backupDates = append(backupDates, backupDate)
	}

	sort.Sort(sort.Reverse(sort.StringSlice(backupDates)))
	return backupDates, nil
}

func pruneLocalBackups(backupDirectory string) {
	backupDates, listError := listLocalBackupDates(backupDirectory)
	if listError != nil || len(backupDates) <= localBackupsToKeep {
		return
	}
	for _, expiredBackupDate := range backupDates[localBackupsToKeep:] {
		os.Remove(localBackupFilePath(backupDirectory, expiredBackupDate))
	}
}

func ListLocalBackups() ([]LocalBackup, error) {
	backupDirectory, backupDirectoryError := localBackupDirectory()
	if backupDirectoryError != nil {
		return nil, backupDirectoryError
	}

	backupDates, listError := listLocalBackupDates(backupDirectory)
	if listError != nil {
		return nil, listError
	}

	localBackups := []LocalBackup{}
	for _, backupDate := range backupDates {
		localBackup, backupReadError := localBackupFromFile(backupDirectory, backupDate)
		if backupReadError != nil {
			continue
		}
		localBackups = append(localBackups, localBackup)
	}
	return localBackups, nil
}

func StageLocalRestore(databasePath string, backupDate string) error {
	_, dateParseError := time.Parse(backupDateLayout, backupDate)
	if dateParseError != nil {
		return fmt.Errorf("invalid backup date %q", backupDate)
	}

	localBackupMutex.Lock()
	defer localBackupMutex.Unlock()

	backupDirectory, backupDirectoryError := localBackupDirectory()
	if backupDirectoryError != nil {
		return backupDirectoryError
	}

	backupFile, backupOpenError := os.Open(localBackupFilePath(backupDirectory, backupDate))
	if backupOpenError != nil {
		return fmt.Errorf("no backup on this PC for %s", backupDate)
	}
	defer backupFile.Close()

	return stageRestoreFromGzip(databasePath, backupFile)
}

func checkBackupFilePath(backupFilePath string) error {
	pathIsAbsolute := filepath.IsAbs(backupFilePath)
	pathHasBackupExtension := strings.HasSuffix(strings.ToLower(backupFilePath), ".gz")
	if !pathIsAbsolute || !pathHasBackupExtension {
		return fmt.Errorf("backup file must be a full path ending in .gz")
	}
	return nil
}

func ExportBackup(database *gorm.DB, targetFilePath string) error {
	pathCheckError := checkBackupFilePath(targetFilePath)
	if pathCheckError != nil {
		return pathCheckError
	}

	freshBackup, backupWriteError := WriteLocalBackup(database)
	if backupWriteError != nil {
		return backupWriteError
	}

	backupDirectory, backupDirectoryError := localBackupDirectory()
	if backupDirectoryError != nil {
		return backupDirectoryError
	}

	sourceFile, sourceOpenError := os.Open(localBackupFilePath(backupDirectory, freshBackup.Date))
	if sourceOpenError != nil {
		return sourceOpenError
	}
	defer sourceFile.Close()

	targetFile, targetCreateError := os.Create(targetFilePath)
	if targetCreateError != nil {
		return fmt.Errorf("could not write to %s: %w", targetFilePath, targetCreateError)
	}

	_, copyError := io.Copy(targetFile, sourceFile)
	closeError := targetFile.Close()
	if copyError != nil {
		return fmt.Errorf("could not write backup file: %w", copyError)
	}
	if closeError != nil {
		return fmt.Errorf("could not finish backup file: %w", closeError)
	}
	return nil
}

func StageFileRestore(databasePath string, sourceFilePath string) error {
	pathCheckError := checkBackupFilePath(sourceFilePath)
	if pathCheckError != nil {
		return pathCheckError
	}

	sourceFile, sourceOpenError := os.Open(sourceFilePath)
	if sourceOpenError != nil {
		return fmt.Errorf("could not open %s: %w", sourceFilePath, sourceOpenError)
	}
	defer sourceFile.Close()

	return stageRestoreFromGzip(databasePath, sourceFile)
}
