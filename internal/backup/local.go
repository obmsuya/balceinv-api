package backup

import (
	"compress/gzip"
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
)

const (
	localBackupsToKeep      = 7
	localBackupFolderName   = "backups"
	localBackupFilePrefix   = "balce-"
	localBackupFileSuffix   = ".db.gz"
	backupDateLayout        = "2006-01-02"
	BeforeRestoreBackupName = "before-restore"
)

var (
	ErrInvalidBackupName = errors.New("choose one of the backups listed")
	ErrBackupNotFound    = errors.New("that backup is not on this computer")
	ErrInvalidBackupPath = errors.New("the backup file must be a full path ending in .gz")
)

type LocalBackup struct {
	Date      string    `json:"date"`
	FileName  string    `json:"file_name"`
	Size      int64     `json:"size"`
	CreatedAt time.Time `json:"created_at"`
}

type Store struct {
	openDatabase    *database.Database
	sqlitePath      string
	backupDirectory string
	fileMutex       sync.Mutex
	cloud           cloudStatus
}

func NewStore(openDatabase *database.Database, sqlitePath string, dataDirectory string) *Store {
	return &Store{
		openDatabase:    openDatabase,
		sqlitePath:      sqlitePath,
		backupDirectory: filepath.Join(dataDirectory, localBackupFolderName),
	}
}

func PendingRestorePath(sqlitePath string) string {
	return sqlitePath + ".restore"
}

func (store *Store) ensureDirectory() error {
	createError := os.MkdirAll(store.backupDirectory, 0o755)
	if createError != nil {
		return fmt.Errorf("could not create the backup folder: %w", createError)
	}
	return nil
}

func (store *Store) backupFilePath(backupName string) string {
	return filepath.Join(store.backupDirectory, localBackupFilePrefix+backupName+localBackupFileSuffix)
}

func (store *Store) describe(backupName string) (LocalBackup, error) {
	backupFileInfo, statError := os.Stat(store.backupFilePath(backupName))
	if statError != nil {
		return LocalBackup{}, statError
	}
	localBackup := LocalBackup{
		Date:      backupName,
		FileName:  filepath.Base(store.backupFilePath(backupName)),
		Size:      backupFileInfo.Size(),
		CreatedAt: backupFileInfo.ModTime().UTC(),
	}
	return localBackup, nil
}

func (store *Store) writeGzippedSnapshot(ctx context.Context, gzipFilePath string) error {
	snapshotFilePath := gzipFilePath + ".snapshot"
	os.Remove(snapshotFilePath)
	defer os.Remove(snapshotFilePath)

	_, vacuumError := store.openDatabase.Writer.ExecContext(ctx, "VACUUM INTO $1", snapshotFilePath)
	if vacuumError != nil {
		return fmt.Errorf("could not copy the database: %w", vacuumError)
	}

	snapshotFile, openError := os.Open(snapshotFilePath)
	if openError != nil {
		return fmt.Errorf("could not read the database copy: %w", openError)
	}
	defer snapshotFile.Close()

	gzipFile, createError := os.Create(gzipFilePath)
	if createError != nil {
		return fmt.Errorf("could not create the backup file: %w", createError)
	}
	defer gzipFile.Close()

	gzipWriter := gzip.NewWriter(gzipFile)
	_, copyError := io.Copy(gzipWriter, snapshotFile)
	if copyError != nil {
		return fmt.Errorf("could not compress the backup: %w", copyError)
	}
	closeError := gzipWriter.Close()
	if closeError != nil {
		return fmt.Errorf("could not finish the backup file: %w", closeError)
	}
	return gzipFile.Sync()
}

func (store *Store) WriteLocalBackup(ctx context.Context) (LocalBackup, error) {
	store.fileMutex.Lock()
	defer store.fileMutex.Unlock()
	return store.writeNamedBackup(ctx, time.Now().Format(backupDateLayout), true)
}

func (store *Store) writeNamedBackup(ctx context.Context, backupName string, shouldPrune bool) (LocalBackup, error) {
	directoryError := store.ensureDirectory()
	if directoryError != nil {
		return LocalBackup{}, directoryError
	}

	backupFilePath := store.backupFilePath(backupName)
	writingFilePath := backupFilePath + ".writing"
	defer os.Remove(writingFilePath)

	snapshotError := store.writeGzippedSnapshot(ctx, writingFilePath)
	if snapshotError != nil {
		return LocalBackup{}, snapshotError
	}
	renameError := os.Rename(writingFilePath, backupFilePath)
	if renameError != nil {
		return LocalBackup{}, fmt.Errorf("could not save the backup file: %w", renameError)
	}

	if shouldPrune {
		store.pruneOldBackups()
	}
	slog.Info("local backup written", "name", backupName)
	return store.describe(backupName)
}

