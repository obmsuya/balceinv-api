package customers_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/google/uuid"
)

func turnOn(t *testing.T, harness *apptest.Harness, ownerToken string, switches map[string]any) {
	t.Helper()
	saved := harness.Call(http.MethodPut, "/api/features", ownerToken, switches)
	if saved.Status != http.StatusOK {
		t.Fatalf("turning on %v returned %d %v", switches, saved.Status, saved.Body)
	}
}

func newProduct(t *testing.T, harness *apptest.Harness, ownerToken string, sku string, price int64, openingQuantity int) string {
	t.Helper()
	created := harness.Call(http.MethodPost, "/api/products", ownerToken, map[string]any{"sku": sku, "name": sku, "price": price, "opening_quantity": openingQuantity})
	if created.Status != http.StatusCreated {
		t.Fatalf("create %s returned %d %v", sku, created.Status, created.Body)
	}
	return created.Data()["id"].(string)
}

func sellOnCredit(t *testing.T, harness *apptest.Harness, token string, clientRef string, customerId string, productId string, quantity int, payments []map[string]any) apptest.Response {
	t.Helper()
	return harness.Call(http.MethodPost, "/api/sales", token, map[string]any{
		"client_ref":  clientRef,
		"customer_id": customerId,
		"items":       []any{map[string]any{"product_id": productId, "quantity": quantity}},
		"payments":    payments,
	})
}

func TestCustomersStayHiddenUntilTurnedOn(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Quiet Shop", "owner@quiet.test")
		someId := uuid.Must(uuid.NewV7()).String()

		entryPoints := []struct{ method, path string }{
			{http.MethodGet, "/api/customers"},
			{http.MethodPost, "/api/customers"},
			{http.MethodGet, "/api/customers/debtors"},
			{http.MethodGet, "/api/customers/" + someId},
			{http.MethodPut, "/api/customers/" + someId},
			{http.MethodDelete, "/api/customers/" + someId},
			{http.MethodPost, "/api/customers/" + someId + "/restore"},
			{http.MethodGet, "/api/customers/" + someId + "/sales"},
			{http.MethodGet, "/api/customers/" + someId + "/statement"},
			{http.MethodGet, "/api/customers/" + someId + "/payments"},
			{http.MethodPost, "/api/customers/" + someId + "/payments"},
			{http.MethodPost, "/api/customers/" + someId + "/payments/" + someId + "/void"},
		}
		for _, entryPoint := range entryPoints {
			refused := harness.Call(entryPoint.method, entryPoint.path, company.OwnerToken, map[string]any{"name": "Juma", "amount": 100, "method": "cash", "reason": "x"})
			if refused.Status != http.StatusForbidden || refused.Code() != "feature_off" {
				t.Fatalf("%s %s with customers off returned %d %s", entryPoint.method, entryPoint.path, refused.Status, refused.Code())
			}
		}
	})
}

