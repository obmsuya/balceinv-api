package backup

import (
	"os"
	"path/filepath"
	"testing"
)

func useTemporaryAppData(t *testing.T) string {
	testDirectory := t.TempDir()
	t.Setenv("HOME", testDirectory)
	t.Setenv("XDG_CONFIG_HOME", testDirectory)
	t.Setenv("APPDATA", testDirectory)
	return testDirectory
}

func readRestoredNote(t *testing.T, databasePath string) string {
	restoredDatabase := openTestDatabase(t, databasePath, "")
	var restoredNote string
	restoredDatabase.Raw("SELECT text FROM notes").Scan(&restoredNote)
	restoredSqlDatabase, _ := restoredDatabase.DB()
	restoredSqlDatabase.Close()
	return restoredNote
}

func closedEmptyDatabase(t *testing.T, databasePath string) {
	emptyDatabase := openTestDatabase(t, databasePath, "")
	emptySqlDatabase, _ := emptyDatabase.DB()
	emptySqlDatabase.Close()
}

func TestLocalBackupRestoreAndPrune(t *testing.T) {
	testDirectory := useTemporaryAppData(t)

	sourceDatabase := openTestDatabase(t, filepath.Join(testDirectory, "source.db"), "offline sale")
	writtenBackup, writeError := WriteLocalBackup(sourceDatabase)
	if writeError != nil {
		t.Fatal(writeError)
	}
	if writtenBackup.Size == 0 {
		t.Fatal("local backup is empty")
	}

	backupDirectory, _ := localBackupDirectory()
	for _, oldBackupDate := range []string{"2026-01-01", "2026-01-02", "2026-01-03", "2026-01-04", "2026-01-05", "2026-01-06", "2026-01-07"} {
		os.WriteFile(localBackupFilePath(backupDirectory, oldBackupDate), []byte("old"), 0o644)
	}
	os.WriteFile(filepath.Join(backupDirectory, "notes.txt"), []byte("not a backup"), 0o644)
	pruneLocalBackups(backupDirectory)

	localBackups, listError := ListLocalBackups()
	if listError != nil {
		t.Fatal(listError)
	}
	if len(localBackups) != localBackupsToKeep {
		t.Fatalf("expected %d backups after pruning, got %d", localBackupsToKeep, len(localBackups))
	}
	if localBackups[0].Date != writtenBackup.Date {
		t.Fatalf("newest backup should be first, got %s", localBackups[0].Date)
	}
	_, oldestStatError := os.Stat(localBackupFilePath(backupDirectory, "2026-01-01"))
	if oldestStatError == nil {
		t.Fatal("oldest backup was not pruned")
	}

	targetDatabasePath := filepath.Join(testDirectory, "balce.db")
	closedEmptyDatabase(t, targetDatabasePath)

	restoreError := StageLocalRestore(targetDatabasePath, writtenBackup.Date)
	if restoreError != nil {
		t.Fatal(restoreError)
	}
	applyError := ApplyPendingRestore(targetDatabasePath)
	if applyError != nil {
		t.Fatal(applyError)
	}
	restoredNote := readRestoredNote(t, targetDatabasePath)
	if restoredNote != "offline sale" {
		t.Fatalf("restored database has %q", restoredNote)
	}

	if StageLocalRestore(targetDatabasePath, "../../etc") == nil {
		t.Fatal("a non-date backup name must be rejected")
	}
	if StageLocalRestore(targetDatabasePath, "2026-01-02") == nil {
		t.Fatal("a file that is not a database must not be staged")
	}
	_, pendingStatError := os.Stat(PendingRestorePath(targetDatabasePath))
	if pendingStatError == nil {
		t.Fatal("a rejected restore left a pending file behind")
	}
}
