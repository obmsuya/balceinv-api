package auth_test

import (
	"net/http"
	"reflect"
	"testing"

	"github.com/chrisostomemataba/balceinv-api/internal/testkit"
	"github.com/chrisostomemataba/balceinv-api/internal/testkit/apptest"
)

func seenToursOf(t *testing.T, harness *apptest.Harness, sessionToken string) []any {
	t.Helper()
	meResponse := harness.Call(http.MethodGet, "/api/auth/me", sessionToken, nil)
	if meResponse.Status != http.StatusOK {
		t.Fatalf("me returned %d: %v", meResponse.Status, meResponse.Body)
	}
	seenTours, isList := meResponse.Data()["seen_tours"].([]any)
	if !isList {
		t.Fatalf("seen_tours missing or not a list: %v", meResponse.Data()["seen_tours"])
	}
	return seenTours
}

func TestSeenToursAreRememberedPerUser(t *testing.T) {
	testkit.ForEachEngine(t, func(t *testing.T, engineCase testkit.EngineCase) {
		harness := apptest.Start(t, engineCase)
		company := harness.CreateCompany("Tour Shop", "owner@tours.test")
		otherCompany := harness.CreateCompany("Other Tour Shop", "owner@othertours.test")

		if seenTours := seenToursOf(t, harness, company.OwnerToken); len(seenTours) != 0 {
			t.Fatalf("a new owner has seen tours %v", seenTours)
		}

		for _, tourName := range []string{"welcome", "products", "welcome"} {
			markResponse := harness.Call(http.MethodPut, "/api/auth/tours", company.OwnerToken, map[string]any{"tour": tourName})
			if markResponse.Status != http.StatusOK {
				t.Fatalf("mark %s returned %d: %v", tourName, markResponse.Status, markResponse.Body)
			}
		}
		if seenTours := seenToursOf(t, harness, company.OwnerToken); !reflect.DeepEqual(seenTours, []any{"welcome", "products"}) {
			t.Fatalf("seen tours = %v, want welcome and products once each", seenTours)
		}
		if seenTours := seenToursOf(t, harness, otherCompany.OwnerToken); len(seenTours) != 0 {
			t.Fatalf("another company's owner sees %v", seenTours)
		}

		for _, badTour := range []string{"", "Welcome", "a,b", "../x"} {
			badResponse := harness.Call(http.MethodPut, "/api/auth/tours", company.OwnerToken, map[string]any{"tour": badTour})
			if badResponse.Status != http.StatusBadRequest && badResponse.Status != http.StatusUnprocessableEntity {
				t.Fatalf("tour %q returned %d, want a validation error", badTour, badResponse.Status)
			}
		}
		if harness.Call(http.MethodPut, "/api/auth/tours", "", map[string]any{"tour": "welcome"}).Status != http.StatusUnauthorized {
			t.Fatal("marking a tour without signing in was allowed")
		}
	})
}
