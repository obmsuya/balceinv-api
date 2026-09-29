package suppliers

import (
	"math/big"
	"regexp"
	"sort"
	"strings"
	"time"

	"github.com/google/uuid"
)

var (
	tanzanianNumberPattern     = regexp.MustCompile(`^[2-7]\d{8}$`)
	internationalNumberPattern = regexp.MustCompile(`^\d{8,15}$`)
	entryKindOrder             = map[string]int{EntryOpeningBalance: 0, EntryPurchase: 1, EntryReturn: 2, EntryPayment: 3}
)

func mulDivRound(firstFactor int64, secondFactor int64, divisor int64) int64 {
	product := new(big.Int).Mul(big.NewInt(firstFactor), big.NewInt(secondFactor))
	halfDivisor := new(big.Int).Quo(big.NewInt(divisor), big.NewInt(2))
	product.Add(product, halfDivisor)
	return product.Quo(product, big.NewInt(divisor)).Int64()
}

func PriceLine(productId uuid.UUID, quantity int, typedUnitCost int64, chargesVat bool, pricesIncludeVat bool, taxRateBasisPoints int) PricedLine {
	pricedLine := PricedLine{
		ProductId: productId,
		Quantity:  quantity,
		UnitCost:  typedUnitCost,
		LineTotal: int64(quantity) * typedUnitCost,
	}
	if !chargesVat || taxRateBasisPoints <= 0 {
		return pricedLine
	}

	vatRate := int64(taxRateBasisPoints)
	if pricesIncludeVat {
		pricedLine.VatAmount = mulDivRound(pricedLine.LineTotal, vatRate, basisPointsDenominator+vatRate)
		pricedLine.UnitCost = mulDivRound(typedUnitCost, basisPointsDenominator, basisPointsDenominator+vatRate)
		return pricedLine
	}

	pricedLine.VatAmount = mulDivRound(pricedLine.LineTotal, vatRate, basisPointsDenominator)
	pricedLine.LineTotal += pricedLine.VatAmount
	return pricedLine
}

func WeightedAverageCost(onHandQuantity int64, currentCost int64, receivedQuantity int64, receivedAmount int64) int64 {
	if receivedQuantity <= 0 {
		return currentCost
	}
	if onHandQuantity <= 0 {
		return mulDivRound(receivedAmount, 1, receivedQuantity)
	}

	existingValue := new(big.Int).Mul(big.NewInt(onHandQuantity), big.NewInt(currentCost))
	combinedValue := existingValue.Add(existingValue, big.NewInt(receivedAmount))
	combinedQuantity := onHandQuantity + receivedQuantity
	combinedValue.Add(combinedValue, big.NewInt(combinedQuantity/2))
	return combinedValue.Quo(combinedValue, big.NewInt(combinedQuantity)).Int64()
}

func sortEntries(entries []LedgerEntry) {
	sort.SliceStable(entries, func(firstIndex int, secondIndex int) bool {
		firstEntry := entries[firstIndex]
		secondEntry := entries[secondIndex]
		if !firstEntry.DatedAt.Equal(secondEntry.DatedAt) {
			return firstEntry.DatedAt.Before(secondEntry.DatedAt)
		}
		if entryKindOrder[firstEntry.Kind] != entryKindOrder[secondEntry.Kind] {
			return entryKindOrder[firstEntry.Kind] < entryKindOrder[secondEntry.Kind]
		}
		return firstEntry.Reference < secondEntry.Reference
	})
}

func Allocate(entries []LedgerEntry) ([]OpenDebit, int64) {
	sortEntries(entries)

	openDebits := []OpenDebit{}
	linkedCredits := map[uuid.UUID]int64{}
	unlinkedCredit := int64(0)
	for _, entry := range entries {
		if entry.Debit > 0 {
			openDebits = append(openDebits, OpenDebit{PurchaseId: entry.DocumentIdForPurchase(), DatedAt: entry.DatedAt, Amount: entry.Debit, Remaining: entry.Debit})
			continue
		}
		if entry.PurchaseId != nil {
			linkedCredits[*entry.PurchaseId] += entry.Credit
			continue
		}
		unlinkedCredit += entry.Credit
	}

	for debitIndex := range openDebits {
		openDebit := &openDebits[debitIndex]
		if openDebit.PurchaseId == nil {
			continue
		}
		appliedCredit := min(linkedCredits[*openDebit.PurchaseId], openDebit.Remaining)
		openDebit.Remaining -= appliedCredit
		linkedCredits[*openDebit.PurchaseId] -= appliedCredit
	}
	for _, leftoverCredit := range linkedCredits {
		unlinkedCredit += leftoverCredit
	}

	for debitIndex := range openDebits {
		openDebit := &openDebits[debitIndex]
		appliedCredit := min(unlinkedCredit, openDebit.Remaining)
		openDebit.Remaining -= appliedCredit
		unlinkedCredit -= appliedCredit
	}

	return openDebits, unlinkedCredit
}

