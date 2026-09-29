package server_test

import (
	"context"
	"net/http"
	"strings"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/config"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/google/uuid"
)

func TestTenantsNeverSeeEachOther(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		firstCompany := harness.CreateCompany("First Company", "owner@first.test")
		secondCompany := harness.CreateCompany("Second Company", "owner@second.test")

		secondRole := harness.Call(http.MethodPost, "/api/roles", secondCompany.OwnerToken, map[string]any{"name": "Second Only Role"})
		secondRoleId := secondRole.Data()["id"].(string)
		secondUser := harness.Call(http.MethodPost, "/api/users", secondCompany.OwnerToken, map[string]any{
			"name": "Second Only User", "email": "only@second.test", "password": "second-password", "role_id": secondRoleId,
		})
		secondUserId := secondUser.Data()["id"].(string)

		listEndpoints := []string{"/api/users?limit=100", "/api/roles?limit=100", "/api/auth/me", "/api/permissions/user/" + firstCompany.OwnerId.String()}
		for _, listEndpoint := range listEndpoints {
			listResponse := harness.Call(http.MethodGet, listEndpoint, firstCompany.OwnerToken, nil)
			if listResponse.Status != http.StatusOK {
				t.Fatalf("%s returned %d", listEndpoint, listResponse.Status)
			}
			serializedBody := strings.ToLower(stringify(listResponse.Body))
			leakedMarkers := []string{"second only", "only@second.test", "owner@second.test", secondCompany.Id.String(), secondCompany.ShopId.String()}
			for _, leakedMarker := range leakedMarkers {
				if strings.Contains(serializedBody, strings.ToLower(leakedMarker)) {
					t.Fatalf("%s leaked %q from the second company", listEndpoint, leakedMarker)
				}
			}
		}

		crossTenantRequests := []struct {
			method string
			path   string
			body   map[string]any
		}{
			{method: http.MethodGet, path: "/api/users/" + secondUserId},
			{method: http.MethodPut, path: "/api/users/" + secondUserId, body: map[string]any{"name": "Hijacked", "email": "only@second.test", "role_id": secondRoleId}},
			{method: http.MethodDelete, path: "/api/users/" + secondUserId},
			{method: http.MethodGet, path: "/api/roles/" + secondRoleId},
			{method: http.MethodPut, path: "/api/roles/" + secondRoleId, body: map[string]any{"name": "Hijacked"}},
			{method: http.MethodDelete, path: "/api/roles/" + secondRoleId},
			{method: http.MethodGet, path: "/api/permissions/user/" + secondUserId},
			{method: http.MethodGet, path: "/api/permissions/role/" + secondRoleId},
			{method: http.MethodPost, path: "/api/permissions/assign-role", body: map[string]any{"role_id": secondRoleId, "permission_ids": []string{"users:view"}}},
			{method: http.MethodPost, path: "/api/permissions/assign-user", body: map[string]any{"user_id": secondUserId, "permission_ids": []string{"users:view"}}},
			{method: http.MethodPost, path: "/api/roles/assign", body: map[string]any{"user_id": secondUserId, "role_id": secondRoleId}},
			{method: http.MethodPost, path: "/api/users/update-password", body: map[string]any{"user_id": secondUserId, "new_password": "taken-over-password"}},
		}
		for _, crossTenantRequest := range crossTenantRequests {
			crossResponse := harness.Call(crossTenantRequest.method, crossTenantRequest.path, firstCompany.OwnerToken, crossTenantRequest.body)
			if crossResponse.Status != http.StatusNotFound {
				t.Fatalf("%s %s across tenants returned %d, want 404", crossTenantRequest.method, crossTenantRequest.path, crossResponse.Status)
			}
		}

		secondStillIntact := harness.Call(http.MethodGet, "/api/users/"+secondUserId, secondCompany.OwnerToken, nil)
		if secondStillIntact.Data()["name"] != "Second Only User" || secondStillIntact.Data()["is_active"] != true {
			t.Fatalf("the second company's user was changed: %v", secondStillIntact.Data())
		}
		harness.MustLogin("only@second.test", "second-password")

		isPostgres := engineCase.Engine == config.EnginePostgres
		if isPostgres {
			assertRowLevelSecurityWithoutFilters(t, harness, firstCompany.Id)
		}
	})
}

func assertRowLevelSecurityWithoutFilters(t *testing.T, harness *apptest.Harness, firstCompanyId uuid.UUID) {
	t.Helper()
	testContext := context.Background()

	unfilteredTables := []string{"companies", "shops", "roles", "users", "user_shops", "role_permissions", "sessions", "settings", "products", "barcodes", "price_history", "product_addons", "shop_stock", "stock_movements", "stock_transfers", "stock_transfer_items", "notifications"}
	for _, tableName := range unfilteredTables {
		noTenantCount := countWithTenant(t, harness, nil, "SELECT COUNT(*) FROM "+tableName)
		if noTenantCount != 0 {
			t.Fatalf("%s returned %d rows with no tenant set; row-level security must hide everything", tableName, noTenantCount)
		}
	}

	firstTenantUsers := countWithTenant(t, harness, &firstCompanyId, "SELECT COUNT(*) FROM users")
	firstTenantCompanies := countWithTenant(t, harness, &firstCompanyId, "SELECT COUNT(*) FROM companies")
	if firstTenantUsers != 1 || firstTenantCompanies != 1 {
		t.Fatalf("with the first tenant set, an unfiltered query saw %d users and %d companies, want 1 and 1", firstTenantUsers, firstTenantCompanies)
	}

	writeTransaction, beginError := harness.Database.Writer.BeginTx(testContext, nil)
	if beginError != nil {
		t.Fatalf("begin: %v", beginError)
	}
	defer writeTransaction.Rollback()
	database.SetTenant(testContext, writeTransaction, true, firstCompanyId)
	_, crossInsertError := writeTransaction.ExecContext(testContext,
		`INSERT INTO shops (id, company_id, name) VALUES ($1, $2, $3)`, uuid.Must(uuid.NewV7()), uuid.Must(uuid.NewV7()), "Smuggled")
	if crossInsertError == nil {
		t.Fatal("row-level security accepted a row for a different company")
	}
}

func countWithTenant(t *testing.T, harness *apptest.Harness, companyId *uuid.UUID, query string) int64 {
	t.Helper()
	testContext := context.Background()

	readTransaction, beginError := harness.Database.Reader.BeginTx(testContext, nil)
	if beginError != nil {
		t.Fatalf("begin: %v", beginError)
	}
	defer readTransaction.Rollback()

	hasTenant := companyId != nil
	if hasTenant {
		setTenantError := database.SetTenant(testContext, readTransaction, true, *companyId)
		if setTenantError != nil {
			t.Fatalf("set tenant: %v", setTenantError)
		}
	}

	rowCount := int64(0)
	scanError := readTransaction.QueryRowContext(testContext, query).Scan(&rowCount)
	if scanError != nil {
		t.Fatalf("%s: %v", query, scanError)
	}
	return rowCount
}

func stringify(body map[string]any) string {
	builder := strings.Builder{}
	var walk func(value any)
	walk = func(value any) {
		switch typedValue := value.(type) {
		case map[string]any:
			for key, nestedValue := range typedValue {
				builder.WriteString(key + "=")
				walk(nestedValue)
			}
		case []any:
			for _, nestedValue := range typedValue {
				walk(nestedValue)
			}
		case string:
			builder.WriteString(typedValue + ";")
		}
	}
	walk(body)
	return builder.String()
}
