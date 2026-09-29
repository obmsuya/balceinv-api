package database

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"strconv"
	"strings"

	"github.com/chrisostomemataba/balceinv-api/internal/config"
	"github.com/chrisostomemataba/balceinv-api/migrations"
	"github.com/golang-migrate/migrate/v4"
	migratedatabase "github.com/golang-migrate/migrate/v4/database"
	migratepgx "github.com/golang-migrate/migrate/v4/database/pgx/v5"
	migratesqlite "github.com/golang-migrate/migrate/v4/database/sqlite"
	"github.com/golang-migrate/migrate/v4/source/iofs"
)

var ErrUnrecognizedDatabase = errors.New("this SQLite file already holds tables but has no migration history; it belongs to another program or an older Balce version, so it was left untouched")

type MigrationResult struct {
	PreviousVersion  uint
	CurrentVersion   uint
	PreMigrationCopy string
}

func MigrateUp(ctx context.Context, engine config.Engine, databaseUrl string, sqlitePath string) (MigrationResult, error) {
	result := MigrationResult{}

	if engine == config.EngineSqlite {
		recognitionError := refuseUnrecognizedSqlite(ctx, sqlitePath)
		if recognitionError != nil {
			return result, recognitionError
		}
	}

	migrator, openMigratorError := openMigrator(engine, databaseUrl, sqlitePath)
	if openMigratorError != nil {
		return result, openMigratorError
	}
	defer migrator.Close()

	previousVersion, isDirty, readVersionError := migrator.Version()
	hasNoVersionYet := errors.Is(readVersionError, migrate.ErrNilVersion)
	if readVersionError != nil && !hasNoVersionYet {
		return result, fmt.Errorf("failed to read schema version: %w", readVersionError)
	}
	if isDirty {
		return result, fmt.Errorf("schema version %d is dirty: a previous migration failed halfway; restore the pre-migration copy or fix the version by hand", previousVersion)
	}
	result.PreviousVersion = previousVersion

	latestVersion, latestVersionError := latestEmbeddedVersion(engine)
	if latestVersionError != nil {
		return result, latestVersionError
	}

	hasPendingMigrations := hasNoVersionYet || previousVersion < latestVersion
	shouldCopySqliteFirst := engine == config.EngineSqlite && !hasNoVersionYet && hasPendingMigrations
	if shouldCopySqliteFirst {
		copyPath, copyError := copySqliteBeforeMigration(ctx, sqlitePath, previousVersion)
		if copyError != nil {
			return result, copyError
		}
		result.PreMigrationCopy = copyPath
	}

	migrateUpError := migrator.Up()
	isAlreadyCurrent := errors.Is(migrateUpError, migrate.ErrNoChange)
	if migrateUpError != nil && !isAlreadyCurrent {
		return result, fmt.Errorf("failed to apply migrations: %w", migrateUpError)
	}

	currentVersion, _, readCurrentVersionError := migrator.Version()
	if readCurrentVersionError != nil {
		return result, fmt.Errorf("failed to read schema version after migrating: %w", readCurrentVersionError)
	}
	result.CurrentVersion = currentVersion

	return result, nil
}

func MigrateDownAll(engine config.Engine, databaseUrl string, sqlitePath string) error {
	migrator, openMigratorError := openMigrator(engine, databaseUrl, sqlitePath)
	if openMigratorError != nil {
		return openMigratorError
	}
	defer migrator.Close()

	migrateDownError := migrator.Down()
	isAlreadyEmpty := errors.Is(migrateDownError, migrate.ErrNoChange)
	if migrateDownError != nil && !isAlreadyEmpty {
		return fmt.Errorf("failed to roll back migrations: %w", migrateDownError)
	}

	return nil
}

func openMigrator(engine config.Engine, databaseUrl string, sqlitePath string) (*migrate.Migrate, error) {
	migrationFiles, migrationDirectory := embeddedMigrations(engine)

	sourceDriver, sourceError := iofs.New(migrationFiles, migrationDirectory)
	if sourceError != nil {
		return nil, fmt.Errorf("failed to read embedded migrations: %w", sourceError)
	}

	connection, databaseDriver, driverError := openMigrationDriver(engine, databaseUrl, sqlitePath)
	if driverError != nil {
		return nil, driverError
	}

	migrator, migratorError := migrate.NewWithInstance("iofs", sourceDriver, string(engine), databaseDriver)
	if migratorError != nil {
		connection.Close()
		return nil, fmt.Errorf("failed to prepare migrator: %w", migratorError)
	}

	return migrator, nil
}