func (entry LedgerEntry) DocumentIdForPurchase() *uuid.UUID {
	if entry.Kind != EntryPurchase {
		return nil
	}
	return entry.DocumentId
}

func Balance(openDebits []OpenDebit, unappliedCredit int64) int64 {
	outstanding := int64(0)
	for _, openDebit := range openDebits {
		outstanding += openDebit.Remaining
	}
	return outstanding - unappliedCredit
}

func civilDate(moment time.Time, location *time.Location) time.Time {
	year, month, day := moment.In(location).Date()
	return time.Date(year, month, day, 0, 0, 0, 0, time.UTC)
}

func Age(openDebits []OpenDebit, paymentTermsDays int, asOfDate time.Time, location *time.Location) AgingBuckets {
	agingBuckets := AgingBuckets{}
	for _, openDebit := range openDebits {
		if openDebit.Remaining <= 0 {
			continue
		}
		dueDate := civilDate(openDebit.DatedAt, location).AddDate(0, 0, paymentTermsDays)
		daysOverdue := int(asOfDate.Sub(dueDate).Hours() / 24)
		switch {
		case daysOverdue <= 0:
			agingBuckets.Current += openDebit.Remaining
		case daysOverdue <= 30:
			agingBuckets.Days1To30 += openDebit.Remaining
		case daysOverdue <= 60:
			agingBuckets.Days31To60 += openDebit.Remaining
		case daysOverdue <= 90:
			agingBuckets.Days61To90 += openDebit.Remaining
		default:
			agingBuckets.DaysOver90 += openDebit.Remaining
		}
	}
	return agingBuckets
}

func (agingBuckets AgingBuckets) Overdue() int64 {
	return agingBuckets.Days1To30 + agingBuckets.Days31To60 + agingBuckets.Days61To90 + agingBuckets.DaysOver90
}

func (agingBuckets AgingBuckets) Plus(otherBuckets AgingBuckets) AgingBuckets {
	return AgingBuckets{
		Current:    agingBuckets.Current + otherBuckets.Current,
		Days1To30:  agingBuckets.Days1To30 + otherBuckets.Days1To30,
		Days31To60: agingBuckets.Days31To60 + otherBuckets.Days31To60,
		Days61To90: agingBuckets.Days61To90 + otherBuckets.Days61To90,
		DaysOver90: agingBuckets.DaysOver90 + otherBuckets.DaysOver90,
	}
}

func Statement(entries []LedgerEntry, rangeStart time.Time, rangeEnd time.Time) (int64, []StatementLineView, int64) {
	sortEntries(entries)

	openingBalance := int64(0)
	statementLines := []StatementLineView{}
	runningBalance := int64(0)
	for _, entry := range entries {
		if entry.DatedAt.Before(rangeStart) {
			openingBalance += entry.Debit - entry.Credit
			runningBalance = openingBalance
			continue
		}
		if !entry.DatedAt.Before(rangeEnd) {
			continue
		}
		runningBalance += entry.Debit - entry.Credit
		statementLines = append(statementLines, StatementLineView{
			Date:       entry.DatedAt,
			Kind:       entry.Kind,
			Reference:  entry.Reference,
			DocumentId: entry.DocumentId,
			Debit:      entry.Debit,
			Credit:     entry.Credit,
			Balance:    runningBalance,
		})
	}
	if len(statementLines) == 0 {
		runningBalance = openingBalance
	}
	return openingBalance, statementLines, runningBalance
}

func NormalizePhone(rawPhone string) (string, bool) {
	trimmedPhone := strings.TrimSpace(rawPhone)
	hasPlus := strings.HasPrefix(trimmedPhone, "+")
	digits := strings.Map(func(character rune) rune {
		if character >= '0' && character <= '9' {
			return character
		}
		return -1
	}, trimmedPhone)

	switch {
	case strings.HasPrefix(digits, "255") && tanzanianNumberPattern.MatchString(digits[3:]):
		return "+" + digits, true
	case strings.HasPrefix(digits, "0") && !strings.HasPrefix(digits, "00") && tanzanianNumberPattern.MatchString(digits[1:]):
		return "+255" + digits[1:], true
	case !hasPlus && tanzanianNumberPattern.MatchString(digits):
		return "+255" + digits, true
	case strings.HasPrefix(digits, "00") && internationalNumberPattern.MatchString(digits[2:]):
		return "+" + digits[2:], true
	case hasPlus && internationalNumberPattern.MatchString(digits):
		return "+" + digits, true
	default:
		return "", false
	}
}
