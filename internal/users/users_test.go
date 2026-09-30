package users_test

import (
	"fmt"
	"net/http"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
)

func createRole(t *testing.T, harness *apptest.Harness, sessionToken string, roleName string, permissionIds []string) string {
	t.Helper()
	createResponse := harness.Call(http.MethodPost, "/api/roles", sessionToken, map[string]any{
		"name":           roleName,
		"permission_ids": permissionIds,
	})
	if createResponse.Status != http.StatusCreated {
		t.Fatalf("create role %s returned %d: %v", roleName, createResponse.Status, createResponse.Body)
	}
	return createResponse.Data()["id"].(string)
}

func createUser(t *testing.T, harness *apptest.Harness, sessionToken string, email string, roleId string, shopIds []string) apptest.Response {
	t.Helper()
	return harness.Call(http.MethodPost, "/api/users", sessionToken, map[string]any{
		"name":     "User " + email,
		"email":    email,
		"password": "user-password-1",
		"role_id":  roleId,
		"shop_ids": shopIds,
	})
}

func ownerRoleId(t *testing.T, harness *apptest.Harness, sessionToken string) string {
	t.Helper()
	roleList := harness.Call(http.MethodGet, "/api/roles", sessionToken, nil)
	for _, roleItem := range roleList.Items() {
		roleFields := roleItem.(map[string]any)
		if roleFields["is_owner"] == true {
			return roleFields["id"].(string)
		}
	}
	t.Fatal("owner role not found")
	return ""
}