func (store *Store) datedBackupNames() ([]string, error) {
	directoryEntries, readError := os.ReadDir(store.backupDirectory)
	if errors.Is(readError, os.ErrNotExist) {
		return []string{}, nil
	}
	if readError != nil {
		return nil, fmt.Errorf("could not list backups: %w", readError)
	}

	backupNames := []string{}
	for _, directoryEntry := range directoryEntries {
		entryName := directoryEntry.Name()
		isBackupFile := strings.HasPrefix(entryName, localBackupFilePrefix) && strings.HasSuffix(entryName, localBackupFileSuffix)
		if !isBackupFile {
			continue
		}
		backupName := strings.TrimSuffix(strings.TrimPrefix(entryName, localBackupFilePrefix), localBackupFileSuffix)
		_, parseError := time.Parse(backupDateLayout, backupName)
		if parseError != nil {
			continue
		}
		backupNames = append(backupNames, backupName)
	}
	sort.Sort(sort.Reverse(sort.StringSlice(backupNames)))
	return backupNames, nil
}

func (store *Store) pruneOldBackups() {
	backupNames, listError := store.datedBackupNames()
	if listError != nil || len(backupNames) <= localBackupsToKeep {
		return
	}
	for _, expiredName := range backupNames[localBackupsToKeep:] {
		os.Remove(store.backupFilePath(expiredName))
	}
}

func (store *Store) ListLocalBackups() ([]LocalBackup, error) {
	backupNames, listError := store.datedBackupNames()
	if listError != nil {
		return nil, listError
	}
	localBackups := []LocalBackup{}
	for _, backupName := range backupNames {
		localBackup, describeError := store.describe(backupName)
		if describeError != nil {
			continue
		}
		localBackups = append(localBackups, localBackup)
	}
	return localBackups, nil
}

func (store *Store) BeforeRestoreCopy() *LocalBackup {
	beforeRestoreBackup, describeError := store.describe(BeforeRestoreBackupName)
	if describeError != nil {
		return nil
	}
	return &beforeRestoreBackup
}

func (store *Store) IsRestorePending() bool {
	_, statError := os.Stat(PendingRestorePath(store.sqlitePath))
	return statError == nil
}

func (store *Store) StageLocalRestore(ctx context.Context, backupName string) error {
	_, parseError := time.Parse(backupDateLayout, backupName)
	isKnownName := parseError == nil || backupName == BeforeRestoreBackupName
	if !isKnownName {
		return ErrInvalidBackupName
	}

	store.fileMutex.Lock()
	defer store.fileMutex.Unlock()

	backupFile, openError := os.Open(store.backupFilePath(backupName))
	if openError != nil {
		return ErrBackupNotFound
	}
	defer backupFile.Close()

	restoringTheSafetyCopy := backupName == BeforeRestoreBackupName
	if !restoringTheSafetyCopy {
		_, safetyCopyError := store.writeNamedBackup(ctx, BeforeRestoreBackupName, false)
		if safetyCopyError != nil {
			return fmt.Errorf("could not keep a copy of today's data before restoring: %w", safetyCopyError)
		}
	}
	return stageRestoreFromGzip(ctx, store.sqlitePath, backupFile)
}

func (store *Store) ExportBackup(ctx context.Context, targetFilePath string) error {
	pathError := checkBackupFilePath(targetFilePath)
	if pathError != nil {
		return pathError
	}

	freshBackup, backupError := store.WriteLocalBackup(ctx)
	if backupError != nil {
		return backupError
	}

	sourceFile, openError := os.Open(store.backupFilePath(freshBackup.Date))
	if openError != nil {
		return fmt.Errorf("could not read the new backup: %w", openError)
	}
	defer sourceFile.Close()

	targetFile, createError := os.Create(targetFilePath)
	if createError != nil {
		return fmt.Errorf("could not write to %s: %w", targetFilePath, createError)
	}
	_, copyError := io.Copy(targetFile, sourceFile)
	closeError := targetFile.Close()
	if copyError != nil {
		return fmt.Errorf("could not write the backup file: %w", copyError)
	}
	if closeError != nil {
		return fmt.Errorf("could not finish the backup file: %w", closeError)
	}
	return nil
}

