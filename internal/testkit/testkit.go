package testkit

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"net/url"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/config"
)

type EngineCase struct {
	Engine      config.Engine
	DatabaseUrl string
	SqlitePath  string
}

func ForEachEngine(t *testing.T, runCase func(t *testing.T, engineCase EngineCase)) {
	t.Helper()

	t.Run("sqlite", func(t *testing.T) {
		sqliteDirectory := filepath.Join(t.TempDir(), "Application Support")
		sqliteCase := EngineCase{
			Engine:     config.EngineSqlite,
			SqlitePath: filepath.Join(sqliteDirectory, "balce.db"),
		}
		runCase(t, sqliteCase)
	})

	adminDatabaseUrl, hasPostgres := os.LookupEnv("TEST_DATABASE_URL")
	if !hasPostgres {
		return
	}

	t.Run("postgres", func(t *testing.T) {
		postgresCase := EngineCase{
			Engine:      config.EnginePostgres,
			DatabaseUrl: createScratchPostgresDatabase(t, adminDatabaseUrl),
		}
		runCase(t, postgresCase)
	})
}

func OpenMigrated(t *testing.T, engineCase EngineCase) *database.Database {
	t.Helper()

	testContext, cancelTest := context.WithTimeout(context.Background(), time.Minute)
	defer cancelTest()

	_, migrateError := database.MigrateUp(testContext, engineCase.Engine, engineCase.DatabaseUrl, engineCase.SqlitePath)
	if migrateError != nil {
		t.Fatalf("migrate up: %v", migrateError)
	}

	openDatabase, openError := database.Open(testContext, engineCase.Engine, engineCase.DatabaseUrl, engineCase.SqlitePath)
	if openError != nil {
		t.Fatalf("open database: %v", openError)
	}
	t.Cleanup(func() {
		openDatabase.Close()
	})

	return openDatabase
}

func createScratchPostgresDatabase(t *testing.T, adminDatabaseUrl string) string {
	t.Helper()

	randomBytes := make([]byte, 6)
	rand.Read(randomBytes)
	scratchDatabaseName := "balce_test_" + hex.EncodeToString(randomBytes)

	adminConnection, openAdminError := sql.Open("pgx", adminDatabaseUrl)
	if openAdminError != nil {
		t.Fatalf("open admin connection: %v", openAdminError)
	}

	_, createError := adminConnection.Exec("CREATE DATABASE " + scratchDatabaseName)
	if createError != nil {
		adminConnection.Close()
		t.Fatalf("create scratch database: %v", createError)
	}

	t.Cleanup(func() {
		adminConnection.Exec("DROP DATABASE IF EXISTS " + scratchDatabaseName + " WITH (FORCE)")
		adminConnection.Close()
	})

	parsedUrl, parseError := url.Parse(adminDatabaseUrl)
	if parseError != nil {
		t.Fatalf("parse TEST_DATABASE_URL: %v", parseError)
	}
	parsedUrl.Path = "/" + scratchDatabaseName

	return parsedUrl.String()
}
