package database

import (
	"context"
	"database/sql"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"sync/atomic"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/config"
	_ "github.com/jackc/pgx/v5/stdlib"
	_ "modernc.org/sqlite"
)

type Querier interface {
	ExecContext(ctx context.Context, query string, args ...any) (sql.Result, error)
	QueryContext(ctx context.Context, query string, args ...any) (*sql.Rows, error)
	QueryRowContext(ctx context.Context, query string, args ...any) *sql.Row
}

type Database struct {
	Engine       config.Engine
	Writer       *sql.DB
	Reader       *sql.DB
	QueryCounter *atomic.Int64
}

func Open(ctx context.Context, engine config.Engine, databaseUrl string, sqlitePath string) (*Database, error) {
	if engine == config.EnginePostgres {
		return openPostgres(ctx, databaseUrl)
	}
	return openSqlite(ctx, sqlitePath)
}

func (database *Database) Close() error {
	closeWriterError := database.Writer.Close()
	if closeWriterError != nil {
		return fmt.Errorf("failed to close writer pool: %w", closeWriterError)
	}

	readerIsSeparatePool := database.Reader != database.Writer
	if readerIsSeparatePool {
		closeReaderError := database.Reader.Close()
		if closeReaderError != nil {
			return fmt.Errorf("failed to close reader pool: %w", closeReaderError)
		}
	}

	return nil
}

func (database *Database) IsPostgres() bool {
	return database.Engine == config.EnginePostgres
}

func openPostgres(ctx context.Context, databaseUrl string) (*Database, error) {
	postgresPool, openError := sql.Open("pgx", databaseUrl)
	if openError != nil {
		return nil, fmt.Errorf("failed to open postgres pool: %w", openError)
	}

	postgresPool.SetMaxOpenConns(10)
	postgresPool.SetMaxIdleConns(5)
	postgresPool.SetConnMaxLifetime(30 * time.Minute)
	postgresPool.SetConnMaxIdleTime(5 * time.Minute)

	pingError := postgresPool.PingContext(ctx)
	if pingError != nil {
		postgresPool.Close()
		return nil, fmt.Errorf("failed to reach postgres: %w", pingError)
	}

	openedDatabase := &Database{
		Engine: config.EnginePostgres,
		Writer: postgresPool,
		Reader: postgresPool,
	}

	return openedDatabase, nil
}

func openSqlite(ctx context.Context, sqlitePath string) (*Database, error) {
	createDirectoryError := ensureSqliteDirectory(sqlitePath)
	if createDirectoryError != nil {
		return nil, createDirectoryError
	}

	writerPool, openWriterError := sql.Open("sqlite", sqliteDsn(sqlitePath, false))
	if openWriterError != nil {
		return nil, fmt.Errorf("failed to open sqlite writer: %w", openWriterError)
	}
	writerPool.SetMaxOpenConns(1)
	writerPool.SetMaxIdleConns(1)
	writerPool.SetConnMaxLifetime(0)

	pingWriterError := writerPool.PingContext(ctx)
	if pingWriterError != nil {
		writerPool.Close()
		return nil, fmt.Errorf("failed to open sqlite file %s: %w", sqlitePath, pingWriterError)
	}

	readerPool, openReaderError := sql.Open("sqlite", sqliteDsn(sqlitePath, true))
	if openReaderError != nil {
		writerPool.Close()
		return nil, fmt.Errorf("failed to open sqlite reader: %w", openReaderError)
	}
	readerPool.SetMaxOpenConns(4)
	readerPool.SetMaxIdleConns(4)

	openedDatabase := &Database{
		Engine: config.EngineSqlite,
		Writer: writerPool,
		Reader: readerPool,
	}

	return openedDatabase, nil
}

func ensureSqliteDirectory(sqlitePath string) error {
	createDirectoryError := os.MkdirAll(filepath.Dir(sqlitePath), 0o700)
	if createDirectoryError != nil {
		return fmt.Errorf("failed to create database directory: %w", createDirectoryError)
	}
	return nil
}

func sqliteDsn(sqlitePath string, isReadOnly bool) string {
	parameters := url.Values{}
	parameters.Add("_pragma", "journal_mode(WAL)")
	parameters.Add("_pragma", "foreign_keys(1)")
	parameters.Add("_pragma", "busy_timeout(5000)")
	parameters.Add("_pragma", "synchronous(NORMAL)")
	parameters.Add("_time_format", "sqlite")

	if isReadOnly {
		parameters.Add("_pragma", "query_only(1)")
	} else {
		parameters.Add("_txlock", "immediate")
	}

	return "file:" + sqlitePath + "?" + parameters.Encode()
}
