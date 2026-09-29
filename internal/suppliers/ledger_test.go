package suppliers

import (
	"testing"
	"time"

	"github.com/google/uuid"
)

func TestPriceLineHandlesVatBothWays(t *testing.T) {
	productId := uuid.Must(uuid.NewV7())

	notRegistered := PriceLine(productId, 3, 1180, false, true, 1800)
	if notRegistered.UnitCost != 1180 || notRegistered.VatAmount != 0 || notRegistered.LineTotal != 3540 {
		t.Fatalf("a shop without VAT got %+v", notRegistered)
	}

	inclusive := PriceLine(productId, 2, 1180, true, true, 1800)
	if inclusive.UnitCost != 1000 || inclusive.VatAmount != 360 || inclusive.LineTotal != 2360 {
		t.Fatalf("VAT-inclusive prices gave %+v", inclusive)
	}

	exclusive := PriceLine(productId, 3, 1000, true, false, 1800)
	if exclusive.UnitCost != 1000 || exclusive.VatAmount != 540 || exclusive.LineTotal != 3540 {
		t.Fatalf("VAT-exclusive prices gave %+v", exclusive)
	}

	roundedInclusive := PriceLine(productId, 1, 999, true, true, 1800)
	if roundedInclusive.VatAmount != 152 || roundedInclusive.UnitCost != 847 || roundedInclusive.LineTotal != 999 {
		t.Fatalf("VAT on 999 inclusive gave %+v, want 152 VAT and 847 cost", roundedInclusive)
	}

	zeroRate := PriceLine(productId, 2, 500, true, false, 0)
	if zeroRate.VatAmount != 0 || zeroRate.LineTotal != 1000 {
		t.Fatalf("a zero VAT rate gave %+v", zeroRate)
	}
}

func TestWeightedAverageCostRoundsAndIgnoresEmptyStock(t *testing.T) {
	cases := []struct {
		name             string
		onHand           int64
		currentCost      int64
		receivedQuantity int64
		receivedAmount   int64
		want             int64
	}{
		{"blends old and new stock", 10, 1000, 5, 6500, 1100},
		{"rounds half up", 15, 1100, 3, 3003, 1084},
		{"rounds down below half", 1, 100, 2, 201, 100},
		{"uses the new cost when nothing is on hand", 0, 500, 4, 3108, 777},
		{"uses the new cost when stock is negative", -3, 500, 4, 3108, 777},
		{"rounds the new cost per unit", 0, 0, 3, 1000, 333},
		{"keeps the cost when nothing arrives", 5, 900, 0, 0, 900},
		{"handles huge values", 1_000_000_000, 10_000_000_000, 1_000_000, 10_000_000_000_000_000, 10_000_000_000},
	}
	for _, testCase := range cases {
		got := WeightedAverageCost(testCase.onHand, testCase.currentCost, testCase.receivedQuantity, testCase.receivedAmount)
		if got != testCase.want {
			t.Errorf("%s: got %d, want %d", testCase.name, got, testCase.want)
		}
	}
}

