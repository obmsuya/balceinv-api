package auth_test

import (
	"crypto/sha256"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/config"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
)

func TestFirstSetupRunsOnceOnDesktopAndIsOpenToEveryBusinessInCloud(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		setupBody := map[string]any{
			"business_name":  "Duka la Mama",
			"owner_name":     "Mama",
			"owner_email":    "mama@duka.test",
			"owner_password": "strong-password-1",
		}

		isCloud := engineCase.Engine == config.EnginePostgres
		if isCloud {
			checkCloudSignUp(t, harness, setupBody)
			return
		}

		beforeResponse := harness.Call(http.MethodGet, "/api/setup/status", "", nil)
		if beforeResponse.Data()["configured"] != false {
			t.Fatalf("fresh desktop reported configured: %v", beforeResponse.Body)
		}

		firstSetup := harness.Call(http.MethodPost, "/api/setup", "", setupBody)
		if firstSetup.Status != http.StatusCreated {
			t.Fatalf("first setup returned %d: %v", firstSetup.Status, firstSetup.Body)
		}

		secondSetup := harness.Call(http.MethodPost, "/api/setup", "", setupBody)
		if secondSetup.Status != http.StatusConflict {
			t.Fatalf("second setup returned %d, want 409", secondSetup.Status)
		}

		ownerToken := harness.MustLogin("MAMA@duka.test ", "strong-password-1")
		meResponse := harness.Call(http.MethodGet, "/api/auth/me", ownerToken, nil)
		meData := meResponse.Data()
		if meData["is_owner"] != true || meData["company_name"] != "Duka la Mama" {
			t.Fatalf("me after setup: %v", meData)
		}
		ownerShops, _ := meData["shops"].([]any)
		if len(ownerShops) != 1 || meData["shop_id"] == nil {
			t.Fatalf("owner should start in the Main Shop, got %v", meData)
		}
		ownerPermissions, _ := meData["permissions"].([]any)
		if len(ownerPermissions) != 61 {
			t.Fatalf("owner has %d permissions, want all 61", len(ownerPermissions))
		}
	})
}

