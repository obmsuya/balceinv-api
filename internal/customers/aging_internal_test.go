package customers

import (
	"testing"
	"time"
)

func TestDebtsAgeOldestFirstAgainstAFixedClock(t *testing.T) {
	darEsSalaam, _ := time.LoadLocation("Africa/Dar_es_Salaam")
	asOf := time.Date(2026, 9, 30, 9, 0, 0, 0, darEsSalaam)
	daysAgo := func(days int) time.Time { return asOf.AddDate(0, 0, -days) }
	debts := []Debt{
		{OccurredAt: daysAgo(10), Amount: 4000},
		{OccurredAt: daysAgo(95), Amount: 1000},
		{OccurredAt: daysAgo(45), Amount: 2000},
		{OccurredAt: daysAgo(61), Amount: 3000},
		{OccurredAt: daysAgo(30), Amount: 500},
		{OccurredAt: daysAgo(31), Amount: 700},
	}

	standing := StandingOf(debts, 1500, asOf, darEsSalaam)
	wantAging := AgingView{Days0To30: 4500, Days31To60: 2700, Days61To90: 2500, DaysOver90: 0}
	if standing.Aging != wantAging || standing.Balance != 9700 || standing.OverdueAmount != 5200 {
		t.Fatalf("standing %+v", standing)
	}
	if !standing.OldestDebtAt.Equal(daysAgo(61)) {
		t.Fatalf("oldest unpaid debt %v, want the one from 61 days ago", standing.OldestDebtAt)
	}

	settled := StandingOf(debts, 11200, asOf, darEsSalaam)
	if settled.Balance != 0 || settled.OldestDebtAt != nil || settled.Aging != (AgingView{}) {
		t.Fatalf("fully paid standing %+v", settled)
	}

	lateEvening := time.Date(2026, 9, 29, 23, 30, 0, 0, darEsSalaam)
	if calendarDaysBetween(lateEvening, asOf, darEsSalaam) != 1 {
		t.Fatal("days are not counted on the company's calendar")
	}
}

func TestPhonesAreStoredInTanzanianFormat(t *testing.T) {
	accepted := map[string]string{
		"0712345678":       "0712345678",
		"0712 345 678":     "0712345678",
		"+255 712 345 678": "0712345678",
		"255712345678":     "0712345678",
		"712345678":        "0712345678",
		"(0754) 123-456":   "0754123456",
		"+255-22-212-3456": "0222123456",
	}
	for rawPhone, wantPhone := range accepted {
		normalizedPhone, isValid := NormalizePhone(rawPhone)
		if !isValid || normalizedPhone != wantPhone {
			t.Fatalf("%q became %q valid=%v, want %q", rawPhone, normalizedPhone, isValid, wantPhone)
		}
	}
	for _, rawPhone := range []string{"", "12345", "07123456789", "0012345678", "+1 555 123 4567", "phone"} {
		if _, isValid := NormalizePhone(rawPhone); isValid {
			t.Fatalf("%q was accepted", rawPhone)
		}
	}
	if phoneSearchFragment("+255 712") != "0712" || phoneSearchFragment("Juma") != "" {
		t.Fatal("phone search fragments are not normalised")
	}
}