func openMigrationDriver(engine config.Engine, databaseUrl string, sqlitePath string) (*sql.DB, migratedatabase.Driver, error) {
	if engine == config.EnginePostgres {
		postgresConnection, openError := sql.Open("pgx", databaseUrl)
		if openError != nil {
			return nil, nil, fmt.Errorf("failed to open postgres for migrations: %w", openError)
		}

		postgresDriver, driverError := migratepgx.WithInstance(postgresConnection, &migratepgx.Config{})
		if driverError != nil {
			postgresConnection.Close()
			return nil, nil, fmt.Errorf("failed to prepare postgres migration driver: %w", driverError)
		}

		return postgresConnection, postgresDriver, nil
	}

	createDirectoryError := ensureSqliteDirectory(sqlitePath)
	if createDirectoryError != nil {
		return nil, nil, createDirectoryError
	}

	sqliteConnection, openError := sql.Open("sqlite", sqliteDsn(sqlitePath, false))
	if openError != nil {
		return nil, nil, fmt.Errorf("failed to open sqlite for migrations: %w", openError)
	}
	sqliteConnection.SetMaxOpenConns(1)

	sqliteDriver, driverError := migratesqlite.WithInstance(sqliteConnection, &migratesqlite.Config{})
	if driverError != nil {
		sqliteConnection.Close()
		return nil, nil, fmt.Errorf("failed to prepare sqlite migration driver: %w", driverError)
	}

	return sqliteConnection, sqliteDriver, nil
}

func embeddedMigrations(engine config.Engine) (fs.FS, string) {
	if engine == config.EnginePostgres {
		return migrations.Postgres, "postgres"
	}
	return migrations.Sqlite, "sqlite"
}

func LatestEmbeddedVersion(engine config.Engine) (uint, error) {
	return latestEmbeddedVersion(engine)
}

func latestEmbeddedVersion(engine config.Engine) (uint, error) {
	migrationFiles, migrationDirectory := embeddedMigrations(engine)

	directoryEntries, readDirectoryError := fs.ReadDir(migrationFiles, migrationDirectory)
	if readDirectoryError != nil {
		return 0, fmt.Errorf("failed to list embedded migrations: %w", readDirectoryError)
	}

	latestVersion := uint(0)
	for _, directoryEntry := range directoryEntries {
		versionText, _, hasSeparator := strings.Cut(directoryEntry.Name(), "_")
		if !hasSeparator {
			continue
		}

		parsedVersion, parseError := strconv.ParseUint(versionText, 10, 64)
		if parseError != nil {
			return 0, fmt.Errorf("migration file %s has no numeric prefix: %w", directoryEntry.Name(), parseError)
		}

		if uint(parsedVersion) > latestVersion {
			latestVersion = uint(parsedVersion)
		}
	}

	return latestVersion, nil
}

func copySqliteBeforeMigration(ctx context.Context, sqlitePath string, previousVersion uint) (string, error) {
	copyPath := fmt.Sprintf("%s.pre-migration-v%d", sqlitePath, previousVersion)

	removeError := os.Remove(copyPath)
	copyAlreadyGone := errors.Is(removeError, fs.ErrNotExist)
	if removeError != nil && !copyAlreadyGone {
		return "", fmt.Errorf("failed to replace old pre-migration copy: %w", removeError)
	}

	sqliteConnection, openError := sql.Open("sqlite", sqliteDsn(sqlitePath, false))
	if openError != nil {
		return "", fmt.Errorf("failed to open sqlite for pre-migration copy: %w", openError)
	}
	defer sqliteConnection.Close()

	_, vacuumError := sqliteConnection.ExecContext(ctx, "VACUUM INTO ?", copyPath)
	if vacuumError != nil {
		return "", fmt.Errorf("failed to copy database before migrating: %w", vacuumError)
	}

	return copyPath, nil
}

func refuseUnrecognizedSqlite(ctx context.Context, sqlitePath string) error {
	_, statError := os.Stat(sqlitePath)
	fileIsMissing := errors.Is(statError, fs.ErrNotExist)
	if fileIsMissing {
		return nil
	}
	if statError != nil {
		return fmt.Errorf("failed to inspect database file: %w", statError)
	}

	readOnlyConnection, openError := sql.Open("sqlite", "file:"+sqlitePath+"?mode=ro")
	if openError != nil {
		return fmt.Errorf("failed to open database file for inspection: %w", openError)
	}
	defer readOnlyConnection.Close()

	inspectionQuery := `
		SELECT
			COUNT(*),
			COALESCE(SUM(CASE WHEN name = 'schema_migrations' THEN 1 ELSE 0 END), 0)
		FROM sqlite_master
		WHERE type = 'table' AND name NOT LIKE 'sqlite_%'
	`

	tableCount := 0
	migrationTableCount := 0
	scanError := readOnlyConnection.QueryRowContext(ctx, inspectionQuery).Scan(&tableCount, &migrationTableCount)
	if scanError != nil {
		return fmt.Errorf("failed to inspect database tables: %w", scanError)
	}

	holdsForeignTables := tableCount > 0 && migrationTableCount == 0
	if holdsForeignTables {
		return fmt.Errorf("%s: %w", sqlitePath, ErrUnrecognizedDatabase)
	}

	return nil
}