func TestLoginFailuresLookIdenticalAndAreRateLimited(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Rate Shop", "owner@rate.test")

		wrongPasswordStarted := time.Now()
		wrongPassword := harness.Call(http.MethodPost, "/api/auth/login", "", map[string]any{"email": company.OwnerEmail, "password": "not-the-password"})
		wrongPasswordDuration := time.Since(wrongPasswordStarted)

		unknownEmailStarted := time.Now()
		unknownEmail := harness.Call(http.MethodPost, "/api/auth/login", "", map[string]any{"email": "nobody@rate.test", "password": "not-the-password"})
		unknownEmailDuration := time.Since(unknownEmailStarted)

		if wrongPassword.Status != http.StatusUnauthorized || unknownEmail.Status != http.StatusUnauthorized {
			t.Fatalf("statuses %d and %d, want 401 for both", wrongPassword.Status, unknownEmail.Status)
		}
		if wrongPassword.Body["message"] != unknownEmail.Body["message"] {
			t.Fatalf("messages differ: %q vs %q", wrongPassword.Body["message"], unknownEmail.Body["message"])
		}
		unknownEmailSkippedHashing := unknownEmailDuration < wrongPasswordDuration/4
		if unknownEmailSkippedHashing {
			t.Fatalf("unknown email answered in %s vs %s for a wrong password; it leaks which emails exist", unknownEmailDuration, wrongPasswordDuration)
		}

		for attemptNumber := 2; attemptNumber <= 5; attemptNumber++ {
			harness.Call(http.MethodPost, "/api/auth/login", "", map[string]any{"email": company.OwnerEmail, "password": "still-wrong"})
		}
		sixthAttempt := harness.Call(http.MethodPost, "/api/auth/login", "", map[string]any{"email": company.OwnerEmail, "password": "still-wrong"})
		if sixthAttempt.Status != http.StatusTooManyRequests {
			t.Fatalf("sixth failed attempt returned %d, want 429", sixthAttempt.Status)
		}

		sprayLimited := false
		for sprayNumber := 1; sprayNumber <= 30; sprayNumber++ {
			sprayAttempt := harness.Call(http.MethodPost, "/api/auth/login", "", map[string]any{"email": fmt.Sprintf("spray%d@rate.test", sprayNumber), "password": "guess-1234"})
			if sprayAttempt.Status == http.StatusTooManyRequests {
				sprayLimited = true
				break
			}
		}
		if !sprayLimited {
			t.Fatal("changing the email on every try got around the sign-in limit")
		}

		failedAttempts := harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM login_attempts WHERE email = $1 AND NOT succeeded`, company.OwnerEmail)
		if failedAttempts < 5 {
			t.Fatalf("recorded %d failed attempts, want at least 5 (failures must survive the 401 rollback)", failedAttempts)
		}
	})
}

func TestBrowserLoginSetsHardenedCookieAndHidesToken(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Cookie Shop", "owner@cookie.test")

		browserLogin := harness.Call(http.MethodPost, "/api/auth/login", "", map[string]any{"email": company.OwnerEmail, "password": company.OwnerPassword})
		if browserLogin.Status != http.StatusOK {
			t.Fatalf("browser login returned %d", browserLogin.Status)
		}
		if _, exposesToken := browserLogin.Data()["session_token"]; exposesToken {
			t.Fatal("browser login must not put the session token in the body")
		}

		sessionCookie := browserLogin.Headers.Get("Set-Cookie")
		lowerCaseCookie := strings.ToLower(sessionCookie)
		requiredParts := []string{"balce_session=", "httponly", "samesite=lax", "path=/"}
		for _, requiredPart := range requiredParts {
			if !strings.Contains(lowerCaseCookie, requiredPart) {
				t.Fatalf("cookie %q is missing %q", sessionCookie, requiredPart)
			}
		}
		isCloud := engineCase.Engine == config.EnginePostgres
		if isCloud && !strings.Contains(lowerCaseCookie, "secure") {
			t.Fatalf("cloud cookie %q must be Secure", sessionCookie)
		}

		cookieValue := strings.TrimPrefix(strings.Split(sessionCookie, ";")[0], "balce_session=")
		cookieRequest := harness.Send(http.MethodGet, "/api/auth/me", "", nil, map[string]string{"Cookie": "balce_session=" + cookieValue})
		if cookieRequest.Status != http.StatusOK {
			t.Fatalf("cookie-authenticated /me returned %d", cookieRequest.Status)
		}

		tokenDigest := sha256.Sum256([]byte(cookieValue))
		storedAsHash := harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM sessions WHERE token_hash = $1`, hex.EncodeToString(tokenDigest[:]))
		storedRaw := harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM sessions WHERE token_hash = $1`, cookieValue)
		if storedAsHash != 1 || storedRaw != 0 {
			t.Fatalf("session storage: hashed=%d raw=%d, want 1 and 0", storedAsHash, storedRaw)
		}
	})
}

func TestSessionsEndOnLogoutExpiryAndDeactivation(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Session Shop", "owner@session.test")

		unauthenticated := harness.Call(http.MethodGet, "/api/auth/me", "", nil)
		if unauthenticated.Status != http.StatusUnauthorized {
			t.Fatalf("no token returned %d, want 401", unauthenticated.Status)
		}
		forgedToken := harness.Call(http.MethodGet, "/api/auth/me", "forged-token", nil)
		if forgedToken.Status != http.StatusUnauthorized {
			t.Fatalf("forged token returned %d, want 401", forgedToken.Status)
		}

		logoutToken := harness.MustLogin(company.OwnerEmail, company.OwnerPassword)
		logoutResponse := harness.Call(http.MethodPost, "/api/auth/logout", logoutToken, nil)
		if logoutResponse.Status != http.StatusOK {
			t.Fatalf("logout returned %d", logoutResponse.Status)
		}
		afterLogout := harness.Call(http.MethodGet, "/api/auth/me", logoutToken, nil)
		if afterLogout.Status != http.StatusUnauthorized {
			t.Fatalf("token after logout returned %d, want 401", afterLogout.Status)
		}

		idleToken := harness.MustLogin(company.OwnerEmail, company.OwnerPassword)
		idleTokenDigest := sha256.Sum256([]byte(idleToken))
		idleTokenHash := hex.EncodeToString(idleTokenDigest[:])
		harness.ExecForCompany(company.Id, `UPDATE sessions SET last_seen_at = $1 WHERE token_hash = $2`, time.Now().UTC().Add(-13*time.Hour), idleTokenHash)
		afterIdle := harness.Call(http.MethodGet, "/api/auth/me", idleToken, nil)
		if afterIdle.Status != http.StatusUnauthorized {
			t.Fatalf("idle session returned %d, want 401", afterIdle.Status)
		}
		remainingIdleSessions := harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM sessions WHERE token_hash = $1`, idleTokenHash)
		if remainingIdleSessions != 0 {
			t.Fatal("an idle session must be deleted when it is rejected")
		}

		expiredToken := harness.MustLogin(company.OwnerEmail, company.OwnerPassword)
		expiredTokenDigest := sha256.Sum256([]byte(expiredToken))
		harness.ExecForCompany(company.Id, `UPDATE sessions SET expires_at = $1 WHERE token_hash = $2`, time.Now().UTC().Add(-time.Minute), hex.EncodeToString(expiredTokenDigest[:]))
		afterExpiry := harness.Call(http.MethodGet, "/api/auth/me", expiredToken, nil)
		if afterExpiry.Status != http.StatusUnauthorized {
			t.Fatalf("expired session returned %d, want 401", afterExpiry.Status)
		}

		cashier := createCashier(t, harness, company, "cashier@session.test")
		cashierToken := harness.MustLogin("cashier@session.test", "cashier-password-1")
		deactivate := harness.Call(http.MethodDelete, "/api/users/"+cashier, company.OwnerToken, nil)
		if deactivate.Status != http.StatusOK {
			t.Fatalf("deactivate returned %d: %v", deactivate.Status, deactivate.Body)
		}
		afterDeactivation := harness.Call(http.MethodGet, "/api/auth/me", cashierToken, nil)
		if afterDeactivation.Status != http.StatusUnauthorized {
			t.Fatalf("deactivated user's session returned %d, want 401", afterDeactivation.Status)
		}
		deactivatedLogin := harness.Call(http.MethodPost, "/api/auth/login", "", map[string]any{"email": "cashier@session.test", "password": "cashier-password-1"})
		if deactivatedLogin.Status != http.StatusUnauthorized {
			t.Fatalf("deactivated user logged in with %d", deactivatedLogin.Status)
		}
	})
}