func (store *Store) StageFileRestore(ctx context.Context, sourceFilePath string) error {
	pathError := checkBackupFilePath(sourceFilePath)
	if pathError != nil {
		return pathError
	}

	sourceFile, openError := os.Open(sourceFilePath)
	if openError != nil {
		return ErrBackupNotFound
	}
	defer sourceFile.Close()

	store.fileMutex.Lock()
	defer store.fileMutex.Unlock()

	_, safetyCopyError := store.writeNamedBackup(ctx, BeforeRestoreBackupName, false)
	if safetyCopyError != nil {
		return fmt.Errorf("could not keep a copy of today's data before restoring: %w", safetyCopyError)
	}
	return stageRestoreFromGzip(ctx, store.sqlitePath, sourceFile)
}

func (store *Store) StageGzipRestore(ctx context.Context, gzipSource io.Reader) error {
	store.fileMutex.Lock()
	defer store.fileMutex.Unlock()

	_, safetyCopyError := store.writeNamedBackup(ctx, BeforeRestoreBackupName, false)
	if safetyCopyError != nil {
		return fmt.Errorf("could not keep a copy of today's data before restoring: %w", safetyCopyError)
	}
	return stageRestoreFromGzip(ctx, store.sqlitePath, gzipSource)
}

func checkBackupFilePath(backupFilePath string) error {
	isAbsolute := filepath.IsAbs(backupFilePath)
	hasGzipExtension := strings.HasSuffix(strings.ToLower(backupFilePath), ".gz")
	if !isAbsolute || !hasGzipExtension {
		return ErrInvalidBackupPath
	}
	return nil
}

func stageRestoreFromGzip(ctx context.Context, sqlitePath string, gzipSource io.Reader) error {
	pendingRestorePath := PendingRestorePath(sqlitePath)
	unpackingFilePath := pendingRestorePath + ".unpacking"
	defer os.Remove(unpackingFilePath)

	unzipError := unzipToFile(gzipSource, unpackingFilePath)
	if unzipError != nil {
		return unzipError
	}
	checkError := CheckRestorableDatabase(ctx, unpackingFilePath)
	if checkError != nil {
		return checkError
	}
	renameError := os.Rename(unpackingFilePath, pendingRestorePath)
	if renameError != nil {
		return fmt.Errorf("could not stage the restore: %w", renameError)
	}
	slog.Info("restore staged, it applies on the next start")
	return nil
}

func unzipToFile(gzipSource io.Reader, targetFilePath string) error {
	gzipReader, readerError := gzip.NewReader(gzipSource)
	if readerError != nil {
		return ErrNotABackup
	}
	defer gzipReader.Close()

	targetFile, createError := os.Create(targetFilePath)
	if createError != nil {
		return fmt.Errorf("could not unpack the backup: %w", createError)
	}
	defer targetFile.Close()

	_, copyError := io.Copy(targetFile, gzipReader)
	if copyError != nil {
		return ErrNotABackup
	}
	return nil
}

func ApplyPendingRestore(sqlitePath string) (bool, error) {
	pendingRestorePath := PendingRestorePath(sqlitePath)
	_, statError := os.Stat(pendingRestorePath)
	if errors.Is(statError, os.ErrNotExist) {
		return false, nil
	}
	if statError != nil {
		return false, fmt.Errorf("could not check for a pending restore: %w", statError)
	}

	previousDatabasePath := sqlitePath + ".before-restore"
	for _, fileSuffix := range []string{"", "-wal", "-shm"} {
		os.Remove(previousDatabasePath + fileSuffix)
		moveError := os.Rename(sqlitePath+fileSuffix, previousDatabasePath+fileSuffix)
		moveFailed := moveError != nil && !errors.Is(moveError, os.ErrNotExist)
		if moveFailed {
			return false, fmt.Errorf("could not set the current database aside: %w", moveError)
		}
	}

	restoreMoveError := os.Rename(pendingRestorePath, sqlitePath)
	if restoreMoveError != nil {
		for _, fileSuffix := range []string{"", "-wal", "-shm"} {
			os.Rename(previousDatabasePath+fileSuffix, sqlitePath+fileSuffix)
		}
		return false, fmt.Errorf("could not put the restored database in place: %w", restoreMoveError)
	}
	slog.Info("restore applied", "previousDatabase", previousDatabasePath)
	return true, nil
}
