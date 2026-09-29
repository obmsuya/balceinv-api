package backup_test

import (
	"compress/gzip"
	"context"
	"database/sql"
	"net/http"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/backup"
	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/config"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/google/uuid"
	_ "modernc.org/sqlite"
)

func isolateLicense(t *testing.T) {
	homeDirectory := t.TempDir()
	t.Setenv("HOME", homeDirectory)
	t.Setenv("APPDATA", homeDirectory)
	t.Setenv("XDG_CONFIG_HOME", homeDirectory)
}

func tableCounts(t *testing.T, databaseConnection *sql.DB) map[string]int64 {
	t.Helper()
	tableRows, queryError := databaseConnection.Query(`SELECT name FROM sqlite_master WHERE type = 'table' AND name NOT LIKE 'sqlite_%' ORDER BY name`)
	if queryError != nil {
		t.Fatalf("list tables: %v", queryError)
	}
	tableNames := []string{}
	for tableRows.Next() {
		tableName := ""
		tableRows.Scan(&tableName)
		tableNames = append(tableNames, tableName)
	}
	tableRows.Close()

	counts := map[string]int64{}
	for _, tableName := range tableNames {
		rowCount := int64(0)
		countError := databaseConnection.QueryRow(`SELECT COUNT(*) FROM "` + tableName + `"`).Scan(&rowCount)
		if countError != nil {
			t.Fatalf("count %s: %v", tableName, countError)
		}
		counts[tableName] = rowCount
	}
	return counts
}

func sell(t *testing.T, harness *apptest.Harness, sessionToken string, clientRef string, productId string) {
	t.Helper()
	saleResponse := harness.Call(http.MethodPost, "/api/sales", sessionToken, map[string]any{
		"client_ref": clientRef,
		"items":      []map[string]any{{"product_id": productId, "quantity": 1}},
		"payments":   []map[string]any{{"method": "cash", "amount": 1000}},
	})
	if saleResponse.Status != http.StatusCreated {
		t.Fatalf("sale returned %d %v", saleResponse.Status, saleResponse.Body)
	}
}

func writeGzipFile(t *testing.T, sourcePath string, targetPath string) {
	t.Helper()
	sourceBytes, readError := os.ReadFile(sourcePath)
	if readError != nil {
		t.Fatalf("read %s: %v", sourcePath, readError)
	}
	targetFile, createError := os.Create(targetPath)
	if createError != nil {
		t.Fatalf("create %s: %v", targetPath, createError)
	}
	gzipWriter := gzip.NewWriter(targetFile)
	gzipWriter.Write(sourceBytes)
	gzipWriter.Close()
	targetFile.Close()
}

func TestBackupRestoreRoundTripKeepsEveryRow(t *testing.T) {
	isolateLicense(t)
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		if engineCase.Engine != config.EngineSqlite {
			cloudHarness := apptest.Start(t, engineCase)
			company := cloudHarness.CreateCompany("Cloud Shop", "owner@cloudbackup.test")
			if cloudHarness.Call(http.MethodGet, "/api/backup/status", company.OwnerToken, nil).Status != http.StatusNotFound {
				t.Fatal("backup routes exist in cloud mode")
			}
			return
		}

		harness := apptest.StartDesktop(t, engineCase)
		company := harness.CreateCompany("Backup Shop", "owner@backup.test")
		cashierToken := harness.CreateStaff(company, "cashier@backup.test", []string{"sales:create", "settings:view", "settings:edit"}, []uuid.UUID{company.ShopId})
		productId := harness.Call(http.MethodPost, "/api/products", company.OwnerToken, map[string]any{"sku": "TEA", "name": "Tea", "price": 1000, "opening_quantity": 20}).Data()["id"].(string)
		sell(t, harness, company.OwnerToken, "backup-sale-1", productId)

		backedUp := harness.Call(http.MethodPost, "/api/backup/local", cashierToken, nil)
		backupDate := time.Now().Format("2006-01-02")
		if backedUp.Status != http.StatusOK || backedUp.Data()["date"] != backupDate || backedUp.Data()["size"].(float64) <= 0 {
			t.Fatalf("backup returned %d %v", backedUp.Status, backedUp.Body)
		}
		countsAtBackup := tableCounts(t, harness.Database.Writer)

		sell(t, harness, company.OwnerToken, "backup-sale-2", productId)
		if tableCounts(t, harness.Database.Writer)["sales"] != countsAtBackup["sales"]+1 {
			t.Fatal("the second sale was not saved")
		}

		if harness.Call(http.MethodPost, "/api/backup/local/restore", cashierToken, map[string]any{"date": backupDate}).Status != http.StatusForbidden {
			t.Fatal("a non-owner staged a restore")
		}
		for _, badName := range []string{"../../balce", "2020-01-01", "yesterday"} {
			if harness.Call(http.MethodPost, "/api/backup/local/restore", company.OwnerToken, map[string]any{"date": badName}).Status != http.StatusBadRequest {
				t.Fatalf("restoring %q was not refused", badName)
			}
		}

		staged := harness.Call(http.MethodPost, "/api/backup/local/restore", company.OwnerToken, map[string]any{"date": backupDate})
		if staged.Status != http.StatusOK || staged.Data()["restart_required"] != true {
			t.Fatalf("staging returned %d %v", staged.Status, staged.Body)
		}
		status := harness.Call(http.MethodGet, "/api/backup/status", cashierToken, nil).Data()
		if status["restore_pending"] != true || status["before_restore_copy"] == nil || status["cloud_available"] != false || len(status["local_backups"].([]any)) != 1 {
			t.Fatalf("status after staging was %v", status)
		}

		harness.Database.Close()
		isRestored, applyError := backup.ApplyPendingRestore(engineCase.SqlitePath)
		if applyError != nil || !isRestored {
			t.Fatalf("applying the restore: %v %v", isRestored, applyError)
		}
		_, previousStatError := os.Stat(engineCase.SqlitePath + ".before-restore")
		if previousStatError != nil {
			t.Fatalf("the replaced database was not kept: %v", previousStatError)
		}

		restoredDatabase, openError := database.Open(context.Background(), config.EngineSqlite, "", engineCase.SqlitePath)
		if openError != nil {
			t.Fatalf("reopen: %v", openError)
		}
		defer restoredDatabase.Close()
		countsAfterRestore := tableCounts(t, restoredDatabase.Writer)
		for tableName, rowCount := range countsAtBackup {
			if countsAfterRestore[tableName] != rowCount {
				t.Fatalf("%s has %d rows after restore, %d at backup", tableName, countsAfterRestore[tableName], rowCount)
			}
		}
		if len(countsAfterRestore) != len(countsAtBackup) {
			t.Fatalf("the restore has %d tables, the backup %d", len(countsAfterRestore), len(countsAtBackup))
		}
		isRestoredAgain, _ := backup.ApplyPendingRestore(engineCase.SqlitePath)
		if isRestoredAgain {
			t.Fatal("a restore was applied twice")
		}
	})
}

