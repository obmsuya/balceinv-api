package database_test

import (
	"context"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/common/database"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/google/uuid"
)

func insertCompanyWithRole(t *testing.T, openDatabase *database.Database, roleName string, isOwner bool) (uuid.UUID, uuid.UUID) {
	t.Helper()
	testContext := context.Background()

	companyId := uuid.Must(uuid.NewV7())
	_, insertCompanyError := openDatabase.Writer.ExecContext(testContext,
		`INSERT INTO companies (id, name) VALUES ($1, $2)`, companyId, "Company "+companyId.String()[:8])
	if insertCompanyError != nil {
		t.Fatalf("insert company: %v", insertCompanyError)
	}

	roleId := uuid.Must(uuid.NewV7())
	_, insertRoleError := openDatabase.Writer.ExecContext(testContext,
		`INSERT INTO roles (id, company_id, name, is_owner) VALUES ($1, $2, $3, $4)`, roleId, companyId, roleName, isOwner)
	if insertRoleError != nil {
		t.Fatalf("insert role: %v", insertRoleError)
	}

	return companyId, roleId
}

func TestSchemaEnforcesTenantBoundaries(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		openDatabase := testkit.OpenMigrated(t, engineCase)
		testContext := context.Background()

		permissionCount := 0
		countError := openDatabase.Reader.QueryRowContext(testContext, `SELECT COUNT(*) FROM permissions`).Scan(&permissionCount)
		if countError != nil {
			t.Fatalf("count permissions: %v", countError)
		}
		if permissionCount != 40 {
			t.Fatalf("seeded %d permissions, want 40", permissionCount)
		}

		sampleDescription := ""
		describeError := openDatabase.Reader.QueryRowContext(testContext,
			`SELECT description FROM permissions WHERE id = $1`, "stock_movements:view").Scan(&sampleDescription)
		if describeError != nil {
			t.Fatalf("read permission: %v", describeError)
		}
		if sampleDescription != "View stock movements" {
			t.Fatalf("description %q, want %q", sampleDescription, "View stock movements")
		}

		firstCompanyId, firstRoleId := insertCompanyWithRole(t, openDatabase, "Manager", true)
		secondCompanyId, _ := insertCompanyWithRole(t, openDatabase, "Manager", true)

		_, duplicateRoleError := openDatabase.Writer.ExecContext(testContext,
			`INSERT INTO roles (id, company_id, name) VALUES ($1, $2, $3)`, uuid.Must(uuid.NewV7()), firstCompanyId, "Manager")
		if duplicateRoleError == nil {
			t.Fatal("a second Manager role in the same company was accepted")
		}

		_, secondOwnerRoleError := openDatabase.Writer.ExecContext(testContext,
			`INSERT INTO roles (id, company_id, name, is_owner) VALUES ($1, $2, $3, $4)`, uuid.Must(uuid.NewV7()), firstCompanyId, "Co-owner", true)
		if secondOwnerRoleError == nil {
			t.Fatal("a second owner role in the same company was accepted")
		}

		_, crossCompanyUserError := openDatabase.Writer.ExecContext(testContext,
			`INSERT INTO users (id, company_id, role_id, name, email, password_hash) VALUES ($1, $2, $3, $4, $5, $6)`,
			uuid.Must(uuid.NewV7()), secondCompanyId, firstRoleId, "Intruder", "intruder@example.com", "x")
		if crossCompanyUserError == nil {
			t.Fatal("a user in company B was allowed to take a role from company A")
		}

		_, mixedCaseEmailError := openDatabase.Writer.ExecContext(testContext,
			`INSERT INTO users (id, company_id, role_id, name, email, password_hash) VALUES ($1, $2, $3, $4, $5, $6)`,
			uuid.Must(uuid.NewV7()), firstCompanyId, firstRoleId, "Mixed", "Mixed@Example.com", "x")
		if mixedCaseEmailError == nil {
			t.Fatal("an email that is not lower case was accepted")
		}
	})
}
