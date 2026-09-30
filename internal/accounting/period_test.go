package accounting

import (
	"testing"
	"time"

	"github.com/chrisostomemataba/balceinv-api/internal/documents"
)

func TestThePreviousPeriodMatchesTheShapeOfTheReport(t *testing.T) {
	day := func(value string) time.Time {
		parsedDay, _ := time.Parse(dateLayout, value)
		return parsedDay
	}
	cases := []struct {
		name         string
		from, to     string
		wantFrom     string
		wantTo       string
		wantHeadings [2]string
	}{
		{"a whole month", "2026-09-01", "2026-09-30", "2026-08-01", "2026-08-31", [2]string{"Sep 2026", "Aug 2026"}},
		{"March against a leap February", "2028-03-01", "2028-03-31", "2028-02-01", "2028-02-29", [2]string{"Mar 2028", "Feb 2028"}},
		{"January against December", "2027-01-01", "2027-01-31", "2026-12-01", "2026-12-31", [2]string{"Jan 2027", "Dec 2026"}},
		{"a quarter", "2026-07-01", "2026-09-30", "2026-04-01", "2026-06-30", [2]string{"This period", "Previous period"}},
		{"a whole year", "2026-01-01", "2026-12-31", "2025-01-01", "2025-12-31", [2]string{"2026", "2025"}},
		{"month to date", "2026-09-01", "2026-09-17", "2026-08-01", "2026-08-17", [2]string{"1–17 Sep 2026", "1–17 Aug 2026"}},
		{"month to date past the end of a shorter month", "2026-03-01", "2026-03-30", "2026-02-01", "2026-02-28", [2]string{"1–30 Mar 2026", "Feb 2026"}},
		{"one day", "2026-09-17", "2026-09-17", "2026-09-16", "2026-09-16", [2]string{"17 Sep 2026", "16 Sep 2026"}},
		{"days across two months", "2026-08-25", "2026-09-07", "2026-08-11", "2026-08-24", [2]string{"This period", "11–24 Aug 2026"}},
	}
	exporting := exporter{language: documents.English}
	for _, testCase := range cases {
		currentPeriod := periodRange{fromDate: day(testCase.from), toDate: day(testCase.to)}
		previousPeriod := precedingPeriod(currentPeriod)
		if previousPeriod.fromDate.Format(dateLayout) != testCase.wantFrom || previousPeriod.toDate.Format(dateLayout) != testCase.wantTo {
			t.Errorf("%s: previous period %s to %s", testCase.name, previousPeriod.fromDate.Format(dateLayout), previousPeriod.toDate.Format(dateLayout))
		}
		headings := [2]string{exporting.periodHeading(currentPeriod, "thisPeriod"), exporting.periodHeading(previousPeriod, "previousPeriod")}
		if headings != testCase.wantHeadings {
			t.Errorf("%s: headings %v", testCase.name, headings)
		}
	}
}
