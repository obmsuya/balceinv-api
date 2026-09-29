package main

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/common/logging"
	"github.com/chrisostomemataba/balceinv-api/internal/common/storage"
	"github.com/chrisostomemataba/balceinv-api/internal/config"
	"github.com/chrisostomemataba/balceinv-api/internal/server"
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

	objectStore, storageError := storage.Open(loadedConfig)
	if storageError != nil {
		slog.Error("startup aborted: object storage unavailable", "error", storageError)
		os.Exit(1)
	}

	application := server.New(loadedConfig, openDatabase, objectStore, logFileWriter.WriteSeparator)

	shutdownSignals := make(chan os.Signal, 1)
	signal.Notify(shutdownSignals, syscall.SIGINT, syscall.SIGTERM)

	listenErrors := make(chan error, 1)
	go func() {
		slog.Info("listening", "address", loadedConfig.ListenAddress)
		listenErrors <- application.Listen(loadedConfig.ListenAddress)
	}()

	select {
	case listenError := <-listenErrors:
		slog.Error("server stopped unexpectedly", "error", listenError)
	case receivedSignal := <-shutdownSignals:
		slog.Info("shutting down", "signal", receivedSignal.String())
	}

	shutdownError := application.ShutdownWithTimeout(15 * time.Second)
	if shutdownError != nil {
		slog.Error("graceful shutdown incomplete", "error", shutdownError)
	}
}
