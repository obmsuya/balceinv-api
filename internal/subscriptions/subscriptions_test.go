package subscriptions_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/chrisostomemataba/balceinv-api/license"
)

type fakeLicensingServer struct {
	mutex         sync.Mutex
	paidDeviceIds map[string]time.Time
	paymentBodies []map[string]any
}

func startFakeLicensingServer(t *testing.T) *fakeLicensingServer {
	t.Helper()
	fake := &fakeLicensingServer{paidDeviceIds: map[string]time.Time{}}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		fake.mutex.Lock()
		defer fake.mutex.Unlock()
		switch {
		case request.Method == http.MethodPost && request.URL.Path == "/balce/pay/":
			paymentBody := map[string]any{}
			json.NewDecoder(request.Body).Decode(&paymentBody)
			fake.paymentBodies = append(fake.paymentBodies, paymentBody)
			writer.Header().Set("Content-Type", "application/json")
			writer.Write([]byte(`{"success": true, "status": "pending"}`))
		case strings.HasPrefix(request.URL.Path, "/balce/license/by-hardware/"):
			deviceId := strings.TrimSuffix(strings.TrimPrefix(request.URL.Path, "/balce/license/by-hardware/"), "/")
			paidUntil, isPaid := fake.paidDeviceIds[deviceId]
			if !isPaid {
				writer.WriteHeader(http.StatusNotFound)
				return
			}
			expiresAt := paidUntil.UTC().Format("2006-01-02T15:04:05.000000+00:00")
			writer.Header().Set("Content-Type", "application/json")
			json.NewEncoder(writer).Encode(map[string]any{
				"success":      true,
				"license_key":  "paid-key-" + deviceId,
				"license_data": map[string]any{"expires_at": expiresAt, "max_devices": 1, "days_granted": 30},
				"signature":    license.ComputeSignature("paid-key-"+deviceId, expiresAt, 1, 30),
			})
		default:
			writer.WriteHeader(http.StatusNotFound)
		}
	}))
	previousBaseUrl := license.DjangoBaseURL
	license.DjangoBaseURL = server.URL
	t.Cleanup(func() {
		license.DjangoBaseURL = previousBaseUrl
		server.Close()
	})
	return fake
}

func (fake *fakeLicensingServer) markPaid(deviceId string, paidUntil time.Time) {
	fake.mutex.Lock()
	defer fake.mutex.Unlock()
	fake.paidDeviceIds[deviceId] = paidUntil
}

