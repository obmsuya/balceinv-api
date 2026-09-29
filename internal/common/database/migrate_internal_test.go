package database

import (
	"context"
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
