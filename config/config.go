package config

import (
	"log"
	"os"
	"path/filepath"

	"github.com/chrisostomemataba/balceinv-api/license"
	"github.com/joho/godotenv"
)

// CompiledAccessTokenSecret and CompiledRefreshTokenSecret are injected at
// compile time via -ldflags for release builds, the same way
// license.LicenseSecret is. They exist because the packaged desktop app
// ships with no .env file and no environment variables set for the sidecar
// process — without a compiled-in fallback, every shipped install would
// sign JWTs with an empty secret.
var CompiledAccessTokenSecret = ""
var CompiledRefreshTokenSecret = ""

// Config holds every value the application needs from the environment.
// All other packages receive this struct — they never call os.Getenv themselves.
type Config struct {
	Port               string
	DBPath             string
	AccessTokenSecret  string
	RefreshTokenSecret string
	LicenseSecret      string
	Env                string
}

// Load reads the .env file and returns a populated Config struct.
// If a value is missing from .env, a safe default is used where possible.
// For secrets, the app logs a warning so you know to fix it before production.
func Load() *Config {
	// godotenv.Load() fills os environment from .env file.
	// If the file does not exist (e.g. in a Docker container where env vars
	// are injected directly), that is fine — we just read from the system env.
	if err := godotenv.Load(); err != nil {
		log.Println("No .env file found, reading from system environment")
	}

	cfg := &Config{
		Port:               getEnv("PORT", "8080"),
		DBPath:             getEnv("DB_PATH", defaultDBPath()),
		AccessTokenSecret:  getEnv("ACCESS_TOKEN_SECRET", CompiledAccessTokenSecret),
		RefreshTokenSecret: getEnv("REFRESH_TOKEN_SECRET", CompiledRefreshTokenSecret),
		LicenseSecret:      getEnv("BALCE_LICENSE_SECRET", ""),
		Env:                getEnv("ENV", "development"),
	}

	// Warn loudly if secrets are missing — a server running without proper
	// JWT secrets will sign tokens with an empty string, which is a security hole.
	if cfg.AccessTokenSecret == "" || cfg.RefreshTokenSecret == "" {
		log.Println("WARNING: JWT secrets are not set. Set ACCESS_TOKEN_SECRET and REFRESH_TOKEN_SECRET in your .env file.")
	}

	return cfg
}

// getEnv reads a single environment variable and returns a fallback
// value if that variable is not set. This is a small helper that keeps
// the Load() function above clean and readable.
func getEnv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}

// defaultDBPath resolves the database into the same OS app-data directory
// license.json already lives in, instead of a path relative to the sidecar
// binary's own location. The installed app's directory (Contents/MacOS on
// macOS, the Program Files install dir on Windows) is not writable, so a
// relative path like "./balce.db" fails to open there — database.Connect()
// errors, main.go calls log.Fatalf, and the sidecar process exits within
// milliseconds of starting, before it ever binds its port. Falls back to
// the old relative path only if the OS directory can't be resolved.
func defaultDBPath() string {
	appDataDir, err := license.GetAppDataDirectory()
	if err != nil {
		return "./balce.db"
	}
	return filepath.Join(appDataDir, "balce.db")
}
