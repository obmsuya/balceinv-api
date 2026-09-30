package server_test

import (
	"net/http"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/config"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
)

func failSignIns(harness *apptest.Harness, visitorAddress string, attempts int) int {
	lastStatus := 0
	for attempt := 0; attempt < attempts; attempt++ {
		signIn := harness.Send(http.MethodPost, "/api/auth/login", "", map[string]any{"email": "owner@proxy.test", "password": "wrong-password-1"}, map[string]string{"CF-Connecting-IP": visitorAddress})
		lastStatus = signIn.Status
	}
	return lastStatus
}

func TestSignInLimitsFollowTheVisitorBehindTheTunnel(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.StartWith(t, engineCase, func(testConfig *config.Config) {
			testConfig.ProxyHeader = "CF-Connecting-IP"
			testConfig.TrustedProxies = []string{"0.0.0.0"}
		})
		harness.CreateCompany("Proxy Shop", "owner@proxy.test")

		if lockedStatus := failSignIns(harness, "196.249.1.10", 6); lockedStatus != http.StatusTooManyRequests {
			t.Fatalf("six wrong passwords from one visitor returned %d, want 429", lockedStatus)
		}
		if otherVisitorStatus := failSignIns(harness, "41.59.2.20", 1); otherVisitorStatus != http.StatusUnauthorized {
			t.Fatalf("another visitor was refused with %d; one person's mistakes locked everyone out", otherVisitorStatus)
		}
	})
}

func TestTheVisitorHeaderIsIgnoredFromAnUntrustedAddress(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.StartWith(t, engineCase, func(testConfig *config.Config) {
			testConfig.ProxyHeader = "CF-Connecting-IP"
			testConfig.TrustedProxies = []string{"172.31.250.1"}
		})
		harness.CreateCompany("Spoof Shop", "owner@proxy.test")

		failSignIns(harness, "196.249.1.10", 6)
		if spoofedStatus := failSignIns(harness, "41.59.2.20", 1); spoofedStatus != http.StatusTooManyRequests {
			t.Fatalf("a forged visitor header escaped the limit with %d", spoofedStatus)
		}
	})
}
