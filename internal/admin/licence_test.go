package admin_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/admin"
	"github.com/chrisostomemataba/balceinv-api/internal/config"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/chrisostomemataba/balceinv-api/license"
)

type fakeWapangaji struct {
	mutex           sync.Mutex
	paidUntil       map[string]time.Time
	hardwareIds     []string
	hasDeviceRoutes bool
	recordedBodies  []map[string]any
}

func startFakeWapangaji(t *testing.T) *fakeWapangaji {
	t.Helper()
	fake := &fakeWapangaji{paidUntil: map[string]time.Time{}, hardwareIds: []string{"old-laptop", "new-laptop"}}
	server := httptest.NewServer(http.HandlerFunc(func(writer http.ResponseWriter, request *http.Request) {
		fake.mutex.Lock()
		defer fake.mutex.Unlock()
		writer.Header().Set("Content-Type", "application/json")
		isSignedIn := request.Header.Get("Authorization") == "Bearer fresh-access"
		needsSignIn := strings.Contains(request.URL.Path, "/balce/admin/")
		if needsSignIn && request.Header.Get("Authorization") == "Bearer stale-access" {
			writer.WriteHeader(http.StatusUnauthorized)
			writer.Write([]byte(`{"detail": "Given token not valid"}`))
			return
		}
		if needsSignIn && !isSignedIn {
			writer.WriteHeader(http.StatusUnauthorized)
			return
		}
		switch {
		case request.URL.Path == "/auth/login/":
			loginBody := map[string]string{}
			json.NewDecoder(request.Body).Decode(&loginBody)
			if loginBody["password"] != "partner-pass" {
				writer.WriteHeader(http.StatusUnauthorized)
				writer.Write([]byte(`{"error": "Invalid credentials"}`))
				return
			}
			writer.Write([]byte(`{"user": {"full_name": "Asha Partner"}, "tokens": {"access": "stale-access", "refresh": "refresh-1"}}`))
		case request.URL.Path == "/auth/token/refresh/":
			writer.Write([]byte(`{"access": "fresh-access"}`))
		case request.URL.Path == "/payments/balce/packages/":
			writer.Write([]byte(`{"data": [{"id": 3, "name": "Monthly", "price": "20000.00", "days_granted": 30, "max_devices": 2}]}`))
		case request.URL.Path == "/payments/balce/admin/lookup/":
			hardwareId := request.URL.Query().Get("hardware_id")
			paidUntil, isPaid := fake.paidUntil[hardwareId]
			if !isPaid {
				writer.Write([]byte(`{"success": true, "found": false}`))
				return
			}
			json.NewEncoder(writer).Encode(map[string]any{"success": true, "found": true, "data": map[string]any{
				"license_key": "key-" + hardwareId, "phone": "+255712000000", "package": "Monthly",
				"expires_at": paidUntil.Format(time.RFC3339), "days_granted": 30, "max_devices": 2, "device_count": 1,
			}})
		case request.URL.Path == "/payments/balce/admin/record-payment/":
			paymentBody := map[string]any{}
			json.NewDecoder(request.Body).Decode(&paymentBody)
			fake.recordedBodies = append(fake.recordedBodies, paymentBody)
			hardwareId := paymentBody["hardware_id"].(string)
			paidUntil := time.Now().UTC().Add(30 * 24 * time.Hour)
			fake.paidUntil[hardwareId] = paidUntil
			json.NewEncoder(writer).Encode(map[string]any{"success": true, "data": map[string]any{
				"license_key": "key-" + hardwareId, "phone": "+255712000000", "package": "Monthly",
				"expires_at": paidUntil.Format(time.RFC3339), "days_granted": 30, "max_devices": 2, "device_count": 1,
			}})
		case request.URL.Path == "/payments/balce/admin/payments/":
			writer.Write([]byte(`{"data": [{"phone": "+255712000000", "package": "Monthly", "amount": "20000.00", "method": "cash", "reference": "R1", "processed_by": "Asha Partner", "created_at": "2026-10-11T08:00:00Z"}]}`))
		case strings.HasPrefix(request.URL.Path, "/payments/balce/license/by-hardware/"):
			hardwareId := strings.TrimSuffix(strings.TrimPrefix(request.URL.Path, "/payments/balce/license/by-hardware/"), "/")
			paidUntil, isPaid := fake.paidUntil[hardwareId]
			if !isPaid {
				writer.WriteHeader(http.StatusNotFound)
				return
			}
			expiresAt := paidUntil.Format("2006-01-02T15:04:05.000000+00:00")
			json.NewEncoder(writer).Encode(map[string]any{
				"success":      true,
				"license_key":  "key-" + hardwareId,
				"license_data": map[string]any{"expires_at": expiresAt, "max_devices": 2, "days_granted": 30},
				"signature":    license.ComputeSignature("key-"+hardwareId, expiresAt, 2, 30),
			})
		case fake.hasDeviceRoutes && request.URL.Path == "/payments/balce/admin/devices/":
			json.NewEncoder(writer).Encode(map[string]any{"success": true, "data": map[string]any{"license_key": "key-x", "hardware_ids": fake.hardwareIds, "max_devices": 2}})
		case fake.hasDeviceRoutes && request.URL.Path == "/payments/balce/admin/remove-device/":
			removeBody := map[string]string{}
			json.NewDecoder(request.Body).Decode(&removeBody)
			keptIds := []string{}
			for _, hardwareId := range fake.hardwareIds {
				if hardwareId != removeBody["remove_hardware_id"] {
					keptIds = append(keptIds, hardwareId)
				}
			}
			fake.hardwareIds = keptIds
			json.NewEncoder(writer).Encode(map[string]any{"success": true, "data": map[string]any{"license_key": "key-x", "hardware_ids": fake.hardwareIds, "max_devices": 2}})
		default:
			writer.Header().Set("Content-Type", "text/html")
			writer.WriteHeader(http.StatusNotFound)
			writer.Write([]byte("<h1>Not Found</h1>"))
		}
	}))
	previousBaseUrl := license.DjangoBaseURL
	license.DjangoBaseURL = server.URL + "/payments"
	t.Cleanup(func() {
		license.DjangoBaseURL = previousBaseUrl
		server.Close()
	})
	return fake
}

