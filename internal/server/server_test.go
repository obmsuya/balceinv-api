package server_test

import (
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/common/httpx"
	"github.com/chrisostomemataba/balceinv-api/internal/common/storage"
	"github.com/chrisostomemataba/balceinv-api/internal/config"
	"github.com/chrisostomemataba/balceinv-api/internal/server"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
)

func TestHealthReflectsDatabaseReachability(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		openDatabase := testkit.OpenMigrated(t, engineCase)
		testConfig := &config.Config{
			Engine:         engineCase.Engine,
			AllowedOrigins: []string{"http://localhost:3000"},
		}
		objectStore, storeError := storage.NewLocalStore(t.TempDir())
		if storeError != nil {
			t.Fatalf("object store: %v", storeError)
		}
		application := server.New(testConfig, openDatabase, objectStore, func() {}, server.Desktop{})

		healthyResponse, healthyError := application.Test(httptest.NewRequest("GET", "/health", nil), 5000)
		if healthyError != nil {
			t.Fatalf("health request: %v", healthyError)
		}
		if healthyResponse.StatusCode != 200 {
			t.Fatalf("health returned %d with a live database", healthyResponse.StatusCode)
		}
		if healthyResponse.Header.Get("X-Request-Id") == "" {
			t.Fatal("every response must carry X-Request-Id")
		}

		openDatabase.Close()

		downResponse, downError := application.Test(httptest.NewRequest("GET", "/health", nil), 5000)
		if downError != nil {
			t.Fatalf("health request after close: %v", downError)
		}
		if downResponse.StatusCode != 503 {
			t.Fatalf("health returned %d with a closed database, want 503", downResponse.StatusCode)
		}
	})
}

func TestDesktopScreenMayCallTheApiFromEveryWebviewOrigin(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		openDatabase := testkit.OpenMigrated(t, engineCase)
		webviewOrigins := []string{"tauri://localhost", "http://tauri.localhost", "https://tauri.localhost"}
		testConfig := &config.Config{
			Engine:         engineCase.Engine,
			AllowedOrigins: webviewOrigins,
		}
		objectStore, storeError := storage.NewLocalStore(t.TempDir())
		if storeError != nil {
			t.Fatalf("object store: %v", storeError)
		}
		application := server.New(testConfig, openDatabase, objectStore, func() {}, server.Desktop{})

		headersTheScreenSends := []string{"Content-Type", "Authorization", httpx.DesktopClientHeader, httpx.SupportPasscodeHeader}
		for _, webviewOrigin := range webviewOrigins {
			preflightRequest := httptest.NewRequest("OPTIONS", "/api/auth/login", nil)
			preflightRequest.Header.Set("Origin", webviewOrigin)
			preflightRequest.Header.Set("Access-Control-Request-Method", "POST")
			preflightRequest.Header.Set("Access-Control-Request-Headers", strings.ToLower(strings.Join(headersTheScreenSends, ",")))

			preflightResponse, preflightError := application.Test(preflightRequest, 5000)
			if preflightError != nil {
				t.Fatalf("preflight from %s: %v", webviewOrigin, preflightError)
			}
			if preflightResponse.Header.Get("Access-Control-Allow-Origin") != webviewOrigin {
				t.Fatalf("preflight from %s allowed origin %q", webviewOrigin, preflightResponse.Header.Get("Access-Control-Allow-Origin"))
			}
			allowedHeaders := strings.ToLower(preflightResponse.Header.Get("Access-Control-Allow-Headers"))
			for _, sentHeader := range headersTheScreenSends {
				if !strings.Contains(allowedHeaders, strings.ToLower(sentHeader)) {
					t.Fatalf("the webview blocks sign-in from %s: %s is not in the allowed headers %q", webviewOrigin, sentHeader, allowedHeaders)
				}
			}
		}
	})
}
