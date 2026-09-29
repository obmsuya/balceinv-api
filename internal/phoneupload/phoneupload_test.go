package phoneupload_test

import (
	"encoding/base64"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/config"
	"github.com/chrisostomemataba/balceinv-api/internal/phoneupload"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/google/uuid"
)

const tinyPngBase64 = "iVBORw0KGgoAAAANSUhEUgAAAAEAAAABCAYAAAAfFcSJAAAADUlEQVR42mNk+M9QDwADhgGAWjR9awAAAABJRU5ErkJggg=="

func TestPhonePhotosReachTheComputerOnce(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		isDesktop := engineCase.Engine == config.EngineSqlite
		startHarness := apptest.Start
		if isDesktop {
			startHarness = apptest.StartDesktop
		}
		harness := startHarness(t, engineCase)
		company := harness.CreateCompany("Photo Shop", "owner@photo.test")
		otherCompany := harness.CreateCompany("Other Photo", "owner@otherphoto.test")
		cashierToken := harness.CreateStaff(company, "cashier@photo.test", []string{"sales:create"}, []uuid.UUID{company.ShopId})

		if harness.Call(http.MethodPost, "/api/phone-uploads", "", nil).Status != http.StatusUnauthorized {
			t.Fatal("an upload link was made without signing in")
		}
		if harness.Call(http.MethodPost, "/api/phone-uploads", cashierToken, nil).Status != http.StatusForbidden {
			t.Fatal("someone who can't edit products made an upload link")
		}

		created := harness.Call(http.MethodPost, "/api/phone-uploads", company.OwnerToken, nil)
		token, _ := created.Data()["token"].(string)
		if created.Status != http.StatusCreated || len(token) != 32 {
			t.Fatalf("creating a link returned %d %v", created.Status, created.Body)
		}
		uploadUrls := created.Data()["upload_urls"].([]any)
		if isDesktop {
			if created.Data()["reachable"] != false || !strings.Contains(created.Data()["reason"].(string), "Settings") {
				t.Fatalf("with LAN off the link claimed to be reachable: %v", created.Data())
			}
			harness.Call(http.MethodPut, "/api/platform/network", company.OwnerToken, map[string]any{"lan_enabled": true})
		} else if len(uploadUrls) != 1 || !strings.HasSuffix(uploadUrls[0].(string), "/upload/"+token) {
			t.Fatalf("the cloud link was %v", uploadUrls)
		}

		page := harness.Call(http.MethodGet, "/upload/"+token, "", nil)
		if page.Status != http.StatusOK || !strings.Contains(string(page.Raw), "Take a photo") {
			t.Fatalf("the phone page returned %d", page.Status)
		}
		if harness.Call(http.MethodGet, "/upload/"+strings.Repeat("0", 32), "", nil).Status != http.StatusNotFound {
			t.Fatal("an unknown link showed the upload page")
		}

		waiting := harness.Call(http.MethodGet, "/api/phone-uploads/"+token, company.OwnerToken, nil)
		if waiting.Data()["status"] != "pending" {
			t.Fatalf("before the photo the status was %v", waiting.Body)
		}
		for _, badImage := range []string{"hello", "data:image/png;base64,bm90IGFuIGltYWdl", "data:text/html;base64," + tinyPngBase64} {
			if harness.Call(http.MethodPost, "/upload/"+token, "", map[string]any{"image": badImage}).Status != http.StatusBadRequest {
				t.Fatalf("%q was accepted as a photo", badImage[:10])
			}
		}
		hugeImage := "data:image/png;base64," + base64.StdEncoding.EncodeToString(make([]byte, 3*1024*1024))
		if harness.Call(http.MethodPost, "/upload/"+token, "", map[string]any{"image": hugeImage}).Status != http.StatusBadRequest {
			t.Fatal("a 3 MB photo was accepted")
		}

		sent := harness.Call(http.MethodPost, "/upload/"+token, "", map[string]any{"image": "data:image/png;base64," + tinyPngBase64})
		if sent.Status != http.StatusOK {
			t.Fatalf("sending the photo returned %d %v", sent.Status, sent.Body)
		}
		if harness.Call(http.MethodPost, "/upload/"+token, "", map[string]any{"image": "data:image/png;base64," + tinyPngBase64}).Status != http.StatusConflict {
			t.Fatal("a second photo replaced the first")
		}
		if harness.Call(http.MethodGet, "/api/phone-uploads/"+token, otherCompany.OwnerToken, nil).Status != http.StatusNotFound {
			t.Fatal("another company collected the photo")
		}
		collected := harness.Call(http.MethodGet, "/api/phone-uploads/"+token, company.OwnerToken, nil)
		if collected.Data()["status"] != "done" || collected.Data()["image"] != "data:image/png;base64,"+tinyPngBase64 {
			t.Fatalf("collecting returned %v", collected.Body)
		}
		if harness.Call(http.MethodGet, "/api/phone-uploads/"+token, company.OwnerToken, nil).Status != http.StatusNotFound {
			t.Fatal("the photo could be collected twice")
		}
	})
}

func TestUploadLinksExpire(t *testing.T) {
	service := phoneupload.NewService()
	currentTime := time.Date(2026, 9, 29, 10, 0, 0, 0, time.UTC)
	service.SetClock(func() time.Time { return currentTime })
	companyId := uuid.New()

	token, expiresAt, createError := service.Create(companyId)
	if createError != nil || !expiresAt.Equal(currentTime.Add(5*time.Minute)) {
		t.Fatalf("create returned %v %v", expiresAt, createError)
	}
	currentTime = currentTime.Add(6 * time.Minute)
	if service.IsLive(token) {
		t.Fatal("a six-minute-old link was still live")
	}
	if service.Submit(token, "data:image/png;base64,"+tinyPngBase64) != phoneupload.ErrSessionNotFound {
		t.Fatal("a photo was accepted on an expired link")
	}
}
