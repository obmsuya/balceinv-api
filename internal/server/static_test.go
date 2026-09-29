package server_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/common/storage"
	"github.com/chrisostomemataba/balceinv-api/internal/config"
	"github.com/chrisostomemataba/balceinv-api/internal/server"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
)

func TestStaticAppFallsBackToIndexButNeverForApi(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		if engineCase.Engine != config.EngineSqlite {
			return
		}
		staticDirectory := t.TempDir()
		os.MkdirAll(filepath.Join(staticDirectory, "_nuxt"), 0o755)
		os.WriteFile(filepath.Join(staticDirectory, "index.html"), []byte("<html>balce app</html>"), 0o644)
		os.WriteFile(filepath.Join(staticDirectory, "_nuxt", "entry.js"), []byte("console.log('balce')"), 0o644)
		secretPath := filepath.Join(filepath.Dir(staticDirectory), "secret.txt")
		os.WriteFile(secretPath, []byte("do not serve"), 0o644)

		objectStore, _ := storage.NewLocalStore(t.TempDir())
		testConfig := &config.Config{
			Engine:          engineCase.Engine,
			AllowedOrigins:  []string{"http://localhost:3000"},
			StaticDirectory: staticDirectory,
		}
		application := server.New(testConfig, testkit.OpenMigrated(t, engineCase), objectStore, func() {}, server.Desktop{})

		fetch := func(requestPath string) (int, string, string) {
			testResponse, testError := application.Test(httptest.NewRequest(http.MethodGet, requestPath, nil), 5000)
			if testError != nil {
				t.Fatalf("GET %s: %v", requestPath, testError)
			}
			bodyBytes, _ := io.ReadAll(testResponse.Body)
			return testResponse.StatusCode, testResponse.Header.Get("Content-Type"), string(bodyBytes)
		}

		for _, deepLink := range []string{"/", "/pos", "/receipts/0192-abc", "/settings?tab=network"} {
			status, _, body := fetch(deepLink)
			if status != http.StatusOK || body != "<html>balce app</html>" {
				t.Fatalf("%s returned %d %q", deepLink, status, body)
			}
		}
		scriptStatus, scriptType, scriptBody := fetch("/_nuxt/entry.js")
		if scriptStatus != http.StatusOK || !strings.Contains(scriptType, "javascript") || scriptBody != "console.log('balce')" {
			t.Fatalf("the script returned %d %s %q", scriptStatus, scriptType, scriptBody)
		}
		if missingStatus, _, missingBody := fetch("/_nuxt/gone.js"); missingStatus != http.StatusNotFound || strings.Contains(missingBody, "<html>") {
			t.Fatalf("a missing script returned %d %q", missingStatus, missingBody)
		}
		for _, apiPath := range []string{"/api/nothing-here", "/api", "/api/products/unknown/deeper"} {
			apiStatus, apiType, apiBody := fetch(apiPath)
			if apiStatus == http.StatusOK || strings.Contains(apiBody, "<html>") || !strings.Contains(apiType, "json") {
				t.Fatalf("%s returned %d %s %q", apiPath, apiStatus, apiType, apiBody)
			}
		}
		if platformStatus, _, platformBody := fetch("/api/platform"); platformStatus != http.StatusOK || !strings.Contains(platformBody, `"mode":"desktop"`) {
			t.Fatalf("the platform route returned %d %q", platformStatus, platformBody)
		}
		for _, escapePath := range []string{"/../secret.txt", "/..%2fsecret.txt", "/_nuxt/../../secret.txt"} {
			_, _, escapeBody := fetch(escapePath)
			if strings.Contains(escapeBody, "do not serve") {
				t.Fatalf("%s served a file outside the app folder", escapePath)
			}
		}
	})
}

func TestOwnerSwitchesTheNetworkSetting(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		if engineCase.Engine != config.EngineSqlite {
			cloudHarness := apptest.Start(t, engineCase)
			company := cloudHarness.CreateCompany("Cloud Net", "owner@cloudnet.test")
			if cloudHarness.Call(http.MethodPut, "/api/platform/network", company.OwnerToken, map[string]any{"lan_enabled": true}).Status != http.StatusNotFound {
				t.Fatal("the network switch exists in cloud mode")
			}
			return
		}
		harness := apptest.StartDesktop(t, engineCase)
		company := harness.CreateCompany("Net Shop", "owner@net.test")
		managerToken := harness.CreateStaff(company, "manager@net.test", []string{"settings:edit"}, nil)

		before := harness.Call(http.MethodGet, "/api/platform", "", nil).Data()
		if before["lan_available"] != true || before["lan_enabled"] != false || len(before["lan_urls"].([]any)) != 0 {
			t.Fatalf("platform before switching was %v", before)
		}
		if harness.Call(http.MethodPut, "/api/platform/network", managerToken, map[string]any{"lan_enabled": true}).Status != http.StatusForbidden {
			t.Fatal("a manager switched the network on")
		}
		if harness.Call(http.MethodPut, "/api/platform/network", company.OwnerToken, map[string]any{}).Status != http.StatusBadRequest {
			t.Fatal("an empty network request was accepted")
		}
		switched := harness.Call(http.MethodPut, "/api/platform/network", company.OwnerToken, map[string]any{"lan_enabled": true})
		if switched.Status != http.StatusOK || switched.Data()["lan_enabled"] != true {
			t.Fatalf("switching on returned %d %v", switched.Status, switched.Body)
		}
		settingsBytes, readError := os.ReadFile(filepath.Join(harness.Config.DataDirectory, "network.json"))
		if readError != nil || !strings.Contains(string(settingsBytes), `"lan_enabled":true`) {
			t.Fatalf("the setting was not saved: %q %v", settingsBytes, readError)
		}
	})
}