func TestOriginGuardAndShopSwitching(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Origin Shop", "owner@origin.test")
		otherCompany := harness.CreateCompany("Other Shop", "owner@other.test")

		foreignOrigin := harness.Send(http.MethodPost, "/api/auth/logout", company.OwnerToken, nil, map[string]string{"Origin": "https://evil.test"})
		if foreignOrigin.Status != http.StatusForbidden || foreignOrigin.Code() != "origin_not_allowed" {
			t.Fatalf("foreign origin returned %d %s, want 403 origin_not_allowed", foreignOrigin.Status, foreignOrigin.Code())
		}
		allowedOrigin := harness.Send(http.MethodPost, "/api/auth/switch-shop", company.OwnerToken, map[string]any{"shop_id": company.ShopId}, map[string]string{"Origin": apptest.AllowedOrigin})
		if allowedOrigin.Status != http.StatusOK {
			t.Fatalf("allowed origin returned %d: %v", allowedOrigin.Status, allowedOrigin.Body)
		}

		foreignShop := harness.Call(http.MethodPost, "/api/auth/switch-shop", company.OwnerToken, map[string]any{"shop_id": otherCompany.ShopId})
		if foreignShop.Status != http.StatusForbidden {
			t.Fatalf("switching into another company's shop returned %d, want 403", foreignShop.Status)
		}

		cashierId := createCashier(t, harness, company, "cashier@origin.test")
		unassignedShop := harness.Call(http.MethodPut, "/api/users/"+cashierId, company.OwnerToken, map[string]any{
			"name":     "Cashier",
			"email":    "cashier@origin.test",
			"role_id":  cashierRoleId(t, harness, company),
			"shop_ids": []string{},
		})
		if unassignedShop.Status != http.StatusOK {
			t.Fatalf("clearing cashier shops returned %d: %v", unassignedShop.Status, unassignedShop.Body)
		}
		cashierToken := harness.MustLogin("cashier@origin.test", "cashier-password-1")
		cashierSwitch := harness.Call(http.MethodPost, "/api/auth/switch-shop", cashierToken, map[string]any{"shop_id": company.ShopId})
		if cashierSwitch.Status != http.StatusForbidden {
			t.Fatalf("cashier switched into an unassigned shop with %d, want 403", cashierSwitch.Status)
		}
	})
}