func TestAllocationAgesOldestFirstAndHonoursLinkedPayments(t *testing.T) {
	location, _ := time.LoadLocation("Africa/Dar_es_Salaam")
	day := func(year int, month time.Month, dayOfMonth int) time.Time {
		return time.Date(year, month, dayOfMonth, 12, 0, 0, 0, location)
	}
	oldPurchase := uuid.Must(uuid.NewV7())
	middlePurchase := uuid.Must(uuid.NewV7())
	newPurchase := uuid.Must(uuid.NewV7())
	entries := []LedgerEntry{
		{Kind: EntryPurchase, DocumentId: &newPurchase, Reference: "PUR-000003", DatedAt: day(2026, 6, 15), Debit: 3000},
		{Kind: EntryOpeningBalance, DatedAt: day(2025, 12, 1), Debit: 500},
		{Kind: EntryPurchase, DocumentId: &oldPurchase, Reference: "PUR-000001", DatedAt: day(2026, 1, 10), Debit: 1000},
		{Kind: EntryPurchase, DocumentId: &middlePurchase, Reference: "PUR-000002", DatedAt: day(2026, 4, 20), Debit: 2000},
		{Kind: EntryPayment, Reference: "PAY-000001", DatedAt: day(2026, 6, 20), Credit: 700},
		{Kind: EntryPayment, Reference: "PAY-000002", DatedAt: day(2026, 6, 21), Credit: 1000, PurchaseId: &newPurchase},
		{Kind: EntryReturn, Reference: "RET-000001", DatedAt: day(2026, 6, 22), Credit: 100},
	}

	openDebits, unappliedCredit := Allocate(entries)
	if unappliedCredit != 0 || Balance(openDebits, unappliedCredit) != 4700 {
		t.Fatalf("balance %d with %d unapplied, want 4700 and 0", Balance(openDebits, unappliedCredit), unappliedCredit)
	}
	remaining := []int64{openDebits[0].Remaining, openDebits[1].Remaining, openDebits[2].Remaining, openDebits[3].Remaining}
	wantRemaining := []int64{0, 700, 2000, 2000}
	for debitIndex := range wantRemaining {
		if remaining[debitIndex] != wantRemaining[debitIndex] {
			t.Fatalf("remaining %v, want %v", remaining, wantRemaining)
		}
	}

	agingBuckets := Age(openDebits, 30, time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC), location)
	wantBuckets := AgingBuckets{Current: 2000, Days1To30: 0, Days31To60: 2000, Days61To90: 0, DaysOver90: 700}
	if agingBuckets != wantBuckets || agingBuckets.Overdue() != 2700 {
		t.Fatalf("aging %+v, want %+v", agingBuckets, wantBuckets)
	}

	edgeBuckets := Age([]OpenDebit{
		{DatedAt: day(2026, 5, 31), Remaining: 1},
		{DatedAt: day(2026, 5, 30), Remaining: 10},
		{DatedAt: day(2026, 4, 30), Remaining: 100},
		{DatedAt: day(2026, 3, 31), Remaining: 1000},
		{DatedAt: day(2026, 3, 30), Remaining: 10000},
	}, 0, time.Date(2026, 6, 29, 0, 0, 0, 0, time.UTC), location)
	wantEdges := AgingBuckets{Current: 0, Days1To30: 11, Days31To60: 100, Days61To90: 1000, DaysOver90: 10000}
	if edgeBuckets != wantEdges {
		t.Fatalf("bucket edges %+v, want %+v", edgeBuckets, wantEdges)
	}

	overpaid, credit := Allocate([]LedgerEntry{
		{Kind: EntryPurchase, DocumentId: &oldPurchase, DatedAt: day(2026, 1, 1), Debit: 1000},
		{Kind: EntryReturn, DatedAt: day(2026, 1, 2), Credit: 1500},
	})
	if Balance(overpaid, credit) != -500 || credit != 500 || Age(overpaid, 0, time.Date(2026, 6, 30, 0, 0, 0, 0, time.UTC), location) != (AgingBuckets{}) {
		t.Fatalf("a supplier who owes the shop gave balance %d and credit %d", Balance(overpaid, credit), credit)
	}
}

func TestStatementCarriesTheOpeningBalanceIntoTheRange(t *testing.T) {
	moment := func(dayOfMonth int) time.Time {
		return time.Date(2026, 7, dayOfMonth, 9, 0, 0, 0, time.UTC)
	}
	entries := []LedgerEntry{
		{Kind: EntryPurchase, Reference: "PUR-000002", DatedAt: moment(10), Debit: 400},
		{Kind: EntryOpeningBalance, DatedAt: moment(1), Debit: 1000},
		{Kind: EntryPayment, Reference: "PAY-000001", DatedAt: moment(5), Credit: 300},
		{Kind: EntryReturn, Reference: "RET-000001", DatedAt: moment(12), Credit: 50},
		{Kind: EntryPurchase, Reference: "PUR-000003", DatedAt: moment(20), Debit: 999},
	}

	openingBalance, statementLines, closingBalance := Statement(entries, moment(6), moment(15))
	if openingBalance != 700 || closingBalance != 1050 || len(statementLines) != 2 {
		t.Fatalf("statement opening %d closing %d lines %d, want 700, 1050 and 2", openingBalance, closingBalance, len(statementLines))
	}
	if statementLines[0].Balance != 1100 || statementLines[1].Balance != 1050 || statementLines[1].Kind != EntryReturn {
		t.Fatalf("running balances %+v", statementLines)
	}

	emptyOpening, emptyLines, emptyClosing := Statement(entries, moment(25), moment(28))
	if emptyOpening != 2049 || emptyClosing != 2049 || len(emptyLines) != 0 {
		t.Fatalf("an empty range gave %d, %d, %d lines", emptyOpening, emptyClosing, len(emptyLines))
	}
}

func TestNormalizePhoneUsesTheTanzanianFormat(t *testing.T) {
	cases := map[string]string{
		"0712 345 678":      "+255712345678",
		"712345678":         "+255712345678",
		"+255 712 345 678":  "+255712345678",
		"255-712-345-678":   "+255712345678",
		"022 212 3456":      "+255222123456",
		"+86 138 0013 8000": "+8613800138000",
		"0086 13800138000":  "+8613800138000",
	}
	for rawPhone, wantPhone := range cases {
		gotPhone, isValid := NormalizePhone(rawPhone)
		if !isValid || gotPhone != wantPhone {
			t.Errorf("%q gave %q %v, want %q", rawPhone, gotPhone, isValid, wantPhone)
		}
	}
	for _, badPhone := range []string{"12", "0812 345 678", "call me", "07123"} {
		if _, isValid := NormalizePhone(badPhone); isValid {
			t.Errorf("%q was accepted", badPhone)
		}
	}
}
