package admin_test

import (
	"context"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/admin"
	"github.com/chrisostomemataba/balceinv-api/internal/config"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
)

type teamMember struct {
	email      string
	password   string
	totpSecret string
}

func saveTeamMember(t *testing.T, harness *apptest.Harness, email string, role string) teamMember {
	t.Helper()
	adminService := admin.NewService(admin.NewRepository(), nil, harness.Database.IsPostgres())
	password, totpSecret, saveError := adminService.SaveStaff(context.Background(), harness.Database.Writer, email, "Team "+role, role)
	if saveError != nil {
		t.Fatalf("save team member: %v", saveError)
	}
	return teamMember{email: email, password: password, totpSecret: totpSecret}
}

func signIn(t *testing.T, harness *apptest.Harness, member teamMember, code string) (apptest.Response, string) {
	t.Helper()
	signInResponse := harness.Call(http.MethodPost, "/api/admin/sign-in", "", map[string]any{"email": member.email, "password": member.password, "code": code})
	cookieValue := ""
	for _, setCookie := range signInResponse.Headers.Values("Set-Cookie") {
		if strings.HasPrefix(setCookie, admin.SessionCookieName+"=") {
			cookieValue = strings.SplitN(strings.TrimPrefix(setCookie, admin.SessionCookieName+"="), ";", 2)[0]
		}
	}
	return signInResponse, cookieValue
}

func currentCode(t *testing.T, member teamMember) string {
	t.Helper()
	code, codeError := admin.TotpCode(member.totpSecret, time.Now().UTC())
	if codeError != nil {
		t.Fatalf("totp code: %v", codeError)
	}
	return code
}

func asStaff(harness *apptest.Harness, cookieValue string, method string, path string, body any) apptest.Response {
	return harness.Send(method, path, "", body, map[string]string{"Cookie": admin.SessionCookieName + "=" + cookieValue})
}

