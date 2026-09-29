package settings_test

import (
	"bytes"
	"image"
	"image/color"
	"image/png"
	"net/http"
	"strings"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
)

func pngBytes(t *testing.T) []byte {
	t.Helper()
	picture := image.NewRGBA(image.Rect(0, 0, 4, 4))
	picture.Set(1, 1, color.RGBA{R: 30, G: 64, B: 175, A: 255})
	encoded := &bytes.Buffer{}
	encodeError := png.Encode(encoded, picture)
	if encodeError != nil {
		t.Fatalf("encode png: %v", encodeError)
	}
	return encoded.Bytes()
}

func signInCashier(t *testing.T, harness *apptest.Harness, company apptest.Company, email string) string {
	t.Helper()
	cashierRole := harness.Call(http.MethodPost, "/api/roles", company.OwnerToken, map[string]any{"name": "Cashier", "permission_ids": []string{"sales:create"}})
	createCashier := harness.Call(http.MethodPost, "/api/users", company.OwnerToken, map[string]any{
		"name": "Cashier", "email": email, "password": "cashier-password", "role_id": cashierRole.Data()["id"],
	})
	if createCashier.Status != http.StatusCreated {
		t.Fatalf("create cashier returned %d: %v", createCashier.Status, createCashier.Body)
	}
	return harness.MustLogin(email, "cashier-password")
}

func TestSettingsDefaultsPartialUpdatesAndValidation(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Settings Shop", "owner@settings.test")
		otherCompany := harness.CreateCompany("Other Settings", "owner@othersettings.test")

		var defaults apptest.Response
		getQueries := harness.CountQueries(func() {
			defaults = harness.Call(http.MethodGet, "/api/settings", company.OwnerToken, nil)
		})
		if defaults.Status != http.StatusOK {
			t.Fatalf("get settings returned %d: %v", defaults.Status, defaults.Body)
		}
		if getQueries > 2 {
			t.Fatalf("reading settings ran %d queries, want at most 2", getQueries)
		}
		defaultData := defaults.Data()
		defaultCompany := defaultData["company"].(map[string]any)
		if defaultData["tax_rate"] != float64(18) || defaultCompany["currency_code"] != "TZS" || defaultCompany["currency_decimals"] != float64(0) || defaultCompany["primary_color"] != "#5ea500" {
			t.Fatalf("unexpected defaults: %v", defaultData)
		}

		brandUpdate := harness.Call(http.MethodPut, "/api/settings", company.OwnerToken, map[string]any{
			"primary_color": "#1D4ED8",
			"tax_rate":      16.5,
			"currency_code": "KES",
		})
		if brandUpdate.Status != http.StatusOK {
			t.Fatalf("brand update returned %d: %v", brandUpdate.Status, brandUpdate.Body)
		}
		updatedData := brandUpdate.Data()
		updatedCompany := updatedData["company"].(map[string]any)
		if updatedCompany["primary_color"] != "#1d4ed8" || updatedData["tax_rate"] != 16.5 || updatedCompany["currency_code"] != "KES" {
			t.Fatalf("update not applied: %v", updatedData)
		}
		if updatedCompany["name"] != "Settings Shop" || updatedData["date_format"] != "DD/MM/YYYY" || updatedData["alert_on_low_stock"] != true {
			t.Fatalf("a partial update changed fields it was not given: %v", updatedData)
		}

		rejectedBodies := []map[string]any{
			{"primary_color": "#fff"},
			{"primary_color": "1d4ed8"},
			{"primary_color": "#12345G"},
			{"tax_rate": 101},
			{"tax_rate": -1},
			{"currency_code": "kes"},
			{"currency_decimals": 3},
			{"timezone": "Mars/Olympus_Mons"},
			{"efd_endpoint": "http://efd.example.com"},
			{"notification_email": "not-an-email"},
			{"printer_paper_width": 70},
			{"business_name": ""},
		}
		for _, rejectedBody := range rejectedBodies {
			rejected := harness.Call(http.MethodPut, "/api/settings", company.OwnerToken, rejectedBody)
			if rejected.Status != http.StatusBadRequest {
				t.Fatalf("%v returned %d, want 400", rejectedBody, rejected.Status)
			}
		}

		meResponse := harness.Call(http.MethodGet, "/api/auth/me", company.OwnerToken, nil)
		branding := meResponse.Data()["branding"].(map[string]any)
		if branding["primary_color"] != "#1d4ed8" || branding["currency_code"] != "KES" {
			t.Fatalf("/me branding did not follow the settings: %v", branding)
		}

		otherSettings := harness.Call(http.MethodGet, "/api/settings", otherCompany.OwnerToken, nil)
		otherCompanyData := otherSettings.Data()["company"].(map[string]any)
		if otherCompanyData["primary_color"] != "#5ea500" || otherCompanyData["currency_code"] != "TZS" || otherCompanyData["name"] != "Other Settings" {
			t.Fatalf("one company's settings leaked into another: %v", otherCompanyData)
		}

		cashierToken := signInCashier(t, harness, company, "cashier@settings.test")
		cashierRead := harness.Call(http.MethodGet, "/api/settings", cashierToken, nil)
		cashierWrite := harness.Call(http.MethodPut, "/api/settings", cashierToken, map[string]any{"primary_color": "#000000"})
		if cashierRead.Status != http.StatusForbidden || cashierWrite.Status != http.StatusForbidden {
			t.Fatalf("cashier read/write returned %d/%d, want 403/403", cashierRead.Status, cashierWrite.Status)
		}
		cashierBranding := harness.Call(http.MethodGet, "/api/auth/me", cashierToken, nil).Data()["branding"].(map[string]any)
		if cashierBranding["primary_color"] != "#1d4ed8" {
			t.Fatalf("a cashier must still receive the brand for theming: %v", cashierBranding)
		}
	})
}

