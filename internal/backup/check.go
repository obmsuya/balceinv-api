package backup

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io"
	"os"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/config"
)

const sqliteFileHeader = "SQLite format 3\x00"

var (
	ErrNotABackup       = errors.New("this file is not a Balce backup")
	ErrOldFormatBackup  = errors.New("this backup is from the old version of Balce and can't be restored here")
	ErrNewerBackup      = errors.New("this backup comes from a newer version of Balce; update this computer first")
	ErrDamagedBackup    = errors.New("this backup is damaged")
	ErrUnfinishedBackup = errors.New("this backup was taken in the middle of an update and can't be used")
)

func CheckRestorableDatabase(ctx context.Context, databaseFilePath string) error {
	headerError := checkSqliteHeader(databaseFilePath)
	if headerError != nil {
		return headerError
	}

	checkConnection, openError := sql.Open("sqlite", "file:"+databaseFilePath+"?mode=ro")
	if openError != nil {
		return fmt.Errorf("could not open the backup: %w", openError)
	}
	defer checkConnection.Close()

	quickCheckResult := ""
	quickCheckError := checkConnection.QueryRowContext(ctx, "PRAGMA quick_check").Scan(&quickCheckResult)
	if quickCheckError != nil || quickCheckResult != "ok" {
		return ErrDamagedBackup
	}

	migrationTableCount := 0
	tableQuery := `SELECT COUNT(*) FROM sqlite_master WHERE type = 'table' AND name IN ('schema_migrations', 'companies')`
	tableError := checkConnection.QueryRowContext(ctx, tableQuery).Scan(&migrationTableCount)
	if tableError != nil {
		return ErrDamagedBackup
	}
	if migrationTableCount != 2 {
		return ErrOldFormatBackup
	}

	backupVersion := uint(0)
	isDirty := false
	versionError := checkConnection.QueryRowContext(ctx, "SELECT version, dirty FROM schema_migrations LIMIT 1").Scan(&backupVersion, &isDirty)
	if versionError != nil {
		return ErrOldFormatBackup
	}
	if isDirty {
		return ErrUnfinishedBackup
	}

	latestVersion, latestError := database.LatestEmbeddedVersion(config.EngineSqlite)
	if latestError != nil {
		return latestError
	}
	if backupVersion > latestVersion {
		return ErrNewerBackup
	}
	return nil
}

func checkSqliteHeader(databaseFilePath string) error {
	databaseFile, openError := os.Open(databaseFilePath)
	if openError != nil {
		return fmt.Errorf("could not read the backup: %w", openError)
	}
	defer databaseFile.Close()

	headerBytes := make([]byte, len(sqliteFileHeader))
	_, readError := io.ReadFull(databaseFile, headerBytes)
	hasSqliteHeader := readError == nil && string(headerBytes) == sqliteFileHeader
	if !hasSqliteHeader {
		return ErrNotABackup
	}
	return nil
}