func TestWebBusinessesGetATrialThenPayOrLock(t *testing.T) {
	fake := startFakeLicensingServer(t)
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Web Shop", "owner@webshop.test")
		otherCompany := harness.CreateCompany("Other Web Shop", "owner@otherwebshop.test")
		cashierToken := harness.CreateStaff(company, "cashier@webshop.test", []string{"sales:create"}, nil)
		deviceId := "cloud-" + company.Id.String()

		trial := harness.Call(http.MethodGet, "/api/license/status", company.OwnerToken, nil)
		trialData := trial.Data()
		if trial.Status != http.StatusOK || trialData["licensed"] != true || trialData["is_trial"] != true || trialData["days_remaining"] != float64(14) {
			t.Fatalf("a new web business is not on a 14-day trial: %d %v", trial.Status, trial.Body)
		}
		idResponse := harness.Call(http.MethodGet, "/api/license/hardware-id", company.OwnerToken, nil)
		if idResponse.Body["hardware_id"] != deviceId {
			t.Fatalf("hardware id = %v, want %s", idResponse.Body["hardware_id"], deviceId)
		}

		harness.ExecForCompany(company.Id, `UPDATE company_subscriptions SET expires_at = $1 WHERE company_id = $2`, time.Now().UTC().AddDate(0, 0, -2), company.Id)
		grace := harness.Call(http.MethodGet, "/api/license/status", company.OwnerToken, nil).Data()
		if grace["licensed"] != true || grace["is_grace_period"] != true {
			t.Fatalf("two days after the trial ended is not the grace period: %v", grace)
		}
		if harness.Call(http.MethodGet, "/api/products", company.OwnerToken, nil).Status != http.StatusOK {
			t.Fatal("the grace period locked products")
		}

		harness.ExecForCompany(company.Id, `UPDATE company_subscriptions SET expires_at = $1 WHERE company_id = $2`, time.Now().UTC().AddDate(0, 0, -6), company.Id)
		locked := harness.Call(http.MethodGet, "/api/products", company.OwnerToken, nil)
		if locked.Status != http.StatusPaymentRequired || locked.Body["code"] != "subscription_required" {
			t.Fatalf("an ended subscription served products: %d %v", locked.Status, locked.Body)
		}
		if harness.Call(http.MethodPost, "/api/sales", cashierToken, map[string]any{"client_ref": "locked-sale"}).Status != http.StatusPaymentRequired {
			t.Fatal("a cashier could sell after the subscription ended")
		}
		for _, openPath := range []string{"/api/auth/me", "/api/license/status", "/api/platform"} {
			if harness.Call(http.MethodGet, openPath, company.OwnerToken, nil).Status != http.StatusOK {
				t.Fatalf("%s was locked, so the pay screen could not load", openPath)
			}
		}
		if harness.Call(http.MethodGet, "/api/products", otherCompany.OwnerToken, nil).Status != http.StatusOK {
			t.Fatal("one business's ended subscription locked another business")
		}

		if harness.Call(http.MethodPost, "/api/license/pay", cashierToken, map[string]any{"phone": "0712345678", "provider": "Mpesa", "package_id": 1}).Status != http.StatusForbidden {
			t.Fatal("a cashier could start a payment")
		}
		payment := harness.Call(http.MethodPost, "/api/license/pay", company.OwnerToken, map[string]any{"phone": "0712345678", "provider": "Mpesa", "package_id": 1})
		if payment.Status != http.StatusOK {
			t.Fatalf("pay returned %d %v", payment.Status, payment.Body)
		}
		lastPayment := fake.paymentBodies[len(fake.paymentBodies)-1]
		if lastPayment["hardware_id"] != deviceId {
			t.Fatalf("payment was sent for %v, want %s", lastPayment["hardware_id"], deviceId)
		}

		unpaidRefresh := harness.Call(http.MethodPost, "/api/license/refresh", company.OwnerToken, nil)
		if unpaidRefresh.Status != http.StatusOK || unpaidRefresh.Data()["licensed"] != false {
			t.Fatalf("refresh before the payment landed returned %d %v", unpaidRefresh.Status, unpaidRefresh.Body)
		}

		fake.markPaid(deviceId, time.Now().UTC().AddDate(0, 0, 30))
		paid := harness.Call(http.MethodPost, "/api/license/refresh", company.OwnerToken, nil)
		paidData := paid.Data()
		if paid.Status != http.StatusOK || paidData["licensed"] != true || paidData["is_trial"] != false || paidData["days_remaining"] != float64(30) {
			t.Fatalf("refresh after paying returned %d %v", paid.Status, paid.Body)
		}
		if harness.Call(http.MethodGet, "/api/products", company.OwnerToken, nil).Status != http.StatusOK {
			t.Fatal("paying did not unlock the business")
		}

		fake.markPaid(deviceId, time.Now().UTC().AddDate(0, 0, 10))
		shorter := harness.Call(http.MethodPost, "/api/license/refresh", company.OwnerToken, nil).Data()
		if shorter["days_remaining"] != float64(30) {
			t.Fatalf("an older licence shortened the subscription: %v", shorter)
		}

		otherStatus := harness.Call(http.MethodGet, "/api/license/status", otherCompany.OwnerToken, nil).Data()
		if otherStatus["is_trial"] != true {
			t.Fatalf("another business's payment changed this one: %v", otherStatus)
		}
	})
}

func TestTrialWebBusinessPicksUpAnActivationStraightAway(t *testing.T) {
	fake := startFakeLicensingServer(t)
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Activated Shop", "owner@activated.test")
		fake.markPaid("cloud-"+company.Id.String(), time.Now().UTC().AddDate(0, 0, 90))

		activated := harness.Call(http.MethodPost, "/api/license/refresh", company.OwnerToken, nil).Data()
		if activated["is_trial"] != false || activated["days_remaining"] != float64(90) {
			t.Fatalf("a trial did not pick up a sales-tool activation: %v", activated)
		}
	})
}

func TestABusinessWithoutASubscriptionRowGetsATrialNotALock(t *testing.T) {
	startFakeLicensingServer(t)
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Rowless Shop", "owner@rowless.test")
		harness.ExecForCompany(company.Id, `DELETE FROM company_subscriptions WHERE company_id = $1`, company.Id)

		products := harness.Call(http.MethodGet, "/api/products", company.OwnerToken, nil)
		if products.Status != http.StatusOK {
			t.Fatalf("a business without a subscription row was locked: %d %v", products.Status, products.Body)
		}
		status := harness.Call(http.MethodGet, "/api/license/status", company.OwnerToken, nil).Data()
		if status["is_trial"] != true || status["days_remaining"] != float64(14) {
			t.Fatalf("the missing row did not become a 14-day trial: %v", status)
		}
		if harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM company_subscriptions WHERE company_id = $1`, company.Id) != 1 {
			t.Fatal("the trial was not saved")
		}
	})
}
