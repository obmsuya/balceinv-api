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
			SqlitePath: filepath.Join(sqliteDirectory, "balce.sqlite"),
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

func OpenMigratedAsApp(t *testing.T, engineCase EngineCase) *database.Database {
	t.Helper()

	isSqlite := engineCase.Engine == config.EngineSqlite
	if isSqlite {
		return OpenMigrated(t, engineCase)
	}

	migratedAsAdmin := OpenMigrated(t, engineCase)
	grantAppRole(t, migratedAsAdmin)

	parsedUrl, parseError := url.Parse(engineCase.DatabaseUrl)
	if parseError != nil {
		t.Fatalf("parse scratch url: %v", parseError)
	}
	parsedUrl.User = url.UserPassword(testAppRoleName, testAppRoleName)

	testContext, cancelTest := context.WithTimeout(context.Background(), time.Minute)
	defer cancelTest()

	appDatabase, openError := database.Open(testContext, config.EnginePostgres, parsedUrl.String(), "")
	if openError != nil {
		t.Fatalf("open as app role: %v", openError)
	}
	t.Cleanup(func() {
		appDatabase.Close()
	})

	return appDatabase
}

const testAppRoleName = "balce_test_app"

func grantAppRole(t *testing.T, adminDatabase *database.Database) {
	t.Helper()

	createRoleStatement := `
		DO $$
		BEGIN
			CREATE ROLE balce_test_app LOGIN PASSWORD 'balce_test_app' NOSUPERUSER NOBYPASSRLS;
		EXCEPTION WHEN duplicate_object OR unique_violation THEN
			NULL;
		END
		$$
	`
	_, createRoleError := adminDatabase.Writer.Exec(createRoleStatement)
	if createRoleError != nil {
		t.Fatalf("create app role: %v", createRoleError)
	}

	grantStatements := []string{
		`GRANT USAGE ON SCHEMA public TO balce_test_app`,
		`GRANT SELECT, INSERT, UPDATE, DELETE ON ALL TABLES IN SCHEMA public TO balce_test_app`,
	}
	for _, grantStatement := range grantStatements {
		_, grantError := adminDatabase.Writer.Exec(grantStatement)
		if grantError != nil {
			t.Fatalf("grant app role: %v", grantError)
		}
	}
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
