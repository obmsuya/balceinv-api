package discounts_test

import (
	"net/http"
	"testing"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/google/uuid"
)

func discountBody(name string, kind string, value int64, startsAt time.Time, endsAt time.Time, productId any) map[string]any {
	return map[string]any{
		"name": name, "kind": kind, "value": value, "product_id": productId,
		"starts_at": startsAt.Format(time.RFC3339), "ends_at": endsAt.Format(time.RFC3339),
	}
}

func TestDiscountRulesStatusesAndIsolation(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Promo Shop", "owner@promo.test")
		otherCompany := harness.CreateCompany("Other Promo", "owner@otherpromo.test")
		now := time.Now().UTC()

		sodaId := harness.Call(http.MethodPost, "/api/products", company.OwnerToken, map[string]any{"sku": "SODA", "name": "Soda", "price": 1000}).Data()["id"].(string)
		foreignId := harness.Call(http.MethodPost, "/api/products", otherCompany.OwnerToken, map[string]any{"sku": "SODA", "name": "Soda", "price": 1000}).Data()["id"].(string)

		sodaPromo := harness.Call(http.MethodPost, "/api/discounts", company.OwnerToken, discountBody(" Soda week ", "percent", 1500, now.Add(-time.Hour), now.Add(time.Hour), sodaId))
		if sodaPromo.Status != http.StatusCreated || sodaPromo.Data()["name"] != "Soda week" || sodaPromo.Data()["status"] != "active" || sodaPromo.Data()["product_name"] != "Soda" {
			t.Fatalf("create returned %d %v", sodaPromo.Status, sodaPromo.Body)
		}
		scheduled := harness.Call(http.MethodPost, "/api/discounts", company.OwnerToken, discountBody("Next week", "fixed", 200, now.Add(24*time.Hour), now.Add(48*time.Hour), nil))
		expired := harness.Call(http.MethodPost, "/api/discounts", company.OwnerToken, discountBody("Last week", "fixed", 200, now.Add(-48*time.Hour), now.Add(-24*time.Hour), nil))
		if scheduled.Data()["status"] != "scheduled" || expired.Data()["status"] != "expired" || scheduled.Data()["product_id"] != nil {
			t.Fatalf("statuses %v and %v", scheduled.Data()["status"], expired.Data()["status"])
		}

		rejected := []struct {
			body       map[string]any
			wantStatus int
		}{
			{discountBody("Too much", "percent", 10001, now, now.Add(time.Hour), nil), http.StatusBadRequest},
			{discountBody("Backwards", "percent", 1000, now, now.Add(-time.Hour), nil), http.StatusBadRequest},
			{discountBody("Instant", "percent", 1000, now, now, nil), http.StatusBadRequest},
			{discountBody("Nothing", "fixed", 0, now, now.Add(time.Hour), nil), http.StatusBadRequest},
			{discountBody("Odd", "bogof", 100, now, now.Add(time.Hour), nil), http.StatusBadRequest},
			{discountBody("", "percent", 100, now, now.Add(time.Hour), nil), http.StatusBadRequest},
			{discountBody("Theirs", "percent", 100, now, now.Add(time.Hour), foreignId), http.StatusNotFound},
			{discountBody("Ghost", "percent", 100, now, now.Add(time.Hour), uuid.Must(uuid.NewV7()).String()), http.StatusNotFound},
		}
		for _, rejectedCase := range rejected {
			rejectedResponse := harness.Call(http.MethodPost, "/api/discounts", company.OwnerToken, rejectedCase.body)
			if rejectedResponse.Status != rejectedCase.wantStatus {
				t.Fatalf("%v returned %d, want %d", rejectedCase.body, rejectedResponse.Status, rejectedCase.wantStatus)
			}
		}

		promoId := sodaPromo.Data()["id"].(string)
		widened := harness.Call(http.MethodPut, "/api/discounts/"+promoId, company.OwnerToken, discountBody("Everything week", "percent", 1000, now.Add(-time.Hour), now.Add(2*time.Hour), nil))
		if widened.Status != http.StatusOK || widened.Data()["product_id"] != nil || widened.Data()["value"] != float64(1000) {
			t.Fatalf("update returned %d %v", widened.Status, widened.Body)
		}
		stopped := harness.Call(http.MethodDelete, "/api/discounts/"+promoId, company.OwnerToken, nil)
		if stopped.Status != http.StatusOK || stopped.Data()["status"] != "stopped" {
			t.Fatalf("stop returned %d %v", stopped.Status, stopped.Body)
		}
		restartBody := discountBody("Everything week", "percent", 1000, now.Add(-time.Hour), now.Add(2*time.Hour), nil)
		restartBody["is_active"] = true
		if harness.Call(http.MethodPut, "/api/discounts/"+promoId, company.OwnerToken, restartBody).Data()["status"] != "active" {
			t.Fatal("a stopped discount could not be restarted")
		}

		listed := harness.Call(http.MethodGet, "/api/discounts", company.OwnerToken, nil)
		if listed.Data()["total"] != float64(3) || harness.Call(http.MethodGet, "/api/discounts", otherCompany.OwnerToken, nil).Data()["total"] != float64(0) {
			t.Fatalf("list totals %v", listed.Data()["total"])
		}
		for _, crossPath := range []string{"/api/discounts/" + promoId} {
			if harness.Call(http.MethodGet, crossPath, otherCompany.OwnerToken, nil).Status != http.StatusNotFound ||
				harness.Call(http.MethodDelete, crossPath, otherCompany.OwnerToken, nil).Status != http.StatusNotFound {
				t.Fatal("another company reached this discount")
			}
		}

		cashierToken := harness.CreateStaff(company, "cashier@promo.test", []string{"products:view"}, []uuid.UUID{company.ShopId})
		if harness.Call(http.MethodGet, "/api/discounts", cashierToken, nil).Status != http.StatusForbidden ||
			harness.Call(http.MethodPost, "/api/discounts", cashierToken, discountBody("Mine", "percent", 100, now, now.Add(time.Hour), nil)).Status != http.StatusForbidden {
			t.Fatal("a cashier without discount permissions got through")
		}
	})
}
