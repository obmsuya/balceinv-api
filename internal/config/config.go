package config

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/chrisostomemataba/balceinv-api/license"
	"github.com/joho/godotenv"
)

var CompiledSupportPasscodeHash = ""

var CompiledSupportSmtpPassword = ""

type Engine string

const (
	EnginePostgres Engine = "postgres"
	EngineSqlite   Engine = "sqlite"
)

type Config struct {
	Engine      Engine
	DatabaseUrl string
	SqlitePath  string

	MigrationDatabaseUrl string
	ProxyHeader          string
	TrustedProxies       []string

	ListenAddress string

	ListenAddressIsExplicit bool
	AllowedOrigins          []string
	LogDirectory            string
	StaticDirectory         string
	MediaDirectory          string
	DataDirectory           string
	EnforceLicense          bool
	ExitWithParent          bool
	LicenseSecret           string
	S3Endpoint              string
	S3Bucket                string
	S3Region                string
	S3AccessKeyId           string
	S3SecretKey             string

	SupportPasscodeHash string

	SupportSmtpHost     string
	SupportSmtpPort     int
	SupportSmtpUsername string
	SupportSmtpPassword string
	SupportEmailTo      string
	SupportEmailFrom    string
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

	migrationDatabaseUrl := readOrDefault(lookup, "MIGRATION_DATABASE_URL", databaseUrl)
	if engine == EngineSqlite && readTrimmed(lookup, "MIGRATION_DATABASE_URL") != "" {
		problems = append(problems, "MIGRATION_DATABASE_URL is for the cloud database only; leave it empty on the desktop")
	}

	proxyHeader := readTrimmed(lookup, "PROXY_HEADER")
	trustedProxies := splitList(readTrimmed(lookup, "TRUSTED_PROXIES"))
	hasProxyHeader := proxyHeader != ""
	hasTrustedProxies := len(trustedProxies) > 0
	if hasProxyHeader != hasTrustedProxies {
		problems = append(problems, "set PROXY_HEADER and TRUSTED_PROXIES together (e.g. CF-Connecting-IP and the address the tunnel connects from)")
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
	isListenAddressExplicit := listenAddress != ""
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

	dataDirectory := ""
	if engine == EngineSqlite && sqlitePath != "" {
		dataDirectory = filepath.Dir(sqlitePath)
	}

	supportPasscodeHash := readTrimmed(lookup, "BALCE_SUPPORT_PASSCODE_HASH")
	if supportPasscodeHash == "" {
		supportPasscodeHash = CompiledSupportPasscodeHash
	}

	supportSmtpHost := readOrDefault(lookup, "SUPPORT_SMTP_HOST", "smtp.mail.yahoo.com")
	supportSmtpPort, supportSmtpPortError := strconv.Atoi(readOrDefault(lookup, "SUPPORT_SMTP_PORT", "465"))
	isSupportSmtpPortValid := supportSmtpPortError == nil && supportSmtpPort > 0 && supportSmtpPort <= 65535
	if !isSupportSmtpPortValid {
		problems = append(problems, "SUPPORT_SMTP_PORT must be a port number such as 465 or 587")
	}
	supportSmtpUsername := readOrDefault(lookup, "SUPPORT_SMTP_USERNAME", "obmsuya@yahoo.com")
	supportSmtpPassword := readOrDefault(lookup, "SUPPORT_SMTP_PASSWORD", CompiledSupportSmtpPassword)
	supportEmailTo := readOrDefault(lookup, "SUPPORT_EMAIL_TO", "obmsuya@gmail.com")
	supportEmailFrom := readOrDefault(lookup, "SUPPORT_EMAIL_FROM", supportSmtpUsername)

	hasProblems := len(problems) > 0
	if hasProblems {
		return nil, errors.New("invalid configuration:\n  - " + strings.Join(problems, "\n  - "))
	}

	loadedConfig := &Config{
		Engine:        engine,
		DatabaseUrl:   databaseUrl,
		SqlitePath:    sqlitePath,
		ListenAddress: listenAddress,

		MigrationDatabaseUrl: migrationDatabaseUrl,
		ProxyHeader:          proxyHeader,
		TrustedProxies:       trustedProxies,

		ListenAddressIsExplicit: isListenAddressExplicit,
		AllowedOrigins:          allowedOrigins,
		LogDirectory:            logDirectory,
		StaticDirectory:         readTrimmed(lookup, "BALCE_STATIC_DIR"),
		MediaDirectory:          mediaDirectory,
		DataDirectory:           dataDirectory,
		EnforceLicense:          dataDirectory != "" && readTrimmed(lookup, "BALCE_LICENSE_CHECK") != "off",
		ExitWithParent:          readTrimmed(lookup, "BALCE_EXIT_WITH_PARENT") == "1",
		LicenseSecret:           readTrimmed(lookup, "BALCE_LICENSE_SECRET"),
		S3Endpoint:              s3Endpoint,
		S3Bucket:                s3Bucket,
		S3Region:                s3Region,
		S3AccessKeyId:           s3AccessKeyId,
		S3SecretKey:             s3SecretKey,

		SupportPasscodeHash: supportPasscodeHash,

		SupportSmtpHost:     supportSmtpHost,
		SupportSmtpPort:     supportSmtpPort,
		SupportSmtpUsername: supportSmtpUsername,
		SupportSmtpPassword: supportSmtpPassword,
		SupportEmailTo:      supportEmailTo,
		SupportEmailFrom:    supportEmailFrom,
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

func readOrDefault(lookup LookupFunc, key string, defaultValue string) string {
	trimmedValue := readTrimmed(lookup, key)
	if trimmedValue == "" {
		return defaultValue
	}
	return trimmedValue
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

func (loadedConfig *Config) IsDesktop() bool {
	return loadedConfig.Engine == EngineSqlite && loadedConfig.DataDirectory != ""
}
