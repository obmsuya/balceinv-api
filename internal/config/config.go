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
	Engine          Engine
	DatabaseUrl     string
	SqlitePath      string
	ListenAddress   string
	AllowedOrigins  []string
	LogDirectory    string
	StaticDirectory string
	MediaDirectory  string
	S3Endpoint      string
	S3Bucket        string
	S3Region        string
	S3AccessKeyId   string
	S3SecretKey     string
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

	s3Endpoint := readTrimmed(lookup, "S3_ENDPOINT")
	s3Bucket := readTrimmed(lookup, "S3_BUCKET")
	s3AccessKeyId := readTrimmed(lookup, "S3_ACCESS_KEY_ID")
	s3SecretKey := readTrimmed(lookup, "S3_SECRET_ACCESS_KEY")
	s3Region := readTrimmed(lookup, "S3_REGION")
	if s3Region == "" {
		s3Region = "garage"
	}

	usesObjectStorage := s3Endpoint != ""
	if engine == EnginePostgres && !usesObjectStorage {
		problems = append(problems, "S3_ENDPOINT is required in cloud mode (object storage for logos and images)")
	}
	if usesObjectStorage {
		requiredS3Values := map[string]string{
			"S3_BUCKET":            s3Bucket,
			"S3_ACCESS_KEY_ID":     s3AccessKeyId,
			"S3_SECRET_ACCESS_KEY": s3SecretKey,
		}
		for _, requiredName := range []string{"S3_BUCKET", "S3_ACCESS_KEY_ID", "S3_SECRET_ACCESS_KEY"} {
			if requiredS3Values[requiredName] == "" {
				problems = append(problems, requiredName+" is required when S3_ENDPOINT is set")
			}
		}
	}

	mediaDirectory := readTrimmed(lookup, "MEDIA_DIR")
	if mediaDirectory == "" && engine == EngineSqlite && sqlitePath != "" {
		mediaDirectory = filepath.Join(filepath.Dir(sqlitePath), "media")
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
		MediaDirectory:  mediaDirectory,
		S3Endpoint:      s3Endpoint,
		S3Bucket:        s3Bucket,
		S3Region:        s3Region,
		S3AccessKeyId:   s3AccessKeyId,
		S3SecretKey:     s3SecretKey,
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
	return filepath.Join(appDataDirectory, "balce.sqlite"), nil
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
