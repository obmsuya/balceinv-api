package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/backup"
	"github.com/chrisostomemataba/balceinv-api/internal/catalog"
	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/logging"
	"github.com/chrisostomemataba/balceinv-api/internal/common/storage"
	"github.com/chrisostomemataba/balceinv-api/internal/config"
	"github.com/chrisostomemataba/balceinv-api/internal/lan"
	"github.com/chrisostomemataba/balceinv-api/internal/server"
	"github.com/chrisostomemataba/balceinv-api/license"
)

func main() {
	loadedConfig, configError := config.Load()
	if configError != nil {
		fmt.Fprintln(os.Stderr, configError)
		os.Exit(1)
	}

	logFileWriter, loggingError := logging.Setup(loadedConfig.LogDirectory)
	if loggingError != nil {
		fmt.Fprintln(os.Stderr, loggingError)
		os.Exit(1)
	}
	defer logFileWriter.Close()

	startupContext, cancelStartup := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancelStartup()

	if loadedConfig.IsDesktop() {
		isRestored, restoreError := backup.ApplyPendingRestore(loadedConfig.SqlitePath)
		if restoreError != nil {
			slog.Error("the staged restore was not applied", "error", restoreError)
		}
		if isRestored {
			slog.Info("a backup was restored before start")
		}
	}

	migrationResult, migrateError := database.MigrateUp(startupContext, loadedConfig.Engine, loadedConfig.DatabaseUrl, loadedConfig.SqlitePath)
	if migrateError != nil {
		slog.Error("startup aborted: migrations failed", "error", migrateError)
		os.Exit(1)
	}
	slog.Info("schema ready",
		"engine", loadedConfig.Engine,
		"previousVersion", migrationResult.PreviousVersion,
		"currentVersion", migrationResult.CurrentVersion,
		"preMigrationCopy", migrationResult.PreMigrationCopy,
	)

	openDatabase, openDatabaseError := database.Open(startupContext, loadedConfig.Engine, loadedConfig.DatabaseUrl, loadedConfig.SqlitePath)
	if openDatabaseError != nil {
		slog.Error("startup aborted: database unavailable", "error", openDatabaseError)
		os.Exit(1)
	}
	defer openDatabase.Close()

	seededCount, seedError := seedCommonProducts(startupContext, openDatabase)
	if seedError != nil {
		slog.Error("startup aborted: common products could not be seeded", "error", seedError)
		os.Exit(1)
	}
	slog.Info("common products ready", "seeded", seededCount)

	objectStore, storageError := storage.Open(loadedConfig)
	if storageError != nil {
		slog.Error("startup aborted: object storage unavailable", "error", storageError)
		os.Exit(1)
	}

	desktop := server.Desktop{}
	backgroundContext, stopBackground := context.WithCancel(context.Background())
	defer stopBackground()
	if loadedConfig.IsDesktop() {
		desktop.Backups = backup.NewStore(openDatabase, loadedConfig.SqlitePath, loadedConfig.DataDirectory)
		desktop.Backups.StartAutomaticBackups(backgroundContext)
	}
	if loadedConfig.EnforceLicense {
		if license.LicenseSecret == "" {
			license.LicenseSecret = loadedConfig.LicenseSecret
		}
		license.StartTimestampWriter()
		go license.SyncWithDjango()
	}

	shutdownSignals := make(chan os.Signal, 1)
	signal.Notify(shutdownSignals, syscall.SIGINT, syscall.SIGTERM)
	restartRequests := make(chan struct{}, 1)

	for {
		listenAddress := loadedConfig.ListenAddress
		if loadedConfig.IsDesktop() {
			listenAddress = lan.ListenAddress(loadedConfig.ListenAddress, loadedConfig.ListenAddressIsExplicit, lan.LoadSettings(loadedConfig.DataDirectory))
			desktop.Network = lan.NewController(loadedConfig.DataDirectory, listenAddress, loadedConfig.ListenAddressIsExplicit, restartRequests)
		}
		application := server.New(loadedConfig, openDatabase, objectStore, logFileWriter.WriteSeparator, desktop)

		listenErrors := make(chan error, 1)
		go func() {
			slog.Info("listening", "address", listenAddress)
			listenErrors <- application.Listen(listenAddress)
		}()

		shouldRestart := false
		select {
		case listenError := <-listenErrors:
			slog.Error("server stopped unexpectedly", "error", listenError, "address", listenAddress)
			isLanListener := loadedConfig.IsDesktop() && lan.LoadSettings(loadedConfig.DataDirectory).LanEnabled && !loadedConfig.ListenAddressIsExplicit
			if isLanListener {
				slog.Warn("turning the network setting off so this computer can still use Balce")
				saveError := lan.SaveSettings(loadedConfig.DataDirectory, lan.Settings{LanEnabled: false})
				shouldRestart = saveError == nil
			}
		case receivedSignal := <-shutdownSignals:
			slog.Info("shutting down", "signal", receivedSignal.String())
		case <-restartRequests:
			slog.Info("restarting the listener for the new network setting")
			shouldRestart = true
		}

		shutdownError := application.ShutdownWithTimeout(15 * time.Second)
		if shutdownError != nil {
			slog.Error("graceful shutdown incomplete", "error", shutdownError)
		}
		if !shouldRestart {
			return
		}
	}
}

func seedCommonProducts(ctx context.Context, openDatabase *database.Database) (int, error) {
	seedTransaction, beginError := openDatabase.Writer.BeginTx(ctx, nil)
	if beginError != nil {
		return 0, fmt.Errorf("failed to begin seeding: %w", beginError)
	}
	defer seedTransaction.Rollback()

	catalogService := catalog.NewService(catalog.NewRepository())
	seededCount, seedError := catalogService.SeedEmptyLists(ctx, seedTransaction)
	if seedError != nil {
		return 0, seedError
	}

	commitError := seedTransaction.Commit()
	if commitError != nil {
		return 0, fmt.Errorf("failed to commit seeding: %w", commitError)
	}
	return seededCount, nil
}