func TestExportImportRefusesFilesThatAreNotUsableBackups(t *testing.T) {
	isolateLicense(t)
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		if engineCase.Engine != config.EngineSqlite {
			return
		}
		harness := apptest.StartDesktop(t, engineCase)
		company := harness.CreateCompany("Export Shop", "owner@export.test")
		scratchDirectory := t.TempDir()

		exportPath := filepath.Join(scratchDirectory, "balce-export.db.gz")
		exported := harness.Call(http.MethodPost, "/api/backup/export", company.OwnerToken, map[string]any{"path": exportPath})
		exportInfo, statError := os.Stat(exportPath)
		if exported.Status != http.StatusOK || statError != nil || exportInfo.Size() == 0 {
			t.Fatalf("export returned %d %v, file %v", exported.Status, exported.Body, statError)
		}
		if harness.Call(http.MethodPost, "/api/backup/import", company.OwnerToken, map[string]any{"path": exportPath}).Status != http.StatusOK {
			t.Fatal("a fresh export could not be imported")
		}

		notGzipPath := filepath.Join(scratchDirectory, "notes.gz")
		os.WriteFile(notGzipPath, []byte("shopping list"), 0o600)

		oldFormatPath := filepath.Join(scratchDirectory, "old.db")
		oldDatabase, _ := sql.Open("sqlite", oldFormatPath)
		oldDatabase.Exec(`CREATE TABLE products (id INTEGER PRIMARY KEY, name TEXT)`)
		oldDatabase.Close()
		oldFormatGzipPath := filepath.Join(scratchDirectory, "old.db.gz")
		writeGzipFile(t, oldFormatPath, oldFormatGzipPath)

		newerPath := filepath.Join(scratchDirectory, "newer.db")
		newerDatabase, _ := sql.Open("sqlite", newerPath)
		newerDatabase.Exec(`CREATE TABLE schema_migrations (version INTEGER, dirty BOOLEAN)`)
		newerDatabase.Exec(`CREATE TABLE companies (id TEXT)`)
		newerDatabase.Exec(`INSERT INTO schema_migrations VALUES (9999, 0)`)
		newerDatabase.Close()
		newerGzipPath := filepath.Join(scratchDirectory, "newer.db.gz")
		writeGzipFile(t, newerPath, newerGzipPath)

		for _, refusedPath := range []string{notGzipPath, oldFormatGzipPath, newerGzipPath, "relative/backup.gz", filepath.Join(scratchDirectory, "missing.gz"), filepath.Join(scratchDirectory, "backup.txt")} {
			refused := harness.Call(http.MethodPost, "/api/backup/import", company.OwnerToken, map[string]any{"path": refusedPath})
			if refused.Status != http.StatusBadRequest {
				t.Fatalf("importing %s returned %d %v", refusedPath, refused.Status, refused.Body)
			}
		}
		oldAnswer := harness.Call(http.MethodPost, "/api/backup/import", company.OwnerToken, map[string]any{"path": oldFormatGzipPath})
		newerAnswer := harness.Call(http.MethodPost, "/api/backup/import", company.OwnerToken, map[string]any{"path": newerGzipPath})
		if oldAnswer.Body["message"] != "This backup is from the old version of Balce and can't be restored here" || newerAnswer.Code() != "invalid_backup" {
			t.Fatalf("old %v, newer %v", oldAnswer.Body, newerAnswer.Body)
		}

		if harness.Call(http.MethodGet, "/api/backup/cloud", company.OwnerToken, nil).Status != http.StatusPaymentRequired {
			t.Fatal("cloud backups were listed without a paid license")
		}
	})
}
