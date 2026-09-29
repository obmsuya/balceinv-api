package access_test

import (
	"net/http"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
)

func TestRoleRulesAndPermissionGranting(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Roles Shop", "owner@roles.test")
		otherCompany := harness.CreateCompany("Roles Rival", "owner@rolesrival.test")

		sameNameElsewhere := harness.Call(http.MethodPost, "/api/roles", otherCompany.OwnerToken, map[string]any{"name": "Manager"})
		managerRole := harness.Call(http.MethodPost, "/api/roles", company.OwnerToken, map[string]any{
			"name":           "Manager",
			"permission_ids": []string{"users:view", "users:edit", "roles:view", "roles:edit"},
		})
		if sameNameElsewhere.Status != http.StatusCreated || managerRole.Status != http.StatusCreated {
			t.Fatalf("two companies creating Manager returned %d and %d, want 201 for both", sameNameElsewhere.Status, managerRole.Status)
		}

		duplicateName := harness.Call(http.MethodPost, "/api/roles", company.OwnerToken, map[string]any{"name": "Manager"})
		if duplicateName.Status != http.StatusConflict {
			t.Fatalf("duplicate role name returned %d, want 409", duplicateName.Status)
		}

		unknownPermission := harness.Call(http.MethodPost, "/api/roles", company.OwnerToken, map[string]any{"name": "Odd", "permission_ids": []string{"rockets:launch"}})
		if unknownPermission.Status != http.StatusBadRequest {
			t.Fatalf("unknown permission returned %d, want 400", unknownPermission.Status)
		}

		managerRoleId := managerRole.Data()["id"].(string)
		createManager := harness.Call(http.MethodPost, "/api/users", company.OwnerToken, map[string]any{
			"name": "Manager", "email": "manager@roles.test", "password": "manager-password", "role_id": managerRoleId,
		})
		if createManager.Status != http.StatusCreated {
			t.Fatalf("create manager returned %d: %v", createManager.Status, createManager.Body)
		}
		managerId := createManager.Data()["id"].(string)
		managerToken := harness.MustLogin("manager@roles.test", "manager-password")

		selfEscalation := harness.Call(http.MethodPost, "/api/permissions/assign-user", managerToken, map[string]any{
			"user_id": managerId, "permission_ids": []string{"settings:edit"},
		})
		if selfEscalation.Status != http.StatusForbidden {
			t.Fatalf("manager granting themselves settings:edit returned %d, want 403", selfEscalation.Status)
		}

		roleEscalation := harness.Call(http.MethodPost, "/api/permissions/assign-role", managerToken, map[string]any{
			"role_id": managerRoleId, "permission_ids": []string{"users:view", "reports:view"},
		})
		if roleEscalation.Status != http.StatusForbidden {
			t.Fatalf("manager widening their own role returned %d, want 403", roleEscalation.Status)
		}

		heldGrant := harness.Call(http.MethodPost, "/api/permissions/assign-user", managerToken, map[string]any{
			"user_id": managerId, "permission_ids": []string{"users:view"},
		})
		if heldGrant.Status != http.StatusOK {
			t.Fatalf("granting a held permission returned %d: %v", heldGrant.Status, heldGrant.Body)
		}

		ownPermissions := harness.Call(http.MethodGet, "/api/permissions/user/"+managerId, managerToken, nil)
		ownPermissionList, _ := ownPermissions.Body["data"].([]any)
		if ownPermissions.Status != http.StatusOK || len(ownPermissionList) != 4 {
			t.Fatalf("manager's own permissions returned %d with %d entries, want 4", ownPermissions.Status, len(ownPermissionList))
		}

		ownerRoleId := ""
		roleList := harness.Call(http.MethodGet, "/api/roles", company.OwnerToken, nil)
		for _, roleItem := range roleList.Items() {
			roleFields := roleItem.(map[string]any)
			if roleFields["is_owner"] == true {
				ownerRoleId = roleFields["id"].(string)
			}
		}

		deleteOwnerRole := harness.Call(http.MethodDelete, "/api/roles/"+ownerRoleId, company.OwnerToken, nil)
		editOwnerRole := harness.Call(http.MethodPost, "/api/permissions/assign-role", company.OwnerToken, map[string]any{"role_id": ownerRoleId, "permission_ids": []string{}})
		if deleteOwnerRole.Status != http.StatusConflict || editOwnerRole.Status != http.StatusConflict {
			t.Fatalf("owner role delete/edit returned %d/%d, want 409/409", deleteOwnerRole.Status, editOwnerRole.Status)
		}

		deleteRoleInUse := harness.Call(http.MethodDelete, "/api/roles/"+managerRoleId, company.OwnerToken, nil)
		if deleteRoleInUse.Status != http.StatusConflict {
			t.Fatalf("deleting a role in use returned %d, want 409", deleteRoleInUse.Status)
		}

		unusedRole := harness.Call(http.MethodPost, "/api/roles", company.OwnerToken, map[string]any{"name": "Temporary"})
		deleteUnused := harness.Call(http.MethodDelete, "/api/roles/"+unusedRole.Data()["id"].(string), company.OwnerToken, nil)
		if deleteUnused.Status != http.StatusOK {
			t.Fatalf("deleting an unused role returned %d", deleteUnused.Status)
		}

		malformedId := harness.Call(http.MethodGet, "/api/roles/not-a-uuid", company.OwnerToken, nil)
		if malformedId.Status != http.StatusNotFound {
			t.Fatalf("malformed id returned %d, want 404", malformedId.Status)
		}
	})
}
