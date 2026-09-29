package sales_test

import (
	"net/http"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/google/uuid"
)

func sellTo(harness *apptest.Harness, sessionToken string, clientRef string, customerId any, items []map[string]any, payments []map[string]any) apptest.Response {
	return harness.Call(http.MethodPost, "/api/sales", sessionToken, map[string]any{"client_ref": clientRef, "customer_id": customerId, "items": items, "payments": payments})
}

func credit(amount int64) map[string]any {
	return map[string]any{"method": "credit", "amount": amount}
}

func TestCreditSalesFollowTheSwitchesTheCustomerAndTheLimit(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Credit Shop", "owner@credit.test")
		otherCompany := harness.CreateCompany("Other Credit", "owner@othercredit.test")
		cashierToken := harness.CreateStaff(company, "cashier@credit.test", []string{"sales:create"}, []uuid.UUID{company.ShopId})
		sugarId := newProduct(t, harness, company.OwnerToken, map[string]any{"sku": "SUGAR", "name": "Sugar", "price": 3000, "opening_quantity": 100})

		switchedOff := sellTo(harness, cashierToken, "credit-off-0001", nil, []map[string]any{line(sugarId, 1)}, []map[string]any{credit(3000)})
		if switchedOff.Status != http.StatusForbidden || switchedOff.Code() != "feature_off" {
			t.Fatalf("credit with customers off returned %d %s", switchedOff.Status, switchedOff.Code())
		}
		someCustomerId := uuid.Must(uuid.NewV7()).String()
		customerWhileOff := sellTo(harness, cashierToken, "credit-off-0002", someCustomerId, []map[string]any{line(sugarId, 1)}, cash(3000))
		if customerWhileOff.Status != http.StatusForbidden || customerWhileOff.Code() != "feature_off" {
			t.Fatalf("a customer with customers off returned %d %s", customerWhileOff.Status, customerWhileOff.Code())
		}

		harness.Call(http.MethodPut, "/api/features", company.OwnerToken, map[string]any{"customers_enabled": true})
		harness.Call(http.MethodPut, "/api/features", otherCompany.OwnerToken, map[string]any{"customers_enabled": true, "credit_sales_enabled": true})
		mariamId := harness.Call(http.MethodPost, "/api/customers", company.OwnerToken, map[string]any{"name": "Mariam", "phone": "0765000001", "credit_limit": 10000, "opening_balance": 1000}).Data()["id"].(string)
		foreignCustomerId := harness.Call(http.MethodPost, "/api/customers", otherCompany.OwnerToken, map[string]any{"name": "Foreign"}).Data()["id"].(string)

		creditStillOff := sellTo(harness, cashierToken, "credit-off-0003", mariamId, []map[string]any{line(sugarId, 1)}, []map[string]any{credit(3000)})
		if creditStillOff.Status != http.StatusForbidden || creditStillOff.Code() != "feature_off" {
			t.Fatalf("credit with credit sales off returned %d %s", creditStillOff.Status, creditStillOff.Code())
		}
		cashWithCustomer := sellTo(harness, cashierToken, "customer-cash-0001", mariamId, []map[string]any{line(sugarId, 1)}, cash(5000))
		cashData := cashWithCustomer.Data()
		if cashWithCustomer.Status != http.StatusCreated || cashData["customer_name"] != "Mariam" || cashData["credit_amount"] != float64(0) || cashData["change_given"] != float64(2000) {
			t.Fatalf("a cash sale to a customer returned %d %v", cashWithCustomer.Status, cashWithCustomer.Body)
		}

		harness.Call(http.MethodPut, "/api/features", company.OwnerToken, map[string]any{"credit_sales_enabled": true})
		salesBefore := harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM sales`)
		refusals := []struct {
			name       string
			customerId any
			payments   []map[string]any
			wantStatus int
			wantCode   string
		}{
			{"credit without a customer", nil, []map[string]any{credit(3000)}, http.StatusBadRequest, "customer_required"},
			{"another company's customer", foreignCustomerId, []map[string]any{credit(3000)}, http.StatusNotFound, "not_found"},
			{"an unknown customer", someCustomerId, []map[string]any{credit(3000)}, http.StatusNotFound, "not_found"},
			{"credit above the total", mariamId, []map[string]any{credit(3001)}, http.StatusBadRequest, "invalid_payment"},
			{"card and credit above the total", mariamId, []map[string]any{{"method": "card", "amount": 1000}, credit(2500)}, http.StatusBadRequest, "invalid_payment"},
			{"credit that falls short", mariamId, []map[string]any{credit(2000)}, http.StatusBadRequest, "invalid_payment"},
			{"credit twice", mariamId, []map[string]any{credit(1000), credit(2000)}, http.StatusBadRequest, "invalid_payment"},
			{"a malformed customer id", "not-a-uuid", []map[string]any{credit(3000)}, http.StatusBadRequest, "validation_failed"},
		}
		for refusalIndex, refusal := range refusals {
			refused := sellTo(harness, cashierToken, "credit-refused-"+string(rune('a'+refusalIndex))+"xxxx", refusal.customerId, []map[string]any{line(sugarId, 1)}, refusal.payments)
			if refused.Status != refusal.wantStatus || refused.Code() != refusal.wantCode {
				t.Fatalf("%s returned %d %s, want %d %s", refusal.name, refused.Status, refused.Code(), refusal.wantStatus, refusal.wantCode)
			}
		}
		if harness.QueryIntForCompany(company.Id, `SELECT COUNT(*) FROM sales`) != salesBefore {
			t.Fatal("a refused credit sale still wrote a sale")
		}

		mixed := sellTo(harness, cashierToken, "credit-mixed-0001", mariamId, []map[string]any{line(sugarId, 2)}, []map[string]any{{"method": "cash", "amount": 2000}, credit(4000)})
		mixedData := mixed.Data()
		if mixed.Status != http.StatusCreated || mixedData["credit_amount"] != float64(4000) || mixedData["amount_paid"] != float64(6000) || mixedData["change_given"] != float64(0) ||
			len(mixedData["payments"].([]any)) != 2 {
			t.Fatalf("a cash and credit sale returned %d %v", mixed.Status, mixed.Body)
		}
		retried := sellTo(harness, cashierToken, "credit-mixed-0001", mariamId, []map[string]any{line(sugarId, 2)}, []map[string]any{{"method": "cash", "amount": 2000}, credit(4000)})
		if retried.Status != http.StatusCreated || retried.Data()["id"] != mixedData["id"] {
			t.Fatalf("retrying a credit sale returned %d %v", retried.Status, retried.Body)
		}
		otherCustomerRetry := sellTo(harness, cashierToken, "credit-mixed-0001", nil, []map[string]any{line(sugarId, 2)}, []map[string]any{{"method": "cash", "amount": 2000}, credit(4000)})
		if otherCustomerRetry.Status != http.StatusConflict || otherCustomerRetry.Code() != "client_ref_reused" {
			t.Fatalf("reusing a credit sale reference without its customer returned %d %s", otherCustomerRetry.Status, otherCustomerRetry.Code())
		}
		if balance := harness.Call(http.MethodGet, "/api/customers/"+mariamId, company.OwnerToken, nil).Data()["balance"]; balance != float64(5000) {
			t.Fatalf("after one credit sale and a retry the balance is %v, want 5000", balance)
		}

		exactlyAtLimit := sellTo(harness, cashierToken, "credit-limit-0001", mariamId, []map[string]any{line(sugarId, 2)}, []map[string]any{{"method": "mobile", "amount": 1000}, credit(5000)})
		if exactlyAtLimit.Status != http.StatusCreated {
			t.Fatalf("credit exactly up to the limit returned %d %v", exactlyAtLimit.Status, exactlyAtLimit.Body)
		}
		overLimit := sellTo(harness, cashierToken, "credit-limit-0002", mariamId, []map[string]any{line(sugarId, 1)}, []map[string]any{{"method": "cash", "amount": 2999}, credit(1)})
		if overLimit.Status != http.StatusBadRequest || overLimit.Code() != "credit_limit_exceeded" {
			t.Fatalf("one shilling over the limit returned %d %s", overLimit.Status, overLimit.Code())
		}

		receipt := harness.Call(http.MethodGet, "/api/sales/"+mixedData["id"].(string)+"/receipt", cashierToken, nil).Data()["sale"].(map[string]any)
		if receipt["customer_name"] != "Mariam" || receipt["customer_phone"] != "0765000001" || receipt["credit_amount"] != float64(4000) {
			t.Fatalf("the receipt does not show the customer and what is owed: %v", receipt)
		}

		harness.Call(http.MethodDelete, "/api/customers/"+mariamId, company.OwnerToken, nil)
		deactivatedCredit := sellTo(harness, cashierToken, "credit-inactive-01", mariamId, []map[string]any{line(sugarId, 1)}, []map[string]any{credit(3000)})
		if deactivatedCredit.Status != http.StatusBadRequest || deactivatedCredit.Code() != "customer_required" {
			t.Fatalf("credit to a deactivated customer returned %d %s", deactivatedCredit.Status, deactivatedCredit.Code())
		}
		history := harness.Call(http.MethodGet, "/api/customers/"+mariamId+"/sales", company.OwnerToken, nil)
		if len(history.Items()) != 3 {
			t.Fatalf("a deactivated customer lost their history: %v", history.Body)
		}

		harness.Call(http.MethodPut, "/api/features", company.OwnerToken, map[string]any{"credit_sales_enabled": false})
		if harness.Call(http.MethodGet, "/api/sales/"+mixedData["id"].(string), cashierToken, nil).Data()["credit_amount"] != float64(4000) {
			t.Fatal("turning credit off hid the credit on an old sale")
		}
		summary := harness.Call(http.MethodGet, "/api/reports/summary", company.OwnerToken, nil)
		if summary.Data()["payments"].(map[string]any)["credit"] != float64(9000) {
			t.Fatalf("the sales summary does not show what was sold on credit: %v", summary.Data()["payments"])
		}
	})
}
