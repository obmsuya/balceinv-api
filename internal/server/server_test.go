package server_test

import (
	"net/http/httptest"
	"testing"

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
