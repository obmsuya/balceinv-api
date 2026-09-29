package config_test

import (
	"strings"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/config"
)

func lookupFrom(environment map[string]string) config.LookupFunc {
	return func(key string) (string, bool) {
		value, isSet := environment[key]
		return value, isSet
	}
}

func TestBothDatabasesSetIsRejected(t *testing.T) {
	_, loadError := config.LoadFrom(lookupFrom(map[string]string{
		"DATABASE_URL":    "postgres://x",
		"DB_PATH":         "/tmp/balce.db",
		"ALLOWED_ORIGINS": "https://app.example.com",
	}))

	if loadError == nil || !strings.Contains(loadError.Error(), "only one of DATABASE_URL") {
		t.Fatalf("expected a both-databases error, got %v", loadError)
	}
}

func TestCloudRequiresAllowedOrigins(t *testing.T) {
	_, loadError := config.LoadFrom(lookupFrom(map[string]string{
		"DATABASE_URL": "postgres://x",
	}))

	if loadError == nil || !strings.Contains(loadError.Error(), "ALLOWED_ORIGINS") {
		t.Fatalf("expected ALLOWED_ORIGINS to be required, got %v", loadError)
	}
}

func TestAllProblemsAreReportedTogether(t *testing.T) {
	_, loadError := config.LoadFrom(lookupFrom(map[string]string{
		"DATABASE_URL": "postgres://x",
		"DB_PATH":      "/tmp/balce.db",
	}))

	if loadError == nil {
		t.Fatal("expected an error")
	}
	reportsBoth := strings.Contains(loadError.Error(), "only one of DATABASE_URL") && strings.Contains(loadError.Error(), "ALLOWED_ORIGINS")
	if !reportsBoth {
		t.Fatalf("expected both problems in one error, got %v", loadError)
	}
}

func TestDesktopDefaults(t *testing.T) {
	loadedConfig, loadError := config.LoadFrom(lookupFrom(map[string]string{
		"DB_PATH": "/tmp/balce-desktop/balce.db",
	}))
	if loadError != nil {
		t.Fatalf("desktop config: %v", loadError)
	}

	if loadedConfig.Engine != config.EngineSqlite {
		t.Fatalf("engine %s, want sqlite", loadedConfig.Engine)
	}
	if loadedConfig.ListenAddress != "127.0.0.1:8080" {
		t.Fatalf("listen address %s must default to loopback only", loadedConfig.ListenAddress)
	}
	if loadedConfig.LogDirectory != "/tmp/balce-desktop/logs" {
		t.Fatalf("log directory %s, want next to the database", loadedConfig.LogDirectory)
	}
	if len(loadedConfig.AllowedOrigins) == 0 {
		t.Fatal("desktop must allow the tauri origins by default")
	}
}

func TestCloudParsesOriginList(t *testing.T) {
	loadedConfig, loadError := config.LoadFrom(lookupFrom(map[string]string{
		"DATABASE_URL":    "postgres://x",
		"ALLOWED_ORIGINS": " https://app.example.com , ,tauri://localhost ",
		"LISTEN_ADDR":     "0.0.0.0:8080",
	}))
	if loadError != nil {
		t.Fatalf("cloud config: %v", loadError)
	}

	if loadedConfig.Engine != config.EnginePostgres {
		t.Fatalf("engine %s, want postgres", loadedConfig.Engine)
	}
	if len(loadedConfig.AllowedOrigins) != 2 || loadedConfig.AllowedOrigins[0] != "https://app.example.com" {
		t.Fatalf("origins parsed as %v", loadedConfig.AllowedOrigins)
	}
}
