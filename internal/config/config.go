package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"github.com/chrisostomemataba/balceinv-api/license"
	"github.com/joho/godotenv"
)

type Engine string

const (
	EnginePostgres Engine = "postgres"
	EngineSqlite   Engine = "sqlite"
)

type Config struct {
	Engine         Engine
	DatabaseUrl    string
	SqlitePath     string
	ListenAddress  string
	AllowedOrigins []string
	LogDirectory   string
	StaticDirectory string
}

type LookupFunc func(key string) (string, bool)

func Load() (*Config, error) {
	envFileExists := fileExists(".env")
	if envFileExists {
		loadEnvFileError := godotenv.Load()
		if loadEnvFileError != nil {
			return nil, fmt.Errorf("failed to read .env: %w", loadEnvFileError)
		}
	}

	return LoadFrom(os.LookupEnv)
}

func LoadFrom(lookup LookupFunc) (*Config, error) {
	problems := []string{}

	databaseUrl := readTrimmed(lookup, "DATABASE_URL")
	sqlitePath := readTrimmed(lookup, "DB_PATH")

	isPostgres := databaseUrl != ""
	isSqliteExplicit := sqlitePath != ""
	if isPostgres && isSqliteExplicit {
		problems = append(problems, "set only one of DATABASE_URL (cloud) or DB_PATH (desktop), not both")
	}

	engine := EngineSqlite
	if isPostgres {
		engine = EnginePostgres
	}

	if engine == EngineSqlite && !isSqliteExplicit {
		defaultPath, defaultPathError := defaultSqlitePath()
		if defaultPathError != nil {
			problems = append(problems, fmt.Sprintf("DB_PATH not set and app data directory unavailable: %v", defaultPathError))
		}
		sqlitePath = defaultPath
	}

	allowedOrigins := splitList(readTrimmed(lookup, "ALLOWED_ORIGINS"))
	hasAllowedOrigins := len(allowedOrigins) > 0
	if engine == EnginePostgres && !hasAllowedOrigins {
		problems = append(problems, "ALLOWED_ORIGINS is required in cloud mode (comma separated, e.g. https://app.example.com)")
	}
	if engine == EngineSqlite && !hasAllowedOrigins {
		allowedOrigins = []string{
			"http://localhost:3000",
			"tauri://localhost",
			"https://tauri.localhost",
			"http://tauri.localhost",
		}
	}

	listenAddress := readTrimmed(lookup, "LISTEN_ADDR")
	if listenAddress == "" {
		listenAddress = "127.0.0.1:8080"
	}

	logDirectory := readTrimmed(lookup, "LOG_DIR")
	if logDirectory == "" {
		logDirectory = defaultLogDirectory(engine, sqlitePath)
	}

	hasProblems := len(problems) > 0
	if hasProblems {
		return nil, errors.New("invalid configuration:\n  - " + strings.Join(problems, "\n  - "))
	}

	loadedConfig := &Config{
		Engine:          engine,
		DatabaseUrl:     databaseUrl,
		SqlitePath:      sqlitePath,
		ListenAddress:   listenAddress,
		AllowedOrigins:  allowedOrigins,
		LogDirectory:    logDirectory,
		StaticDirectory: readTrimmed(lookup, "BALCE_STATIC_DIR"),
	}

	return loadedConfig, nil
}

func readTrimmed(lookup LookupFunc, key string) string {
	rawValue, isSet := lookup(key)
	if !isSet {
		return ""
	}
	return strings.TrimSpace(rawValue)
}

func splitList(rawList string) []string {
	items := []string{}
	for _, rawItem := range strings.Split(rawList, ",") {
		trimmedItem := strings.TrimSpace(rawItem)
		if trimmedItem != "" {
			items = append(items, trimmedItem)
		}
	}
	return items
}

func defaultSqlitePath() (string, error) {
	appDataDirectory, appDataDirectoryError := license.GetAppDataDirectory()
	if appDataDirectoryError != nil {
		return "", appDataDirectoryError
	}
	return filepath.Join(appDataDirectory, "balce.db"), nil
}

func defaultLogDirectory(engine Engine, sqlitePath string) string {
	if engine == EngineSqlite && sqlitePath != "" {
		return filepath.Join(filepath.Dir(sqlitePath), "logs")
	}
	return "logs"
}

func fileExists(path string) bool {
	_, statError := os.Stat(path)
	return statError == nil
}