func createCashier(t *testing.T, harness *apptest.Harness, company apptest.Company, email string) string {
	t.Helper()

	createUser := harness.Call(http.MethodPost, "/api/users", company.OwnerToken, map[string]any{
		"name":     "Cashier",
		"email":    email,
		"password": "cashier-password-1",
		"role_id":  cashierRoleId(t, harness, company),
		"shop_ids": []string{company.ShopId.String()},
	})
	if createUser.Status != http.StatusCreated {
		t.Fatalf("create cashier returned %d: %v", createUser.Status, createUser.Body)
	}

	userId, _ := createUser.Data()["id"].(string)
	return userId
}

func cashierRoleId(t *testing.T, harness *apptest.Harness, company apptest.Company) string {
	t.Helper()

	roleList := harness.Call(http.MethodGet, "/api/roles?limit=100", company.OwnerToken, nil)
	for _, roleItem := range roleList.Items() {
		roleFields, _ := roleItem.(map[string]any)
		if roleFields["name"] == "Cashier" {
			return roleFields["id"].(string)
		}
	}

	createRole := harness.Call(http.MethodPost, "/api/roles", company.OwnerToken, map[string]any{
		"name":           "Cashier",
		"permission_ids": []string{"sales:view", "sales:create", "products:view"},
	})
	if createRole.Status != http.StatusCreated {
		t.Fatalf("create cashier role returned %d: %v", createRole.Status, createRole.Body)
	}

	return createRole.Data()["id"].(string)
}

func TestEachUserChoosesTheirOwnLanguage(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Language Shop", "owner@language.test")
		createCashier(t, harness, company, "cashier@language.test")
		cashierToken := harness.MustLogin("cashier@language.test", "cashier-password-1")

		chosen := harness.Call(http.MethodPut, "/api/auth/language", cashierToken, map[string]any{"locale": "sw"})
		if chosen.Status != http.StatusOK || chosen.Data()["locale"] != "sw" {
			t.Fatalf("choosing Swahili returned %d %v", chosen.Status, chosen.Body)
		}
		if harness.Call(http.MethodGet, "/api/auth/me", cashierToken, nil).Data()["locale"] != "sw" {
			t.Fatal("the chosen language was not kept")
		}
		if harness.Call(http.MethodGet, "/api/auth/me", company.OwnerToken, nil).Data()["locale"] != nil {
			t.Fatal("one user's language changed another user's")
		}

		unsupported := harness.Call(http.MethodPut, "/api/auth/language", cashierToken, map[string]any{"locale": "fr"})
		if unsupported.Status != http.StatusBadRequest || unsupported.Code() != "validation_failed" {
			t.Fatalf("an unsupported language returned %d %v", unsupported.Status, unsupported.Body)
		}

		companyDefault := harness.Call(http.MethodPut, "/api/auth/language", cashierToken, map[string]any{"locale": nil})
		if companyDefault.Status != http.StatusOK || companyDefault.Data()["locale"] != nil {
			t.Fatalf("going back to the company language returned %d %v", companyDefault.Status, companyDefault.Body)
		}
	})
}

