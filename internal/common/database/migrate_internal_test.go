package database

import (
	"context"
	"database/sql"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/config"
)

func TestSqliteIsCopiedBeforePendingMigrations(t *testing.T) {
	sqlitePath := filepath.Join(t.TempDir(), "Application Support", "balce.db")

	migrator, openMigratorError := openMigrator(config.EngineSqlite, "", sqlitePath)
	if openMigratorError != nil {
		t.Fatalf("open migrator: %v", openMigratorError)
	}
	stepToFirstVersionError := migrator.Migrate(1)
	migrator.Close()
	if stepToFirstVersionError != nil {
		t.Fatalf("migrate to version 1: %v", stepToFirstVersionError)
	}

	migrationResult, migrateUpError := MigrateUp(context.Background(), config.EngineSqlite, "", sqlitePath)
	if migrateUpError != nil {
		t.Fatalf("migrate up: %v", migrateUpError)
	}

	if migrationResult.PreviousVersion != 1 {
		t.Fatalf("previous version %d, want 1", migrationResult.PreviousVersion)
	}
	if migrationResult.PreMigrationCopy == "" {
		t.Fatal("expected a pre-migration copy when migrations were pending")
	}

	copyInfo, statError := os.Stat(migrationResult.PreMigrationCopy)
	if statError != nil {
		t.Fatalf("pre-migration copy missing: %v", statError)
	}
	if copyInfo.Size() == 0 {
		t.Fatal("pre-migration copy is empty")
	}
}

func TestUnrecognizedSqliteIsLeftUntouched(t *testing.T) {
	sqlitePath := filepath.Join(t.TempDir(), "legacy.db")

	legacyConnection, openError := sql.Open("sqlite", "file:"+sqlitePath)
	if openError != nil {
		t.Fatalf("open legacy file: %v", openError)
	}
	_, createError := legacyConnection.Exec(`CREATE TABLE products (id INTEGER PRIMARY KEY, name TEXT)`)
	legacyConnection.Close()
	if createError != nil {
		t.Fatalf("create legacy table: %v", createError)
	}

	_, migrateUpError := MigrateUp(context.Background(), config.EngineSqlite, "", sqlitePath)
	if !errors.Is(migrateUpError, ErrUnrecognizedDatabase) {
		t.Fatalf("expected ErrUnrecognizedDatabase, got %v", migrateUpError)
	}

	inspectConnection, reopenError := sql.Open("sqlite", "file:"+sqlitePath+"?mode=ro")
	if reopenError != nil {
		t.Fatalf("reopen legacy file: %v", reopenError)
	}
	defer inspectConnection.Close()

	tableNames := []string{}
	tableRows, queryError := inspectConnection.Query(`SELECT name FROM sqlite_master WHERE type = 'table' ORDER BY name`)
	if queryError != nil {
		t.Fatalf("list tables: %v", queryError)
	}
	defer tableRows.Close()
	for tableRows.Next() {
		tableName := ""
		tableRows.Scan(&tableName)
		tableNames = append(tableNames, tableName)
	}

	if len(tableNames) != 1 || tableNames[0] != "products" {
		t.Fatalf("legacy file was modified; tables are now %v", tableNames)
	}
}
