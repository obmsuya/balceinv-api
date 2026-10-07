package accounting_test

import (
	"net/http"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/google/uuid"
)

func TestSalaryNamesWhoWasPaidAndEntriesNameTheirCustomerOrSupplier(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Payroll Shop", "owner@payroll.test")
		otherCompany := harness.CreateCompany("Other Payroll", "owner@otherpayroll.test")
		turnOn(t, harness, company.OwnerToken, map[string]any{"accounting_mode": "simple", "customers_enabled": true})
		startBooks(t, harness, company.OwnerToken, map[string]any{"cash_in_drawer": 500000})
		harness.CreateStaff(company, "juma@payroll.test", []string{"sales:create"}, []uuid.UUID{company.ShopId})
		jumaId := ""
		for _, rawUser := range harness.Call(http.MethodGet, "/api/users", company.OwnerToken, nil).Items() {
			listedUser := rawUser.(map[string]any)
			if listedUser["email"] == "juma@payroll.test" {
				jumaId = listedUser["id"].(string)
			}
		}
		outsiderId := harness.Call(http.MethodGet, "/api/auth/me", otherCompany.OwnerToken, nil).Data()["id"].(string)
		salariesId := findAccountId(t, harness, company.OwnerToken, "salaries")
		people := harness.Call(http.MethodGet, "/api/accounting/people", company.OwnerToken, nil)
		if people.Status != http.StatusOK || len(people.Body["data"].([]any)) != 2 {
			t.Fatalf("the people who can be paid returned %d %v", people.Status, people.Body)
		}

		salaryBody := func(clientRef string, paidTo string) map[string]any {
			return map[string]any{
				"client_ref": clientRef, "kind": "expense", "amount": 150000, "money_account": "cash",
				"expense_account_id": salariesId, "note": "September salary", "paid_to_user_id": paidTo,
			}
		}
		salary := harness.Call(http.MethodPost, "/api/accounting/money", company.OwnerToken, salaryBody("salary-juma-0001", jumaId))
		if salary.Status != http.StatusCreated || salary.Data()["paid_to_user_id"] != jumaId || salary.Data()["paid_to_name"] == nil {
			t.Fatalf("paying Juma returned %d %v", salary.Status, salary.Body)
		}
		outsider := harness.Call(http.MethodPost, "/api/accounting/money", company.OwnerToken, salaryBody("salary-outsider-0001", outsiderId))
		if outsider.Status != http.StatusNotFound {
			t.Fatalf("paying someone from another business returned %d %v", outsider.Status, outsider.Body)
		}

		harness.Call(http.MethodPost, "/api/customers", company.OwnerToken, map[string]any{"name": "Mariam Shop", "opening_balance": 20000})
		entries := harness.Call(http.MethodGet, "/api/accounting/entries", company.OwnerToken, nil).Items()
		foundParty := false
		for _, rawEntry := range entries {
			entry := rawEntry.(map[string]any)
			if entry["party_type"] == "customer" && entry["party_name"] == "Mariam Shop" {
				foundParty = true
			}
		}
		if !foundParty {
			t.Fatalf("no entry named its customer: %v", entries)
		}
	})
}
