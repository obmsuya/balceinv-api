package licensing_test

import (
	"net/http"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/config"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/chrisostomemataba/balceinv-api/license"
)

func isolateLicense(t *testing.T) {
	homeDirectory := t.TempDir()
	t.Setenv("HOME", homeDirectory)
	t.Setenv("APPDATA", homeDirectory)
	t.Setenv("XDG_CONFIG_HOME", homeDirectory)
	previousBaseUrl := license.DjangoBaseURL
	license.DjangoBaseURL = "http://127.0.0.1:9"
	t.Cleanup(func() { license.DjangoBaseURL = previousBaseUrl })
}

func TestDesktopNeedsALicenseAndSetupStartsATrial(t *testing.T) {
	isolateLicense(t)
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		if engineCase.Engine != config.EngineSqlite {
			cloudHarness := apptest.Start(t, engineCase)
			if cloudHarness.Call(http.MethodGet, "/api/license/status", "", nil).Status != http.StatusUnauthorized {
				t.Fatal("the cloud license status answered without signing in")
			}
			return
		}

		harness := apptest.StartDesktopWithLicenseCheck(t, engineCase)

		for _, licensePath := range []string{"/api/license/status", "/api/license/hardware-id", "/api/license/packages"} {
			if harness.Call(http.MethodGet, licensePath, "", nil).Status != http.StatusUnauthorized {
				t.Fatalf("%s answered without signing in", licensePath)
			}
		}
		if harness.Call(http.MethodPost, "/api/license/pay", "", map[string]any{"phone": "0712345678", "provider": "Mpesa", "package_id": 1}).Status != http.StatusUnauthorized {
			t.Fatal("a payment was started without signing in")
		}
		if harness.Call(http.MethodGet, "/api/platform", "", nil).Status != http.StatusOK || harness.Call(http.MethodGet, "/health", "", nil).Status != http.StatusOK {
			t.Fatal("platform or health was locked without a license")
		}

		setup := harness.Call(http.MethodPost, "/api/setup", "", map[string]any{
			"business_name": "Trial Shop", "owner_name": "Trial Owner", "owner_email": "owner@trial.test", "owner_password": "trial-password",
		})
		if setup.Status >= http.StatusBadRequest {
			t.Fatalf("setup returned %d %v", setup.Status, setup.Body)
		}
		trialState, loadError := license.LoadLicenseState()
		if loadError != nil || !trialState.IsTrial {
			t.Fatalf("setup did not start a trial: %v %v", trialState, loadError)
		}

		ownerToken := harness.MustLogin("owner@trial.test", "trial-password")
		trialStatus := harness.Call(http.MethodGet, "/api/license/status", ownerToken, nil)
		if trialStatus.Status != http.StatusOK || trialStatus.Data()["licensed"] != true {
			t.Fatalf("status during the trial returned %d %v", trialStatus.Status, trialStatus.Body)
		}
		hardware := harness.Call(http.MethodGet, "/api/license/hardware-id", ownerToken, nil)
		hardwareId, _ := hardware.Body["hardware_id"].(string)
		if hardware.Status != http.StatusOK || len(hardwareId) < 16 {
			t.Fatalf("hardware id returned %d %v", hardware.Status, hardware.Body)
		}
		if harness.Call(http.MethodPost, "/api/license/refresh", ownerToken, nil).Status != http.StatusServiceUnavailable {
			t.Fatal("refresh without internet did not say so")
		}
		if harness.Call(http.MethodPost, "/api/license/pay", ownerToken, map[string]any{"phone": "", "provider": "Mpesa", "package_id": 1}).Status != http.StatusBadRequest {
			t.Fatal("a payment without a phone number was forwarded")
		}

		if products := harness.Call(http.MethodGet, "/api/products", ownerToken, nil); products.Status != http.StatusOK {
			t.Fatalf("a trial could not list products: %d", products.Status)
		}

		trialState.ExpiresAt = "2020-01-01T00:00:00Z"
		trialState.LastKnownTime = "2020-01-01T00:00:00Z"
		license.SaveLicenseState(trialState)
		expired := harness.Call(http.MethodGet, "/api/products", ownerToken, nil)
		if expired.Status != http.StatusPaymentRequired || expired.Body["error"] != "subscription_required" {
			t.Fatalf("an expired license returned %d %v", expired.Status, expired.Body)
		}
		if harness.Call(http.MethodPost, "/api/auth/logout", ownerToken, nil).Status == http.StatusPaymentRequired {
			t.Fatal("signing out was locked")
		}
	})
}

func TestUnlicensedDesktopIsLockedButTestsCanOptOut(t *testing.T) {
	isolateLicense(t)
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		if engineCase.Engine != config.EngineSqlite {
			return
		}
		lockedHarness := apptest.StartDesktopWithLicenseCheck(t, engineCase)
		company := lockedHarness.CreateCompany("Locked Shop", "owner@locked.test")
		if lockedHarness.Call(http.MethodGet, "/api/products", company.OwnerToken, nil).Status != http.StatusPaymentRequired {
			t.Fatal("a desktop with no license served products")
		}

		loadedConfig, configError := config.LoadFrom(func(key string) (string, bool) {
			values := map[string]string{"DB_PATH": engineCase.SqlitePath, "BALCE_LICENSE_CHECK": "off"}
			value, isSet := values[key]
			return value, isSet
		})
		if configError != nil || loadedConfig.EnforceLicense || !loadedConfig.IsDesktop() {
			t.Fatalf("BALCE_LICENSE_CHECK=off gave %+v %v", loadedConfig, configError)
		}
		defaultConfig, _ := config.LoadFrom(func(key string) (string, bool) {
			if key == "DB_PATH" {
				return engineCase.SqlitePath, true
			}
			return "", false
		})
		if !defaultConfig.EnforceLicense {
			t.Fatal("a desktop does not check the license by default")
		}
	})
}