func TestUserRulesProtectOwnersAndEmails(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Rules Shop", "owner@rules.test")
		otherCompany := harness.CreateCompany("Rival Shop", "owner@rival.test")

		managerRoleId := createRole(t, harness, company.OwnerToken, "Manager", []string{"users:view", "users:create", "users:edit", "users:delete"})
		cashierRoleId := createRole(t, harness, company.OwnerToken, "Cashier", []string{"sales:create"})
		rivalRoleId := createRole(t, harness, otherCompany.OwnerToken, "Manager", []string{"users:view"})

		invalidBody := harness.Call(http.MethodPost, "/api/users", company.OwnerToken, map[string]any{"email": "not-an-email"})
		if invalidBody.Status != http.StatusBadRequest || invalidBody.Code() != "validation_failed" {
			t.Fatalf("invalid body returned %d %s, want 400 validation_failed", invalidBody.Status, invalidBody.Code())
		}
		fieldErrors, _ := invalidBody.Body["fields"].([]any)
		if len(fieldErrors) < 3 {
			t.Fatalf("expected field errors for name, email, password and role, got %v", fieldErrors)
		}

		duplicateEmail := createUser(t, harness, company.OwnerToken, otherCompany.OwnerEmail, cashierRoleId, nil)
		if duplicateEmail.Status != http.StatusConflict || duplicateEmail.Code() != "email_taken" {
			t.Fatalf("email used by another company returned %d %s, want 409 email_taken", duplicateEmail.Status, duplicateEmail.Code())
		}

		foreignRole := createUser(t, harness, company.OwnerToken, "foreign-role@rules.test", rivalRoleId, nil)
		if foreignRole.Status != http.StatusNotFound {
			t.Fatalf("another company's role returned %d, want 404", foreignRole.Status)
		}

		foreignShop := createUser(t, harness, company.OwnerToken, "foreign-shop@rules.test", cashierRoleId, []string{otherCompany.ShopId.String()})
		if foreignShop.Status != http.StatusNotFound {
			t.Fatalf("another company's shop returned %d, want 404", foreignShop.Status)
		}
		leftoverUsers := harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM users WHERE email = $1`, "foreign-shop@rules.test")
		if leftoverUsers != 0 {
			t.Fatal("a rejected create left a user row behind")
		}

		managerCreate := createUser(t, harness, company.OwnerToken, "manager@rules.test", managerRoleId, []string{company.ShopId.String()})
		if managerCreate.Status != http.StatusCreated {
			t.Fatalf("create manager returned %d: %v", managerCreate.Status, managerCreate.Body)
		}
		managerToken := harness.MustLogin("manager@rules.test", "user-password-1")

		ownerByManager := createUser(t, harness, managerToken, "second-owner@rules.test", ownerRoleId(t, harness, company.OwnerToken), nil)
		if ownerByManager.Status != http.StatusForbidden {
			t.Fatalf("manager creating an owner returned %d, want 403", ownerByManager.Status)
		}

		demoteOwnerByManager := harness.Call(http.MethodPost, "/api/roles/assign", managerToken, map[string]any{"user_id": company.OwnerId, "role_id": cashierRoleId})
		if demoteOwnerByManager.Status != http.StatusForbidden {
			t.Fatalf("manager demoting the owner returned %d, want 403", demoteOwnerByManager.Status)
		}

		demoteLastOwner := harness.Call(http.MethodPost, "/api/roles/assign", company.OwnerToken, map[string]any{"user_id": company.OwnerId, "role_id": cashierRoleId})
		if demoteLastOwner.Status != http.StatusConflict {
			t.Fatalf("demoting the last owner returned %d, want 409", demoteLastOwner.Status)
		}

		deactivateSelf := harness.Call(http.MethodDelete, "/api/users/"+company.OwnerId.String(), company.OwnerToken, nil)
		if deactivateSelf.Status != http.StatusConflict {
			t.Fatalf("owner deactivating themselves returned %d, want 409", deactivateSelf.Status)
		}

		cashierOnly := createUser(t, harness, company.OwnerToken, "cashier@rules.test", cashierRoleId, nil)
		cashierToken := harness.MustLogin("cashier@rules.test", "user-password-1")
		cashierListsUsers := harness.Call(http.MethodGet, "/api/users", cashierToken, nil)
		if cashierListsUsers.Status != http.StatusForbidden {
			t.Fatalf("cashier listing users returned %d, want 403", cashierListsUsers.Status)
		}

		cashierId := cashierOnly.Data()["id"].(string)
		cashierPromotion := harness.Call(http.MethodPost, "/api/roles/assign", company.OwnerToken, map[string]any{"user_id": cashierId, "role_id": managerRoleId})
		if cashierPromotion.Status != http.StatusOK {
			t.Fatalf("promoting cashier returned %d: %v", cashierPromotion.Status, cashierPromotion.Body)
		}
		afterRoleChange := harness.Call(http.MethodGet, "/api/auth/me", cashierToken, nil)
		if afterRoleChange.Status != http.StatusUnauthorized {
			t.Fatalf("a role change must end the user's sessions, got %d", afterRoleChange.Status)
		}
	})
}

func TestPasswordChangeKeepsOwnSessionAndRevokesOthers(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Password Shop", "owner@password.test")
		secondDeviceToken := harness.MustLogin(company.OwnerEmail, company.OwnerPassword)

		tooShort := harness.Call(http.MethodPost, "/api/users/update-password", company.OwnerToken, map[string]any{"user_id": company.OwnerId, "new_password": "short"})
		if tooShort.Status != http.StatusBadRequest {
			t.Fatalf("short password returned %d, want 400", tooShort.Status)
		}

		changeOwn := harness.Call(http.MethodPost, "/api/users/update-password", company.OwnerToken, map[string]any{"user_id": company.OwnerId, "new_password": "brand-new-password"})
		if changeOwn.Status != http.StatusOK {
			t.Fatalf("change own password returned %d: %v", changeOwn.Status, changeOwn.Body)
		}

		currentSession := harness.Call(http.MethodGet, "/api/auth/me", company.OwnerToken, nil)
		if currentSession.Status != http.StatusOK {
			t.Fatalf("the session that changed the password must survive, got %d", currentSession.Status)
		}
		otherSession := harness.Call(http.MethodGet, "/api/auth/me", secondDeviceToken, nil)
		if otherSession.Status != http.StatusUnauthorized {
			t.Fatalf("other sessions must end after a password change, got %d", otherSession.Status)
		}

		oldPassword := harness.Call(http.MethodPost, "/api/auth/login", "", map[string]any{"email": company.OwnerEmail, "password": company.OwnerPassword})
		if oldPassword.Status != http.StatusUnauthorized {
			t.Fatalf("old password still works: %d", oldPassword.Status)
		}
		harness.MustLogin(company.OwnerEmail, "brand-new-password")
	})
}

func TestUserListIsPaginatedWithoutExtraQueries(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Busy Shop", "owner@busy.test")
		cashierRoleId := createRole(t, harness, company.OwnerToken, "Cashier", []string{"sales:create"})

		for userNumber := 1; userNumber <= 30; userNumber++ {
			createResponse := createUser(t, harness, company.OwnerToken, fmt.Sprintf("cashier%02d@busy.test", userNumber), cashierRoleId, []string{company.ShopId.String()})
			if createResponse.Status != http.StatusCreated {
				t.Fatalf("create cashier %d returned %d", userNumber, createResponse.Status)
			}
		}

		var fullPage apptest.Response
		fullPageQueries := harness.CountQueries(func() {
			fullPage = harness.Call(http.MethodGet, "/api/users?limit=25", company.OwnerToken, nil)
		})
		if len(fullPage.Items()) != 25 || fullPage.Data()["total"] != float64(31) {
			t.Fatalf("first page has %d items, total %v; want 25 of 31", len(fullPage.Items()), fullPage.Data()["total"])
		}
		if fullPageQueries > 4 {
			t.Fatalf("listing 25 users ran %d queries, want at most 4", fullPageQueries)
		}
		firstUser := fullPage.Items()[0].(map[string]any)
		firstUserShops, _ := firstUser["shop_ids"].([]any)
		if len(firstUserShops) != 1 {
			t.Fatalf("shop ids were not attached to listed users: %v", firstUser)
		}

		beyondEnd := harness.Call(http.MethodGet, "/api/users?limit=25&offset=500", company.OwnerToken, nil)
		if beyondEnd.Status != http.StatusOK || len(beyondEnd.Items()) != 0 {
			t.Fatalf("offset beyond the end returned %d with %d items", beyondEnd.Status, len(beyondEnd.Items()))
		}

		searchResult := harness.Call(http.MethodGet, "/api/users?q=CASHIER07", company.OwnerToken, nil)
		if len(searchResult.Items()) != 1 {
			t.Fatalf("search returned %d users, want 1", len(searchResult.Items()))
		}
	})
}

func TestUserEditorsCannotReachAboveTheirOwnPermissions(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Reach Shop", "owner@reach.test")
		shopIds := []string{company.ShopId.String()}

		supervisorRoleId := createRole(t, harness, company.OwnerToken, "Supervisor", []string{"users:view", "users:create", "users:edit", "sales:create"})
		managerRoleId := createRole(t, harness, company.OwnerToken, "Manager", []string{"users:view", "users:edit", "sales:create", "reports:view", "settings:edit"})
		cashierRoleId := createRole(t, harness, company.OwnerToken, "Cashier", []string{"sales:create"})

		supervisorId := createUser(t, harness, company.OwnerToken, "supervisor@reach.test", supervisorRoleId, shopIds).Data()["id"].(string)
		managerId := createUser(t, harness, company.OwnerToken, "manager@reach.test", managerRoleId, shopIds).Data()["id"].(string)
		supervisorToken := harness.MustLogin("supervisor@reach.test", "user-password-1")

		if answer := createUser(t, harness, supervisorToken, "new-manager@reach.test", managerRoleId, shopIds); answer.Status != http.StatusForbidden || answer.Code() != "cannot_grant_unheld_permission" {
			t.Fatalf("a supervisor created a manager: %d %v", answer.Status, answer.Body)
		}
		if answer := harness.Call(http.MethodPost, "/api/roles/assign", supervisorToken, map[string]any{"user_id": supervisorId, "role_id": managerRoleId}); answer.Status != http.StatusForbidden {
			t.Fatalf("a supervisor promoted themselves: %d %v", answer.Status, answer.Body)
		}
		if answer := harness.Call(http.MethodPost, "/api/users/update-password", supervisorToken, map[string]any{"user_id": managerId, "new_password": "taken-over-123"}); answer.Status != http.StatusForbidden || answer.Code() != "user_above_you" {
			t.Fatalf("a supervisor reset a manager's password: %d %v", answer.Status, answer.Body)
		}
		if answer := harness.Call(http.MethodDelete, "/api/users/"+managerId, supervisorToken, nil); answer.Status != http.StatusForbidden {
			t.Fatalf("a supervisor turned off a manager: %d %v", answer.Status, answer.Body)
		}
		managerLogin := harness.Call(http.MethodPost, "/api/auth/login", "", map[string]any{"email": "manager@reach.test", "password": "user-password-1"})
		if managerLogin.Status != http.StatusOK {
			t.Fatalf("the manager's password changed: %d", managerLogin.Status)
		}

		cashierAnswer := createUser(t, harness, supervisorToken, "cashier@reach.test", cashierRoleId, shopIds)
		if cashierAnswer.Status != http.StatusCreated {
			t.Fatalf("a supervisor could not add a cashier: %d %v", cashierAnswer.Status, cashierAnswer.Body)
		}
		cashierId := cashierAnswer.Data()["id"].(string)
		if answer := harness.Call(http.MethodPost, "/api/users/update-password", supervisorToken, map[string]any{"user_id": cashierId, "new_password": "cashier-new-123"}); answer.Status != http.StatusOK {
			t.Fatalf("a supervisor could not reset a cashier's password: %d %v", answer.Status, answer.Body)
		}
		if answer := harness.Call(http.MethodPost, "/api/roles/assign", company.OwnerToken, map[string]any{"user_id": cashierId, "role_id": managerRoleId}); answer.Status != http.StatusOK {
			t.Fatalf("the owner could not promote a cashier: %d %v", answer.Status, answer.Body)
		}
	})
}

func TestRemovingAShopSignsThePersonOut(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Shop Access", "owner@shopaccess.test")
		cashierRoleId := createRole(t, harness, company.OwnerToken, "Till", []string{"sales:create"})
		cashierId := createUser(t, harness, company.OwnerToken, "till@shopaccess.test", cashierRoleId, []string{company.ShopId.String()}).Data()["id"].(string)
		cashierToken := harness.MustLogin("till@shopaccess.test", "user-password-1")
		secondShopId := harness.Call(http.MethodPost, "/api/shops", company.OwnerToken, map[string]any{"name": "Second Branch", "receipt_prefix": "SEC"}).Data()["id"].(string)

		updateShops := func(shopIds []string) {
			t.Helper()
			answer := harness.Call(http.MethodPut, "/api/users/"+cashierId, company.OwnerToken, map[string]any{
				"name": "Till", "email": "till@shopaccess.test", "role_id": cashierRoleId, "shop_ids": shopIds,
			})
			if answer.Status != http.StatusOK {
				t.Fatalf("updating shops returned %d %v", answer.Status, answer.Body)
			}
		}

		updateShops([]string{company.ShopId.String(), secondShopId})
		if harness.Call(http.MethodGet, "/api/auth/me", cashierToken, nil).Status != http.StatusOK {
			t.Fatal("adding a shop signed the cashier out")
		}

		updateShops([]string{secondShopId})
		if harness.Call(http.MethodGet, "/api/auth/me", cashierToken, nil).Status != http.StatusUnauthorized {
			t.Fatal("the cashier kept working after losing a shop")
		}
	})
}