func TestTheAdminPanelSeesEveryShopOnlyForSignedInTeamMembers(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		if engineCase.Engine != config.EnginePostgres {
			if harness.Call(http.MethodPost, "/api/admin/sign-in", "", map[string]any{"email": "a@b.test", "password": "x", "code": "123456"}).Status != http.StatusNotFound {
				t.Fatal("the desktop server answered the admin panel")
			}
			return
		}

		duka := harness.CreateCompany("Duka Admin Test", "owner@dukaadmin.test")
		harness.CreateCompany("Second Admin Test", "owner@secondadmin.test")
		support := saveTeamMember(t, harness, "support@balce.test", admin.RoleSupport)
		chief := saveTeamMember(t, harness, "chief@balce.test", admin.RoleAdmin)

		wrongCode, _ := signIn(t, harness, support, "000000")
		if wrongCode.Code() != "admin_bad_sign_in" {
			t.Fatalf("a wrong code returned %d %v", wrongCode.Status, wrongCode.Body)
		}
		signedIn, supportCookie := signIn(t, harness, support, currentCode(t, support))
		if signedIn.Status != http.StatusOK || supportCookie == "" {
			t.Fatalf("signing in returned %d %v", signedIn.Status, signedIn.Body)
		}
		_, chiefCookie := signIn(t, harness, chief, currentCode(t, chief))

		if harness.Call(http.MethodGet, "/api/admin/shops", "", nil).Code() != "admin_sign_in_required" {
			t.Fatal("the shop list opened without signing in")
		}
		if harness.Call(http.MethodGet, "/api/admin/shops", duka.OwnerToken, nil).Code() != "admin_sign_in_required" {
			t.Fatal("a shop owner opened the admin panel")
		}

		shopList := asStaff(harness, supportCookie, http.MethodGet, "/api/admin/shops", nil)
		if shopList.Status != http.StatusOK || shopList.Data()["total"] != float64(2) {
			t.Fatalf("the shop list returned %d %v", shopList.Status, shopList.Body)
		}
		shopDetail := asStaff(harness, supportCookie, http.MethodGet, "/api/admin/shops/"+duka.Id.String(), nil)
		detailUsers := shopDetail.Data()["users"].([]any)
		if shopDetail.Status != http.StatusOK || len(detailUsers) != 1 || detailUsers[0].(map[string]any)["email"] != "owner@dukaadmin.test" {
			t.Fatalf("the shop detail returned %d %v", shopDetail.Status, shopDetail.Body)
		}

		extended := asStaff(harness, supportCookie, http.MethodPost, "/api/admin/shops/"+duka.Id.String()+"/extend-trial", map[string]any{"days": 10})
		if extended.Status != http.StatusOK {
			t.Fatalf("extending the trial returned %d %v", extended.Status, extended.Body)
		}
		trialEnd, parseError := time.Parse(time.RFC3339, extended.Data()["trial_ends_at"].(string))
		if parseError != nil || trialEnd.Before(time.Now().Add(20*24*time.Hour)) {
			t.Fatalf("the trial now ends %v (%v), want about 24 days away", trialEnd, parseError)
		}

		reset := asStaff(harness, supportCookie, http.MethodPost, "/api/admin/shops/"+duka.Id.String()+"/users/"+duka.OwnerId.String()+"/reset-password", nil)
		newPassword, _ := reset.Data()["one_time_password"].(string)
		if reset.Status != http.StatusOK || newPassword == "" {
			t.Fatalf("resetting the password returned %d %v", reset.Status, reset.Body)
		}
		if harness.Call(http.MethodGet, "/api/auth/me", duka.OwnerToken, nil).Status != http.StatusUnauthorized {
			t.Fatal("the owner stayed signed in after a password reset")
		}
		ownerToken := harness.MustLogin("owner@dukaadmin.test", newPassword)
		submitted := harness.Call(http.MethodPost, "/api/support", ownerToken, map[string]any{"topic": "problem", "message": "The printer stopped printing receipts", "contact_phone": "0712000000"})
		if submitted.Status >= 300 {
			t.Fatalf("sending a support message returned %d %v", submitted.Status, submitted.Body)
		}
		openMessages := asStaff(harness, supportCookie, http.MethodGet, "/api/admin/support", nil).Body["data"].([]any)
		if len(openMessages) != 1 || openMessages[0].(map[string]any)["company_name"] != "Duka Admin Test" {
			t.Fatalf("the support inbox showed %v", openMessages)
		}
		messageId := openMessages[0].(map[string]any)["id"].(string)
		if asStaff(harness, supportCookie, http.MethodPost, "/api/admin/support/"+messageId+"/handled", nil).Status != http.StatusOK {
			t.Fatal("marking the message handled failed")
		}
		if len(asStaff(harness, supportCookie, http.MethodGet, "/api/admin/support", nil).Body["data"].([]any)) != 0 {
			t.Fatal("a handled message stayed in the open inbox")
		}

		newShopBody := map[string]any{"business_name": "Made By Team", "owner_name": "Juma", "owner_email": "juma@madebyteam.test"}
		if asStaff(harness, supportCookie, http.MethodPost, "/api/admin/shops", newShopBody).Code() != "admin_forbidden" {
			t.Fatal("a support team member created a shop")
		}
		created := asStaff(harness, chiefCookie, http.MethodPost, "/api/admin/shops", newShopBody)
		createdPassword, _ := created.Data()["one_time_password"].(string)
		if created.Status != http.StatusCreated || createdPassword == "" {
			t.Fatalf("creating a shop returned %d %v", created.Status, created.Body)
		}
		harness.MustLogin("juma@madebyteam.test", createdPassword)

		if asStaff(harness, supportCookie, http.MethodGet, "/api/admin/audit", nil).Code() != "admin_forbidden" {
			t.Fatal("a support team member read the audit log")
		}
		auditEntries := asStaff(harness, chiefCookie, http.MethodGet, "/api/admin/audit", nil).Body["data"].([]any)
		actions := map[string]bool{}
		for _, rawEntry := range auditEntries {
			actions[rawEntry.(map[string]any)["action"].(string)] = true
		}
		for _, expectedAction := range []string{"signed_in", "extended_trial", "reset_password", "handled_support", "created_shop"} {
			if !actions[expectedAction] {
				t.Fatalf("the audit log is missing %s: %v", expectedAction, actions)
			}
		}

		asStaff(harness, supportCookie, http.MethodPost, "/api/admin/sign-out", nil)
		if asStaff(harness, supportCookie, http.MethodGet, "/api/admin/me", nil).Code() != "admin_sign_in_required" {
			t.Fatal("the session still worked after signing out")
		}
	})
}
