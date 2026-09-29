package rates_test

import (
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/rates"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
)

type fakeProvider struct {
	server *httptest.Server
	mode   atomic.Value
	hits   atomic.Int64
}

func startFakeProvider(t *testing.T) *fakeProvider {
	fake := &fakeProvider{}
	fake.mode.Store("ok")
	fake.server = httptest.NewTLSServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		fake.hits.Add(1)
		switch fake.mode.Load().(string) {
		case "down":
			writer.WriteHeader(http.StatusServiceUnavailable)
		case "garbage":
			io.WriteString(writer, "<html>captive portal</html>")
		case "error":
			io.WriteString(writer, `{"result":"error","error-type":"quota-reached"}`)
		default:
			io.WriteString(writer, `{"result":"success","base_code":"USD","time_last_update_unix":1790640151,"rates":{"USD":1,"TZS":2500,"EUR":0.8,"GBP":0.5,"KES":125,"BAD":0}}`)
		}
	}))
	t.Cleanup(fake.server.Close)
	rates.UseProvider(fake.server.URL, fake.server.Client().Transport, 0)
	return fake
}

func rateOf(t *testing.T, ratesData map[string]any, code string) (float64, bool) {
	t.Helper()
	for _, rawRate := range ratesData["rates"].([]any) {
		rate := rawRate.(map[string]any)
		if rate["code"] == code {
			return rate["value"].(float64), true
		}
	}
	return 0, false
}

func TestExchangeRatesCacheGoStaleOfflineAndRecover(t *testing.T) {
	fake := startFakeProvider(t)
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Rates Shop", "owner@rates.test")
		cashierToken := harness.CreateStaff(company, "cashier@rates.test", []string{"sales:create"}, nil)

		for _, failingMode := range []string{"down", "garbage", "error"} {
			fake.mode.Store(failingMode)
			failed := harness.Call(http.MethodGet, "/api/exchange-rates", company.OwnerToken, nil)
			if failed.Status != http.StatusOK || failed.Data()["available"] != false || failed.Data()["problem"] == "" {
				t.Fatalf("with the provider %s the rates returned %d %v", failingMode, failed.Status, failed.Data())
			}
		}

		fake.mode.Store("ok")
		hitsBefore := fake.hits.Load()
		fresh := harness.Call(http.MethodGet, "/api/exchange-rates", cashierToken, nil).Data()
		usdRate, hasUsd := rateOf(t, fresh, "USD")
		eurRate, _ := rateOf(t, fresh, "EUR")
		kesRate, _ := rateOf(t, fresh, "KES")
		_, hasOwnCurrency := rateOf(t, fresh, "TZS")
		if fresh["available"] != true || fresh["base_currency"] != "TZS" || !hasUsd || usdRate != 2500 || eurRate != 3125 || kesRate != 20 || hasOwnCurrency || fresh["is_stale"] != false || fresh["provider_updated_at"] == nil {
			t.Fatalf("fresh rates were %v", fresh)
		}
		harness.Call(http.MethodGet, "/api/exchange-rates", company.OwnerToken, nil)
		if fake.hits.Load() != hitsBefore+1 {
			t.Fatalf("fresh rates were fetched %d times, want once", fake.hits.Load()-hitsBefore)
		}

		harness.ExecForCompany(company.Id, `UPDATE exchange_rate_snapshots SET fetched_at = $1`, time.Now().UTC().Add(-7*time.Hour))
		fake.mode.Store("down")
		offline := harness.Call(http.MethodGet, "/api/exchange-rates", company.OwnerToken, nil).Data()
		offlineUsd, _ := rateOf(t, offline, "USD")
		if offline["available"] != true || offline["is_stale"] != true || offlineUsd != 2500 {
			t.Fatalf("offline with old rates returned %v", offline)
		}

		fake.mode.Store("ok")
		recovered := false
		for attempt := 0; attempt < 40 && !recovered; attempt++ {
			recovered = harness.Call(http.MethodGet, "/api/exchange-rates", company.OwnerToken, nil).Data()["is_stale"] == false
			time.Sleep(50 * time.Millisecond)
		}
		if !recovered {
			t.Fatal("stale rates never refreshed once the provider came back")
		}

		kesCompany := harness.CreateCompany("Nairobi Shop", "owner@nairobi.test")
		harness.Call(http.MethodPut, "/api/settings", kesCompany.OwnerToken, map[string]any{"currency_code": "KES", "currency_decimals": 2})
		kesRates := harness.Call(http.MethodGet, "/api/exchange-rates", kesCompany.OwnerToken, nil).Data()
		kesUsd, _ := rateOf(t, kesRates, "USD")
		if kesRates["base_currency"] != "KES" || kesUsd != 125 {
			t.Fatalf("a KES company saw %v", kesRates)
		}

		unlistedCompany := harness.CreateCompany("Douala Shop", "owner@douala.test")
		harness.Call(http.MethodPut, "/api/settings", unlistedCompany.OwnerToken, map[string]any{"currency_code": "XAF"})
		unlisted := harness.Call(http.MethodGet, "/api/exchange-rates", unlistedCompany.OwnerToken, nil).Data()
		unlistedProblem, _ := unlisted["problem"].(string)
		if unlisted["available"] != false || !strings.Contains(unlistedProblem, "XAF") {
			t.Fatalf("a currency without rates returned %v", unlisted)
		}

		if harness.Call(http.MethodGet, "/api/exchange-rates", "", nil).Status != http.StatusUnauthorized {
			t.Fatal("rates were served without signing in")
		}
	})
}