func TestSetupWarnsWhenTheOldAppsDataIsOnThisComputer(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		if engineCase.Engine != config.EngineSqlite {
			return
		}
		harness := apptest.StartDesktop(t, engineCase)
		if harness.Call(http.MethodGet, "/api/setup/status", "", nil).Data()["old_data_found"] != false {
			t.Fatal("old data reported on a computer without the old app")
		}

		oldDatabasePath := filepath.Join(harness.Config.DataDirectory, "balce.db")
		os.WriteFile(oldDatabasePath, []byte("SQLite format 3\x00"), 0o600)
		if harness.Call(http.MethodGet, "/api/setup/status", "", nil).Data()["old_data_found"] != true {
			t.Fatal("the old app's database was not noticed before setup")
		}

		harness.CreateCompany("Duka Jipya", "owner@olddata.test")
		if harness.Call(http.MethodGet, "/api/setup/status", "", nil).Data()["old_data_found"] != false {
			t.Fatal("the notice stayed after the business was set up")
		}
	})
}

func checkCloudSignUp(t *testing.T, harness *apptest.Harness, setupBody map[string]any) {
	statusResponse := harness.Call(http.MethodGet, "/api/setup/status", "", nil)
	if statusResponse.Data()["signup_open"] != true {
		t.Fatalf("cloud must report signup open, got %v", statusResponse.Body)
	}

	firstBusiness := harness.Call(http.MethodPost, "/api/setup", "", setupBody)
	if firstBusiness.Status != http.StatusCreated {
		t.Fatalf("cloud sign-up returned %d: %v", firstBusiness.Status, firstBusiness.Body)
	}
	sameEmail := harness.Call(http.MethodPost, "/api/setup", "", setupBody)
	if sameEmail.Status != http.StatusConflict || sameEmail.Code() != "email_taken" {
		t.Fatalf("a second business with the same email returned %d %v", sameEmail.Status, sameEmail.Body)
	}
	secondBusiness := harness.Call(http.MethodPost, "/api/setup", "", map[string]any{
		"business_name":  "Duka la Baba",
		"owner_name":     "Baba",
		"owner_email":    "baba@duka.test",
		"owner_password": "strong-password-2",
	})
	if secondBusiness.Status != http.StatusCreated {
		t.Fatalf("a second business returned %d: %v", secondBusiness.Status, secondBusiness.Body)
	}

	mamaToken := harness.MustLogin("mama@duka.test", "strong-password-1")
	babaToken := harness.MustLogin("baba@duka.test", "strong-password-2")
	mamaMe := harness.Call(http.MethodGet, "/api/auth/me", mamaToken, nil).Data()
	babaMe := harness.Call(http.MethodGet, "/api/auth/me", babaToken, nil).Data()
	if mamaMe["company_name"] != "Duka la Mama" || babaMe["company_name"] != "Duka la Baba" || mamaMe["must_change_password"] == true {
		t.Fatalf("each owner must land in their own business: %v %v", mamaMe, babaMe)
	}
	harness.Call(http.MethodPost, "/api/products", mamaToken, map[string]any{"sku": "MAMA-1", "name": "Sukari", "price": 3000})
	babaProducts := harness.Call(http.MethodGet, "/api/products", babaToken, nil)
	if strings.Contains(fmt.Sprint(babaProducts.Body), "Sukari") {
		t.Fatal("one business saw another business's products")
	}

	lastStatus := 0
	for attempt := 0; attempt < 10; attempt++ {
		lastStatus = harness.Call(http.MethodPost, "/api/setup", "", setupBody).Status
	}
	if lastStatus != http.StatusTooManyRequests {
		t.Fatalf("sign-ups from one network were not limited, last status %d", lastStatus)
	}
}