func TestTheAdminPanelActivatesLicencesAndRemovesDevicesThroughWapangaji(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		if engineCase.Engine != config.EnginePostgres {
			return
		}
		fake := startFakeWapangaji(t)
		harness := apptest.Start(t, engineCase)
		duka := harness.CreateCompany("Licence Duka", "owner@licenceduka.test")
		support := saveTeamMember(t, harness, "sales@balce.test", admin.RoleSupport)
		_, cookie := signIn(t, harness, support, currentCode(t, support))
		cloudId := "cloud-" + duka.Id.String()

		if asStaff(harness, cookie, http.MethodPost, "/api/admin/licences/lookup", map[string]any{"hardware_id": cloudId}).Code() != "wapangaji_not_connected" {
			t.Fatal("a lookup ran before signing in to Wapangaji")
		}
		if asStaff(harness, cookie, http.MethodPost, "/api/admin/wapangaji/connect", map[string]any{"phone": "0712000000", "password": "wrong"}).Code() != "wapangaji_bad_sign_in" {
			t.Fatal("a wrong Wapangaji password was accepted")
		}
		connected := asStaff(harness, cookie, http.MethodPost, "/api/admin/wapangaji/connect", map[string]any{"phone": "0712000000", "password": "partner-pass"})
		if connected.Status != http.StatusOK || connected.Data()["name"] != "Asha Partner" {
			t.Fatalf("connecting returned %d %v", connected.Status, connected.Body)
		}

		notFound := asStaff(harness, cookie, http.MethodPost, "/api/admin/licences/lookup", map[string]any{"hardware_id": cloudId})
		if notFound.Status != http.StatusOK || notFound.Data()["found"] != false || notFound.Data()["company_name"] != "Licence Duka" {
			t.Fatalf("looking up an unpaid shop after a token refresh returned %d %v", notFound.Status, notFound.Body)
		}

		plans := asStaff(harness, cookie, http.MethodGet, "/api/admin/licences/packages", nil)
		if len(plans.Body["data"].([]any)) != 1 {
			t.Fatalf("plans returned %v", plans.Body)
		}

		recorded := asStaff(harness, cookie, http.MethodPost, "/api/admin/licences/record-payment", map[string]any{
			"hardware_id": cloudId, "phone": "0712000000", "package_id": 3, "amount": 20000, "method": "cash", "reference": "R1",
		})
		if recorded.Status != http.StatusOK || recorded.Data()["package"] != "Monthly" {
			t.Fatalf("recording a payment returned %d %v", recorded.Status, recorded.Body)
		}
		if len(fake.recordedBodies) != 1 || fake.recordedBodies[0]["hardware_id"] != cloudId {
			t.Fatalf("Wapangaji got %v", fake.recordedBodies)
		}
		if harness.QueryIntForCompany(duka.Id, `SELECT COUNT(*) FROM company_subscriptions WHERE company_id = $1 AND NOT is_trial`, duka.Id) != 1 {
			t.Fatal("the web shop's subscription did not switch to the paid plan straight away")
		}

		history := asStaff(harness, cookie, http.MethodPost, "/api/admin/licences/payments", map[string]any{"hardware_id": cloudId})
		if history.Status != http.StatusOK || len(history.Body["data"].([]any)) != 1 {
			t.Fatalf("payment history returned %d %v", history.Status, history.Body)
		}

		if asStaff(harness, cookie, http.MethodPost, "/api/admin/licences/devices", map[string]any{"hardware_id": "old-laptop"}).Code() != "wapangaji_update_required" {
			t.Fatal("the missing Wapangaji device endpoint was not reported as an update")
		}
		fake.mutex.Lock()
		fake.hasDeviceRoutes = true
		fake.mutex.Unlock()
		devices := asStaff(harness, cookie, http.MethodPost, "/api/admin/licences/devices", map[string]any{"hardware_id": "new-laptop"})
		if devices.Status != http.StatusOK || len(devices.Data()["hardware_ids"].([]any)) != 2 {
			t.Fatalf("listing devices returned %d %v", devices.Status, devices.Body)
		}
		removed := asStaff(harness, cookie, http.MethodPost, "/api/admin/licences/remove-device", map[string]any{"hardware_id": "new-laptop", "remove_hardware_id": "old-laptop"})
		if removed.Status != http.StatusOK || len(removed.Data()["hardware_ids"].([]any)) != 1 {
			t.Fatalf("removing a device returned %d %v", removed.Status, removed.Body)
		}

		chief := saveTeamMember(t, harness, "boss@balce.test", admin.RoleAdmin)
		_, chiefCookie := signIn(t, harness, chief, currentCode(t, chief))
		actions := map[string]bool{}
		for _, rawEntry := range asStaff(harness, chiefCookie, http.MethodGet, "/api/admin/audit", nil).Body["data"].([]any) {
			actions[rawEntry.(map[string]any)["action"].(string)] = true
		}
		for _, expectedAction := range []string{"connected_wapangaji", "recorded_payment", "removed_device"} {
			if !actions[expectedAction] {
				t.Fatalf("the audit log is missing %s: %v", expectedAction, actions)
			}
		}
	})
}
