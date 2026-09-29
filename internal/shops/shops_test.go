package shops_test

import (
	"net/http"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
	"github.com/google/uuid"
)

func TestShopsCanBeOpenedRenamedAndClosed(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Chain Store", "owner@chain.test")
		otherCompany := harness.CreateCompany("Rival Chain", "owner@rivalchain.test")

		branch := harness.Call(http.MethodPost, "/api/shops", company.OwnerToken, map[string]any{
			"name": "  Kariakoo Branch ", "phone": "0712 000 000", "receipt_prefix": "kko",
		})
		if branch.Status != http.StatusCreated || branch.Data()["name"] != "Kariakoo Branch" || branch.Data()["receipt_prefix"] != "KKO" || branch.Data()["address"] != nil {
			t.Fatalf("create branch returned %d %v", branch.Status, branch.Body)
		}
		branchId := branch.Data()["id"].(string)

		duplicateName := harness.Call(http.MethodPost, "/api/shops", company.OwnerToken, map[string]any{"name": "Kariakoo Branch"})
		sameNameElsewhere := harness.Call(http.MethodPost, "/api/shops", otherCompany.OwnerToken, map[string]any{"name": "Kariakoo Branch"})
		if duplicateName.Status != http.StatusConflict || duplicateName.Code() != "shop_name_taken" || sameNameElsewhere.Status != http.StatusCreated {
			t.Fatalf("duplicate name returned %d, other company %d", duplicateName.Status, sameNameElsewhere.Status)
		}

		invalidBodies := []map[string]any{
			{"name": ""},
			{"name": "Bad Prefix", "receipt_prefix": "SALE-1"},
			{"name": "Long Prefix", "receipt_prefix": "ABCDEFGHIJKLM"},
		}
		for _, invalidBody := range invalidBodies {
			invalidShop := harness.Call(http.MethodPost, "/api/shops", company.OwnerToken, invalidBody)
			if invalidShop.Status != http.StatusBadRequest {
				t.Fatalf("%v returned %d, want 400", invalidBody, invalidShop.Status)
			}
		}

		shopList := harness.Call(http.MethodGet, "/api/shops", company.OwnerToken, nil)
		if shopList.Status != http.StatusOK || shopList.Data()["total"] != float64(2) {
			t.Fatalf("shop list returned %d %v", shopList.Status, shopList.Body)
		}

		renamed := harness.Call(http.MethodPut, "/api/shops/"+branchId, company.OwnerToken, map[string]any{"name": "Kariakoo", "address": "Msimbazi St"})
		if renamed.Status != http.StatusOK || renamed.Data()["name"] != "Kariakoo" || renamed.Data()["address"] != "Msimbazi St" || renamed.Data()["receipt_prefix"] != "SALE" {
			t.Fatalf("rename returned %d %v", renamed.Status, renamed.Body)
		}

		closeBranch := harness.Call(http.MethodDelete, "/api/shops/"+branchId, company.OwnerToken, nil)
		if closeBranch.Status != http.StatusOK || closeBranch.Data()["is_active"] != false {
			t.Fatalf("close branch returned %d %v", closeBranch.Status, closeBranch.Body)
		}
		closeLast := harness.Call(http.MethodDelete, "/api/shops/"+company.ShopId.String(), company.OwnerToken, nil)
		closeLastByUpdate := harness.Call(http.MethodPut, "/api/shops/"+company.ShopId.String(), company.OwnerToken, map[string]any{"name": "Main Shop", "is_active": false})
		if closeLast.Status != http.StatusConflict || closeLast.Code() != "last_active_shop" || closeLastByUpdate.Status != http.StatusConflict {
			t.Fatalf("closing the last open shop returned %d and %d, want 409", closeLast.Status, closeLastByUpdate.Status)
		}
		reopen := harness.Call(http.MethodPut, "/api/shops/"+branchId, company.OwnerToken, map[string]any{"name": "Kariakoo", "is_active": true})
		if reopen.Status != http.StatusOK || reopen.Data()["is_active"] != true {
			t.Fatalf("reopen returned %d %v", reopen.Status, reopen.Body)
		}

		crossRequests := []struct {
			method string
			path   string
			body   any
		}{
			{http.MethodGet, "/api/shops/" + branchId, nil},
			{http.MethodPut, "/api/shops/" + branchId, map[string]any{"name": "Taken over"}},
			{http.MethodDelete, "/api/shops/" + branchId, nil},
			{http.MethodGet, "/api/shops/" + uuid.Must(uuid.NewV7()).String(), nil},
			{http.MethodGet, "/api/shops/not-a-uuid", nil},
		}
		for _, crossRequest := range crossRequests {
			crossResponse := harness.Call(crossRequest.method, crossRequest.path, otherCompany.OwnerToken, crossRequest.body)
			if crossResponse.Status != http.StatusNotFound {
				t.Fatalf("%s %s returned %d, want 404", crossRequest.method, crossRequest.path, crossResponse.Status)
			}
		}

		cashierToken := harness.CreateStaff(company, "cashier@chain.test", []string{"products:view"}, []uuid.UUID{company.ShopId})
		cashierList := harness.Call(http.MethodGet, "/api/shops", cashierToken, nil)
		cashierCreate := harness.Call(http.MethodPost, "/api/shops", cashierToken, map[string]any{"name": "Rogue"})
		if cashierList.Status != http.StatusForbidden || cashierCreate.Status != http.StatusForbidden {
			t.Fatalf("cashier list/create returned %d/%d, want 403/403", cashierList.Status, cashierCreate.Status)
		}

		ownerMe := harness.Call(http.MethodGet, "/api/auth/me", company.OwnerToken, nil)
		ownerShops, _ := ownerMe.Data()["shops"].([]any)
		if len(ownerShops) != 2 {
			t.Fatalf("the owner can work in %d shops, want both open shops", len(ownerShops))
		}
	})
}
