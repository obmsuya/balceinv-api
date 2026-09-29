package customers

import (
	"sort"
	"time"
)

const overdueAfterDays = 30

type DebtStanding struct {
	Balance       int64
	OverdueAmount int64
	OldestDebtAt  *time.Time
	Aging         AgingView
}

func StandingOf(debts []Debt, paidTotal int64, asOf time.Time, companyLocation *time.Location) DebtStanding {
	oldestFirst := append([]Debt{}, debts...)
	sort.SliceStable(oldestFirst, func(left int, right int) bool {
		return oldestFirst[left].OccurredAt.Before(oldestFirst[right].OccurredAt)
	})

	standing := DebtStanding{}
	paymentLeft := paidTotal
	for _, debt := range oldestFirst {
		unpaidAmount := debt.Amount
		appliedPayment := min(unpaidAmount, paymentLeft)
		unpaidAmount -= appliedPayment
		paymentLeft -= appliedPayment
		if unpaidAmount <= 0 {
			continue
		}

		standing.Balance += unpaidAmount
		if standing.OldestDebtAt == nil {
			occurredAt := debt.OccurredAt
			standing.OldestDebtAt = &occurredAt
		}

		ageInDays := calendarDaysBetween(debt.OccurredAt, asOf, companyLocation)
		switch {
		case ageInDays <= 30:
			standing.Aging.Days0To30 += unpaidAmount
		case ageInDays <= 60:
			standing.Aging.Days31To60 += unpaidAmount
		case ageInDays <= 90:
			standing.Aging.Days61To90 += unpaidAmount
		default:
			standing.Aging.DaysOver90 += unpaidAmount
		}
		if ageInDays > overdueAfterDays {
			standing.OverdueAmount += unpaidAmount
		}
	}

	return standing
}

func calendarDaysBetween(earlier time.Time, later time.Time, companyLocation *time.Location) int {
	earlierLocal := earlier.In(companyLocation)
	laterLocal := later.In(companyLocation)
	earlierDay := time.Date(earlierLocal.Year(), earlierLocal.Month(), earlierLocal.Day(), 0, 0, 0, 0, time.UTC)
	laterDay := time.Date(laterLocal.Year(), laterLocal.Month(), laterLocal.Day(), 0, 0, 0, 0, time.UTC)
	return int(laterDay.Sub(earlierDay).Hours() / 24)
}
