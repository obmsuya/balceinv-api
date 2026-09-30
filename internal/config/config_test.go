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
		"DATABASE_URL":         "postgres://x",
		"ALLOWED_ORIGINS":      " https://app.example.com , ,tauri://localhost ",
		"LISTEN_ADDR":          "0.0.0.0:8080",
		"S3_ENDPOINT":          "http://garage:3900",
		"S3_BUCKET":            "balce-media",
		"S3_ACCESS_KEY_ID":     "key",
		"S3_SECRET_ACCESS_KEY": "secret",
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

func TestCloudRequiresCompleteObjectStorage(t *testing.T) {
	_, missingEndpointError := config.LoadFrom(lookupFrom(map[string]string{
		"DATABASE_URL":    "postgres://x",
		"ALLOWED_ORIGINS": "https://app.example.com",
	}))
	if missingEndpointError == nil || !strings.Contains(missingEndpointError.Error(), "S3_ENDPOINT") {
		t.Fatalf("expected S3_ENDPOINT to be required in cloud, got %v", missingEndpointError)
	}

	_, partialError := config.LoadFrom(lookupFrom(map[string]string{
		"DB_PATH":     "/tmp/balce/balce.sqlite",
		"S3_ENDPOINT": "http://garage:3900",
	}))
	if partialError == nil || !strings.Contains(partialError.Error(), "S3_BUCKET") || !strings.Contains(partialError.Error(), "S3_SECRET_ACCESS_KEY") {
		t.Fatalf("expected every missing S3 value to be listed, got %v", partialError)
	}
}

func TestDesktopKeepsMediaNextToTheDatabase(t *testing.T) {
	loadedConfig, loadError := config.LoadFrom(lookupFrom(map[string]string{
		"DB_PATH": "/tmp/balce-desktop/balce.sqlite",
	}))
	if loadError != nil {
		t.Fatalf("desktop config: %v", loadError)
	}
	if loadedConfig.MediaDirectory != "/tmp/balce-desktop/media" {
		t.Fatalf("media directory %s, want next to the database", loadedConfig.MediaDirectory)
	}
}

func TestSupportEmailDefaultsAndOverrides(t *testing.T) {
	defaultConfig, defaultError := config.LoadFrom(lookupFrom(map[string]string{
		"DB_PATH": "/tmp/balce-desktop/balce.db",
	}))
	if defaultError != nil {
		t.Fatalf("desktop config: %v", defaultError)
	}
	usesDefaults := defaultConfig.SupportSmtpHost == "smtp.mail.yahoo.com" && defaultConfig.SupportSmtpPort == 465 &&
		defaultConfig.SupportSmtpUsername == "obmsuya@yahoo.com" && defaultConfig.SupportEmailTo == "obmsuya@gmail.com" &&
		defaultConfig.SupportEmailFrom == "obmsuya@yahoo.com" && defaultConfig.SupportSmtpPassword == config.CompiledSupportSmtpPassword
	if !usesDefaults {
		t.Fatalf("support email defaults are %+v", defaultConfig)
	}

	overriddenConfig, overrideError := config.LoadFrom(lookupFrom(map[string]string{
		"DB_PATH":               "/tmp/balce-desktop/balce.db",
		"SUPPORT_SMTP_HOST":     "smtp.example.com",
		"SUPPORT_SMTP_PORT":     "587",
		"SUPPORT_SMTP_USERNAME": "robot@example.com",
		"SUPPORT_SMTP_PASSWORD": " app-password ",
		"SUPPORT_EMAIL_TO":      "team@example.com",
	}))
	if overrideError != nil {
		t.Fatalf("overridden config: %v", overrideError)
	}
	isOverridden := overriddenConfig.SupportSmtpPort == 587 && overriddenConfig.SupportSmtpPassword == "app-password" &&
		overriddenConfig.SupportEmailFrom == "robot@example.com" && overriddenConfig.SupportEmailTo == "team@example.com"
	if !isOverridden {
		t.Fatalf("support email overrides are %+v", overriddenConfig)
	}

	_, badPortError := config.LoadFrom(lookupFrom(map[string]string{
		"DB_PATH":           "/tmp/balce-desktop/balce.db",
		"SUPPORT_SMTP_PORT": "smtp",
	}))
	if badPortError == nil || !strings.Contains(badPortError.Error(), "SUPPORT_SMTP_PORT") {
		t.Fatalf("expected a bad port error, got %v", badPortError)
	}
}

func TestProxySettingsComeTogether(t *testing.T) {
	_, loadError := config.LoadFrom(lookupFrom(map[string]string{
		"DATABASE_URL":         "postgres://x",
		"ALLOWED_ORIGINS":      "https://app.example.com",
		"S3_ENDPOINT":          "http://garage:3900",
		"S3_BUCKET":            "b",
		"S3_ACCESS_KEY_ID":     "k",
		"S3_SECRET_ACCESS_KEY": "s",
		"PROXY_HEADER":         "CF-Connecting-IP",
	}))
	if loadError == nil || !strings.Contains(loadError.Error(), "PROXY_HEADER and TRUSTED_PROXIES together") {
		t.Fatalf("a proxy header without trusted proxies was accepted: %v", loadError)
	}
}

func TestCloudMigratesWithItsOwnDatabaseUrl(t *testing.T) {
	cloudEnvironment := map[string]string{
		"DATABASE_URL":         "postgres://app@db/balce",
		"ALLOWED_ORIGINS":      "https://app.example.com",
		"S3_ENDPOINT":          "http://garage:3900",
		"S3_BUCKET":            "b",
		"S3_ACCESS_KEY_ID":     "k",
		"S3_SECRET_ACCESS_KEY": "s",
		"PROXY_HEADER":         "CF-Connecting-IP",
		"TRUSTED_PROXIES":      "172.31.250.1",
	}
	sameUrlConfig, sameUrlError := config.LoadFrom(lookupFrom(cloudEnvironment))
	if sameUrlError != nil || sameUrlConfig.MigrationDatabaseUrl != "postgres://app@db/balce" {
		t.Fatalf("migrations should default to DATABASE_URL: %v %v", sameUrlError, sameUrlConfig)
	}
	cloudEnvironment["MIGRATION_DATABASE_URL"] = "postgres://owner@db/balce"
	splitConfig, splitError := config.LoadFrom(lookupFrom(cloudEnvironment))
	if splitError != nil || splitConfig.MigrationDatabaseUrl != "postgres://owner@db/balce" || splitConfig.DatabaseUrl != "postgres://app@db/balce" {
		t.Fatalf("the owner url must be used only for migrations: %v", splitError)
	}
	if len(splitConfig.TrustedProxies) != 1 || splitConfig.ProxyHeader != "CF-Connecting-IP" {
		t.Fatalf("proxy settings were not read: %v", splitConfig.TrustedProxies)
	}

	_, desktopError := config.LoadFrom(lookupFrom(map[string]string{"DB_PATH": "/tmp/x/balce.sqlite", "MIGRATION_DATABASE_URL": "postgres://owner@db/balce"}))
	if desktopError == nil {
		t.Fatal("the desktop accepted a migration database url")
	}
}
