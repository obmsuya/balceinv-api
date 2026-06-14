package config

import (
	"log"
	"os"

	"github.com/joho/godotenv"
)

// Config holds every value the application needs from the environment.
// All other packages receive this struct — they never call os.Getenv themselves.
type Config struct {
	Port                string
	DBPath              string
	AccessTokenSecret   string
	RefreshTokenSecret  string
	Env                 string
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
		DBPath:             getEnv("DB_PATH", "./balce.db"),
		AccessTokenSecret:  getEnv("ACCESS_TOKEN_SECRET", ""),
		RefreshTokenSecret: getEnv("REFRESH_TOKEN_SECRET", ""),
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