func TestCustomersKeepCleanPhonesPermissionsAndCompanyWalls(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Duka la Juma", "owner@juma.test")
		otherCompany := harness.CreateCompany("Duka Jingine", "owner@jingine.test")
		turnOn(t, harness, company.OwnerToken, map[string]any{"customers_enabled": true})
		turnOn(t, harness, otherCompany.OwnerToken, map[string]any{"customers_enabled": true})
		cashierToken := harness.CreateStaff(company, "cashier@juma.test", []string{"sales:create"}, []uuid.UUID{company.ShopId})

		created := harness.Call(http.MethodPost, "/api/customers", company.OwnerToken, map[string]any{"name": "  Asha Mussa ", "phone": "+255 712 345 678", "credit_limit": 50000})
		createdData := created.Data()
		if created.Status != http.StatusCreated || createdData["phone"] != "0712345678" || createdData["name"] != "Asha Mussa" || createdData["balance"] != float64(0) ||
			createdData["available_credit"] != float64(50000) || createdData["is_active"] != true {
			t.Fatalf("create returned %d %v", created.Status, created.Body)
		}
		ashaId := createdData["id"].(string)

		duplicate := harness.Call(http.MethodPost, "/api/customers", company.OwnerToken, map[string]any{"name": "Asha M", "phone": "0712 345 678"})
		if duplicate.Status != http.StatusConflict || duplicate.Code() != "phone_taken" {
			t.Fatalf("a duplicate phone returned %d %s", duplicate.Status, duplicate.Code())
		}
		badPhone := harness.Call(http.MethodPost, "/api/customers", company.OwnerToken, map[string]any{"name": "Bad", "phone": "12345"})
		if badPhone.Status != http.StatusBadRequest || badPhone.Code() != "invalid_phone" {
			t.Fatalf("a bad phone returned %d %s", badPhone.Status, badPhone.Code())
		}
		samePhoneElsewhere := harness.Call(http.MethodPost, "/api/customers", otherCompany.OwnerToken, map[string]any{"name": "Asha", "phone": "0712345678"})
		if samePhoneElsewhere.Status != http.StatusCreated {
			t.Fatalf("another company could not use the same phone: %d %v", samePhoneElsewhere.Status, samePhoneElsewhere.Body)
		}
		noPhone := harness.Call(http.MethodPost, "/api/customers", company.OwnerToken, map[string]any{"name": "Walk-in regular"})
		if noPhone.Status != http.StatusCreated || noPhone.Data()["phone"] != nil {
			t.Fatalf("a customer without a phone returned %d %v", noPhone.Status, noPhone.Body)
		}

		byPhone := harness.Call(http.MethodGet, "/api/customers?q=255712", cashierToken, nil)
		if byPhone.Status != http.StatusOK || len(byPhone.Items()) != 1 || byPhone.Items()[0].(map[string]any)["id"] != ashaId {
			t.Fatalf("the till search by phone returned %d %v", byPhone.Status, byPhone.Body)
		}
		if harness.Call(http.MethodGet, "/api/customers/"+ashaId, cashierToken, nil).Status != http.StatusOK {
			t.Fatal("the cashier could not open the customer picked at the till")
		}
		cashierRefusals := []struct{ method, path string }{
			{http.MethodPost, "/api/customers"},
			{http.MethodPut, "/api/customers/" + ashaId},
			{http.MethodDelete, "/api/customers/" + ashaId},
			{http.MethodGet, "/api/customers/debtors"},
			{http.MethodPost, "/api/customers/" + ashaId + "/payments"},
			{http.MethodGet, "/api/customers/" + ashaId + "/statement"},
		}
		for _, refusal := range cashierRefusals {
			refused := harness.Call(refusal.method, refusal.path, cashierToken, map[string]any{"name": "X", "amount": 100, "method": "cash"})
			if refused.Status != http.StatusForbidden || refused.Code() != "forbidden" {
				t.Fatalf("the cashier %s %s returned %d %s", refusal.method, refusal.path, refused.Status, refused.Code())
			}
		}

		if harness.Call(http.MethodGet, "/api/customers/"+ashaId, otherCompany.OwnerToken, nil).Status != http.StatusNotFound {
			t.Fatal("another company could open this customer")
		}
		if harness.Call(http.MethodPut, "/api/customers/"+ashaId, otherCompany.OwnerToken, map[string]any{"name": "Stolen"}).Status != http.StatusNotFound {
			t.Fatal("another company could edit this customer")
		}
		if len(harness.Call(http.MethodGet, "/api/customers", otherCompany.OwnerToken, nil).Items()) != 1 {
			t.Fatal("another company sees this company's customers")
		}

		deactivated := harness.Call(http.MethodDelete, "/api/customers/"+ashaId, company.OwnerToken, nil)
		if deactivated.Status != http.StatusOK || deactivated.Data()["is_active"] != false {
			t.Fatalf("deactivating returned %d %v", deactivated.Status, deactivated.Body)
		}
		if len(harness.Call(http.MethodGet, "/api/customers?q=asha", company.OwnerToken, nil).Items()) != 0 ||
			len(harness.Call(http.MethodGet, "/api/customers?q=asha&include_inactive=true", company.OwnerToken, nil).Items()) != 1 {
			t.Fatal("a deactivated customer is not hidden from the default list but kept in the full one")
		}
		newAsha := harness.Call(http.MethodPost, "/api/customers", company.OwnerToken, map[string]any{"name": "New Asha", "phone": "0712345678"})
		if newAsha.Status != http.StatusCreated {
			t.Fatalf("the phone of a deactivated customer could not be reused: %d %v", newAsha.Status, newAsha.Body)
		}
		restored := harness.Call(http.MethodPost, "/api/customers/"+ashaId+"/restore", company.OwnerToken, nil)
		if restored.Status != http.StatusConflict || restored.Code() != "phone_taken" {
			t.Fatalf("restoring onto a taken phone returned %d %s", restored.Status, restored.Code())
		}
	})
}