func TestEfdApiKeyIsWriteOnly(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Efd Shop", "owner@efd.test")
		secretValue := "tra-efd-secret-5f2c"

		setKey := harness.Call(http.MethodPut, "/api/settings", company.OwnerToken, map[string]any{
			"efd_enabled":  true,
			"efd_endpoint": "https://efd.example.com/api",
			"efd_api_key":  secretValue,
		})
		readBack := harness.Call(http.MethodGet, "/api/settings", company.OwnerToken, nil)
		for _, efdResponse := range []apptest.Response{setKey, readBack} {
			if efdResponse.Status != http.StatusOK || efdResponse.Data()["efd_api_key_set"] != true {
				t.Fatalf("expected efd_api_key_set=true, got %d %v", efdResponse.Status, efdResponse.Data())
			}
			if strings.Contains(string(efdResponse.Raw), secretValue) {
				t.Fatal("the EFD API key appeared in a response")
			}
		}

		keepKey := harness.Call(http.MethodPut, "/api/settings", company.OwnerToken, map[string]any{"efd_enabled": false})
		if keepKey.Data()["efd_api_key_set"] != true {
			t.Fatal("updating another field must not clear the stored key")
		}

		clearKey := harness.Call(http.MethodPut, "/api/settings", company.OwnerToken, map[string]any{"efd_api_key": ""})
		if clearKey.Data()["efd_api_key_set"] != false {
			t.Fatal("an empty key must clear the stored key")
		}
	})
}

func TestLogoUploadAndPublicServing(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Logo Shop", "owner@logo.test")
		validPng := pngBytes(t)

		missingField := harness.Upload("/api/settings/upload-logo", company.OwnerToken, "", "", nil)
		if missingField.Status != http.StatusBadRequest {
			t.Fatalf("upload without a file returned %d, want 400", missingField.Status)
		}

		disguisedText := harness.Upload("/api/settings/upload-logo", company.OwnerToken, "file", "logo.png", []byte("definitely not an image, just text"))
		if disguisedText.Status != http.StatusBadRequest || disguisedText.Code() != "invalid_logo" {
			t.Fatalf("text named .png returned %d %s, want 400 invalid_logo", disguisedText.Status, disguisedText.Code())
		}

		oversized := append(append([]byte{}, validPng...), make([]byte, 1<<20)...)
		tooLarge := harness.Upload("/api/settings/upload-logo", company.OwnerToken, "file", "huge.png", oversized)
		if tooLarge.Status != http.StatusRequestEntityTooLarge {
			t.Fatalf("oversized logo returned %d, want 413", tooLarge.Status)
		}

		cashierToken := signInCashier(t, harness, company, "cashier@logo.test")
		cashierUpload := harness.Upload("/api/settings/upload-logo", cashierToken, "file", "logo.png", validPng)
		if cashierUpload.Status != http.StatusForbidden {
			t.Fatalf("cashier upload returned %d, want 403", cashierUpload.Status)
		}

		accepted := harness.Upload("/api/settings/upload-logo", company.OwnerToken, "file", "anything.bin", validPng)
		if accepted.Status != http.StatusOK {
			t.Fatalf("valid png returned %d: %v", accepted.Status, accepted.Body)
		}
		logoUrl, hasLogoUrl := accepted.Data()["company"].(map[string]any)["logo_url"].(string)
		if !hasLogoUrl || !strings.HasPrefix(logoUrl, "/api/branding/logo/"+company.Id.String()+"/") || !strings.HasSuffix(logoUrl, ".png") {
			t.Fatalf("unexpected logo url %v", accepted.Data()["company"])
		}

		publicLogo := harness.Call(http.MethodGet, logoUrl, "", nil)
		if publicLogo.Status != http.StatusOK || !bytes.Equal(publicLogo.Raw, validPng) {
			t.Fatalf("public logo returned %d with %d bytes", publicLogo.Status, len(publicLogo.Raw))
		}
		if publicLogo.Headers.Get("Content-Type") != "image/png" || !strings.Contains(publicLogo.Headers.Get("Cache-Control"), "immutable") {
			t.Fatalf("logo headers: %v", publicLogo.Headers)
		}

		meBranding := harness.Call(http.MethodGet, "/api/auth/me", company.OwnerToken, nil).Data()["branding"].(map[string]any)
		if meBranding["logo_url"] != logoUrl {
			t.Fatalf("/me logo_url %v, want %s", meBranding["logo_url"], logoUrl)
		}

		unsafePaths := []string{
			"/api/branding/logo/" + company.Id.String() + "/..%2F..%2Fetc%2Fpasswd",
			"/api/branding/logo/not-a-uuid/0190f7a2-0000-7000-8000-000000000000.png",
			"/api/branding/logo/" + company.Id.String() + "/0190f7a2-0000-7000-8000-000000000000.png",
			"/api/branding/logo/" + company.Id.String() + "/logo.svg",
		}
		for _, unsafePath := range unsafePaths {
			unsafeResponse := harness.Call(http.MethodGet, unsafePath, "", nil)
			if unsafeResponse.Status != http.StatusNotFound {
				t.Fatalf("%s returned %d, want 404", unsafePath, unsafeResponse.Status)
			}
		}
	})
}
