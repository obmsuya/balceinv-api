package database_test

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/config"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/google/uuid"
)

func TestMigrationsRunUpDownUp(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		testContext := context.Background()

		firstResult, firstUpError := database.MigrateUp(testContext, engineCase.Engine, engineCase.DatabaseUrl, engineCase.SqlitePath)
		if firstUpError != nil {
			t.Fatalf("first up: %v", firstUpError)
		}
		if firstResult.CurrentVersion == 0 {
			t.Fatalf("expected a schema version after migrating, got 0")
		}

		repeatResult, repeatUpError := database.MigrateUp(testContext, engineCase.Engine, engineCase.DatabaseUrl, engineCase.SqlitePath)
		if repeatUpError != nil {
			t.Fatalf("repeat up must be a no-op: %v", repeatUpError)
		}
		if repeatResult.PreMigrationCopy != "" {
			t.Fatalf("no copy expected when nothing is pending, got %s", repeatResult.PreMigrationCopy)
		}

		downError := database.MigrateDownAll(engineCase.Engine, engineCase.DatabaseUrl, engineCase.SqlitePath)
		if downError != nil {
			t.Fatalf("down: %v", downError)
		}

		secondResult, secondUpError := database.MigrateUp(testContext, engineCase.Engine, engineCase.DatabaseUrl, engineCase.SqlitePath)
		if secondUpError != nil {
			t.Fatalf("second up: %v", secondUpError)
		}
		if secondResult.CurrentVersion != firstResult.CurrentVersion {
			t.Fatalf("version after re-up %d, want %d", secondResult.CurrentVersion, firstResult.CurrentVersion)
		}
	})
}

func TestTimeAndUuidRoundTrip(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		openDatabase := testkit.OpenMigrated(t, engineCase)
		testContext := context.Background()

		companyId := uuid.Must(uuid.NewV7())
		createdAt := time.Date(2026, 9, 29, 21, 30, 15, 123456000, time.UTC)

		insertQuery := `
			INSERT INTO companies (id, name, created_at, updated_at)
			VALUES ($1, $2, $3, $4)
		`
		_, insertError := openDatabase.Writer.ExecContext(testContext, insertQuery, companyId, "Round Trip Ltd", createdAt, createdAt)
		if insertError != nil {
			t.Fatalf("insert: %v", insertError)
		}

		selectQuery := `
			SELECT id, created_at
			FROM companies
			WHERE id = $1
		`
		foundId := uuid.UUID{}
		foundCreatedAt := time.Time{}
		scanError := openDatabase.Reader.QueryRowContext(testContext, selectQuery, companyId).Scan(&foundId, &foundCreatedAt)
		if scanError != nil {
			t.Fatalf("select: %v", scanError)
		}

		if foundId != companyId {
			t.Fatalf("uuid came back as %s, want %s", foundId, companyId)
		}
		if !foundCreatedAt.Equal(createdAt) {
			t.Fatalf("time came back as %s, want %s", foundCreatedAt.UTC(), createdAt)
		}
	})
}

func TestNumberedPlaceholdersBindByNumber(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		openDatabase := testkit.OpenMigrated(t, engineCase)

		reversedQuery := `SELECT CAST($2 AS TEXT), CAST($1 AS TEXT)`
		firstColumn := ""
		secondColumn := ""
		scanError := openDatabase.Reader.QueryRowContext(context.Background(), reversedQuery, "one", "two").Scan(&firstColumn, &secondColumn)
		if scanError != nil {
			t.Fatalf("select: %v", scanError)
		}

		bindsByNumber := firstColumn == "two" && secondColumn == "one"
		if !bindsByNumber {
			t.Fatalf("placeholders bound by appearance, got (%s, %s)", firstColumn, secondColumn)
		}
	})
}

func TestConcurrentSqliteWritesNeverLock(t *testing.T) {
	sqliteCase := testkit.EngineCase{
		Engine:     config.EngineSqlite,
		SqlitePath: t.TempDir() + "/concurrent.db",
	}
	openDatabase := testkit.OpenMigrated(t, sqliteCase)
	testContext := context.Background()

	writerCount := 50
	var waitGroup sync.WaitGroup
	writeErrors := make(chan error, writerCount)

	for writerIndex := 0; writerIndex < writerCount; writerIndex++ {
		waitGroup.Add(1)
		go func() {
			defer waitGroup.Done()

			writeTransaction, beginError := openDatabase.Writer.BeginTx(testContext, nil)
			if beginError != nil {
				writeErrors <- beginError
				return
			}
			defer writeTransaction.Rollback()

			readQuery := `SELECT COUNT(*) FROM companies`
			existingCount := 0
			readError := writeTransaction.QueryRowContext(testContext, readQuery).Scan(&existingCount)
			if readError != nil {
				writeErrors <- readError
				return
			}

			insertQuery := `INSERT INTO companies (id, name) VALUES ($1, $2)`
			_, insertError := writeTransaction.ExecContext(testContext, insertQuery, uuid.Must(uuid.NewV7()), "Concurrent Shop")
			if insertError != nil {
				writeErrors <- insertError
				return
			}

			writeErrors <- writeTransaction.Commit()
		}()
	}

	waitGroup.Wait()
	close(writeErrors)

	for writeError := range writeErrors {
		if writeError != nil {
			t.Fatalf("concurrent write failed: %v", writeError)
		}
	}

	finalCount := 0
	countError := openDatabase.Reader.QueryRowContext(testContext, `SELECT COUNT(*) FROM companies`).Scan(&finalCount)
	if countError != nil {
		t.Fatalf("count: %v", countError)
	}
	if finalCount != writerCount {
		t.Fatalf("expected %d companies, found %d", writerCount, finalCount)
	}
}