func TestBalancesPaymentsStatementsAndWhoOwes(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Madeni Shop", "owner@madeni.test")
		turnOn(t, harness, company.OwnerToken, map[string]any{"customers_enabled": true, "credit_sales_enabled": true})
		clerkToken := harness.CreateStaff(company, "clerk@madeni.test", []string{"customers:view", "customers:edit", "sales:create"}, []uuid.UUID{company.ShopId})
		riceId := newProduct(t, harness, company.OwnerToken, "RICE", 10000, 100)

		juma := harness.Call(http.MethodPost, "/api/customers", company.OwnerToken, map[string]any{"name": "Juma", "phone": "0754000001", "opening_balance": 5000}).Data()
		jumaId := juma["id"].(string)
		if juma["balance"] != float64(5000) {
			t.Fatalf("the opening balance is not owed: %v", juma)
		}
		neema := harness.Call(http.MethodPost, "/api/customers", company.OwnerToken, map[string]any{"name": "Neema", "phone": "0754000002"}).Data()
		neemaId := neema["id"].(string)

		firstSale := sellOnCredit(t, harness, clerkToken, "credit-sale-0001", jumaId, riceId, 1, []map[string]any{{"method": "cash", "amount": 4000}, {"method": "credit", "amount": 6000}})
		if firstSale.Status != http.StatusCreated {
			t.Fatalf("a credit sale returned %d %v", firstSale.Status, firstSale.Body)
		}
		secondSale := sellOnCredit(t, harness, clerkToken, "credit-sale-0002", jumaId, riceId, 2, []map[string]any{{"method": "credit", "amount": 20000}})
		neemaSale := sellOnCredit(t, harness, clerkToken, "credit-sale-0003", neemaId, riceId, 1, []map[string]any{{"method": "credit", "amount": 10000}})
		if secondSale.Status != http.StatusCreated || neemaSale.Status != http.StatusCreated {
			t.Fatalf("credit sales returned %d %d", secondSale.Status, neemaSale.Status)
		}

		now := time.Now().UTC()
		harness.ExecForCompany(company.Id, `UPDATE customers SET created_at = $1 WHERE id = $2`, now.AddDate(0, 0, -120), jumaId)
		harness.ExecForCompany(company.Id, `UPDATE sales SET created_at = $1 WHERE id = $2`, now.AddDate(0, 0, -45), firstSale.Data()["id"])
		harness.ExecForCompany(company.Id, `UPDATE sales SET created_at = $1 WHERE id = $2`, now.AddDate(0, 0, -5), secondSale.Data()["id"])
		harness.ExecForCompany(company.Id, `UPDATE sales SET created_at = $1 WHERE id = $2`, now.AddDate(0, 0, -70), neemaSale.Data()["id"])

		jumaNow := harness.Call(http.MethodGet, "/api/customers/"+jumaId, clerkToken, nil).Data()
		jumaAging := jumaNow["aging"].(map[string]any)
		if jumaNow["balance"] != float64(31000) || jumaAging["days_over_90"] != float64(5000) || jumaAging["days_31_60"] != float64(6000) ||
			jumaAging["days_0_30"] != float64(20000) || jumaNow["overdue_amount"] != float64(11000) {
			t.Fatalf("Juma's balance after two credit sales %v", jumaNow)
		}

		overpaid := harness.Call(http.MethodPost, "/api/customers/"+jumaId+"/payments", clerkToken, map[string]any{"amount": 31001, "method": "cash"})
		if overpaid.Status != http.StatusBadRequest || overpaid.Code() != "overpayment" {
			t.Fatalf("an over-payment returned %d %s", overpaid.Status, overpaid.Code())
		}
		paid := harness.Call(http.MethodPost, "/api/customers/"+jumaId+"/payments", clerkToken, map[string]any{"amount": 8000, "method": "mobile", "reference": " MPESA-123 "})
		if paid.Status != http.StatusCreated || paid.Data()["reference"] != "MPESA-123" || paid.Data()["created_by_name"] == "" {
			t.Fatalf("a payment returned %d %v", paid.Status, paid.Body)
		}
		paymentId := paid.Data()["id"].(string)
		secondPayment := harness.Call(http.MethodPost, "/api/customers/"+jumaId+"/payments", clerkToken, map[string]any{"amount": 1000, "method": "cash"})

		afterPayments := harness.Call(http.MethodGet, "/api/customers/"+jumaId, clerkToken, nil).Data()
		afterAging := afterPayments["aging"].(map[string]any)
		if afterPayments["balance"] != float64(22000) || afterAging["days_over_90"] != float64(0) || afterAging["days_31_60"] != float64(2000) || afterAging["days_0_30"] != float64(20000) {
			t.Fatalf("payments were not taken from the oldest debt first: %v", afterPayments)
		}

		voided := harness.Call(http.MethodPost, "/api/customers/"+jumaId+"/payments/"+paymentId+"/void", clerkToken, map[string]any{"reason": "Entered twice"})
		if voided.Status != http.StatusOK || voided.Data()["void_reason"] != "Entered twice" || voided.Data()["voided_at"] == nil {
			t.Fatalf("voiding returned %d %v", voided.Status, voided.Body)
		}
		voidedAgain := harness.Call(http.MethodPost, "/api/customers/"+jumaId+"/payments/"+paymentId+"/void", clerkToken, map[string]any{"reason": "Again"})
		if voidedAgain.Status != http.StatusConflict || voidedAgain.Code() != "already_voided" {
			t.Fatalf("voiding twice returned %d %s", voidedAgain.Status, voidedAgain.Code())
		}
		if harness.Call(http.MethodGet, "/api/customers/"+jumaId, clerkToken, nil).Data()["balance"] != float64(30000) {
			t.Fatal("a voided payment still lowers the balance")
		}
		paymentList := harness.Call(http.MethodGet, "/api/customers/"+jumaId+"/payments", clerkToken, nil).Body["data"].([]any)
		if len(paymentList) != 2 {
			t.Fatalf("the payment history lost the voided payment: %v", paymentList)
		}
		if secondPayment.Status != http.StatusCreated {
			t.Fatalf("the second payment returned %d", secondPayment.Status)
		}

		tooLow := harness.Call(http.MethodPut, "/api/customers/"+jumaId, company.OwnerToken, map[string]any{"name": "Juma", "phone": "0754000001", "opening_balance": 0})
		if tooLow.Status != http.StatusOK {
			t.Fatalf("lowering the opening balance while debt remains returned %d %v", tooLow.Status, tooLow.Body)
		}
		harness.Call(http.MethodPost, "/api/customers/"+jumaId+"/payments", clerkToken, map[string]any{"amount": 25000, "method": "cash"})
		refusedLowering := harness.Call(http.MethodPut, "/api/customers/"+jumaId, company.OwnerToken, map[string]any{"name": "Juma", "phone": "0754000001", "opening_balance": 0, "credit_limit": nil})
		if refusedLowering.Status != http.StatusOK {
			t.Fatalf("saving without changing the opening balance returned %d %v", refusedLowering.Status, refusedLowering.Body)
		}
		if harness.Call(http.MethodGet, "/api/customers/"+jumaId, clerkToken, nil).Data()["balance"] != float64(0) {
			t.Fatal("Juma should owe nothing now")
		}
		raised := harness.Call(http.MethodPut, "/api/customers/"+jumaId, company.OwnerToken, map[string]any{"name": "Juma", "opening_balance": 3000})
		loweredBelowPaid := harness.Call(http.MethodPut, "/api/customers/"+jumaId, company.OwnerToken, map[string]any{"name": "Juma", "opening_balance": 0})
		if raised.Status != http.StatusOK || loweredBelowPaid.Status != http.StatusOK {
			t.Fatalf("raising and lowering an unpaid opening balance returned %d %d", raised.Status, loweredBelowPaid.Status)
		}
		harness.Call(http.MethodPut, "/api/customers/"+jumaId, company.OwnerToken, map[string]any{"name": "Juma", "opening_balance": 3000})
		harness.Call(http.MethodPost, "/api/customers/"+jumaId+"/payments", clerkToken, map[string]any{"amount": 3000, "method": "cash"})
		belowPaid := harness.Call(http.MethodPut, "/api/customers/"+jumaId, company.OwnerToken, map[string]any{"name": "Juma", "opening_balance": 1000})
		if belowPaid.Status != http.StatusBadRequest || belowPaid.Code() != "opening_balance_too_low" {
			t.Fatalf("lowering the opening balance below what was paid returned %d %s", belowPaid.Status, belowPaid.Code())
		}

		today := now.In(time.FixedZone("EAT", 3*60*60)).Format("2006-01-02")
		fromDay := now.AddDate(0, 0, -50).In(time.FixedZone("EAT", 3*60*60)).Format("2006-01-02")
		statement := harness.Call(http.MethodGet, "/api/customers/"+jumaId+"/statement?from="+fromDay+"&to="+today, clerkToken, nil)
		statementData := statement.Data()
		statementEntries := statementData["entries"].([]any)
		if statement.Status != http.StatusOK || statementData["opening_balance"] != float64(3000) || statementData["closing_balance"] != float64(0) ||
			statementData["total_debits"] != float64(26000) || statementData["total_credits"] != float64(29000) || len(statementEntries) != 5 {
			t.Fatalf("statement returned %d %v", statement.Status, statementData)
		}
		firstEntry := statementEntries[0].(map[string]any)
		if firstEntry["kind"] != "credit_sale" || firstEntry["debit"] != float64(6000) || firstEntry["balance"] != float64(9000) {
			t.Fatalf("the first statement line %v", firstEntry)
		}
		backwards := harness.Call(http.MethodGet, "/api/customers/"+jumaId+"/statement?from="+today+"&to="+fromDay, clerkToken, nil)
		if backwards.Status != http.StatusBadRequest || backwards.Code() != "invalid_range" {
			t.Fatalf("a backwards range returned %d %s", backwards.Status, backwards.Code())
		}

		purchases := harness.Call(http.MethodGet, "/api/customers/"+jumaId+"/sales", clerkToken, nil)
		if purchases.Status != http.StatusOK || len(purchases.Items()) != 2 || purchases.Items()[0].(map[string]any)["credit_amount"] != float64(20000) {
			t.Fatalf("purchase history returned %d %v", purchases.Status, purchases.Body)
		}

		debtors := harness.Call(http.MethodGet, "/api/customers/debtors", company.OwnerToken, nil)
		debtorList := debtors.Data()["customers"].([]any)
		if debtors.Status != http.StatusOK || len(debtorList) != 1 || debtorList[0].(map[string]any)["id"] != neemaId ||
			debtors.Data()["totals"].(map[string]any)["balance"] != float64(10000) {
			t.Fatalf("who owes returned %d %v", debtors.Status, debtors.Body)
		}
		harness.Call(http.MethodPut, "/api/customers/"+jumaId, company.OwnerToken, map[string]any{"name": "Juma", "opening_balance": 5000})
		sortedDebtors := harness.Call(http.MethodGet, "/api/customers/debtors", company.OwnerToken, nil).Data()["customers"].([]any)
		if len(sortedDebtors) != 2 || sortedDebtors[0].(map[string]any)["id"] != neemaId || sortedDebtors[1].(map[string]any)["id"] != jumaId {
			t.Fatalf("who owes is not sorted by the oldest debt: %v", sortedDebtors)
		}
		earlierDebtors := harness.Call(http.MethodGet, "/api/customers/debtors?as_of="+now.AddDate(0, 0, -40).Format("2006-01-02"), company.OwnerToken, nil).Data()
		neemaEarlier := earlierDebtors["customers"].([]any)[0].(map[string]any)["aging"].(map[string]any)
		if neemaEarlier["days_0_30"] != float64(10000) {
			t.Fatalf("aging as of an earlier day %v", earlierDebtors)
		}
		if harness.Call(http.MethodGet, "/api/customers/debtors?as_of=yesterday", company.OwnerToken, nil).Code() != "invalid_range" {
			t.Fatal("a bad as_of date was accepted")
		}
	})
}
