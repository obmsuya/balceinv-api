package documents

import (
	"strconv"
	"time"
)

func PreviousPeriod(firstDay time.Time, lastDay time.Time) (time.Time, time.Time) {
	dayBefore := firstDay.AddDate(0, 0, -1)
	coversWholeMonths := firstDay.Day() == 1 && lastDay.AddDate(0, 0, 1).Day() == 1
	if coversWholeMonths {
		monthCount := (lastDay.Year()-firstDay.Year())*12 + int(lastDay.Month()-firstDay.Month()) + 1
		return firstDay.AddDate(0, -monthCount, 0), dayBefore
	}
	isMonthToDate := firstDay.Day() == 1 && lastDay.Year() == firstDay.Year() && lastDay.Month() == firstDay.Month()
	if isMonthToDate {
		previousMonthStart := firstDay.AddDate(0, -1, 0)
		sameDay := previousMonthStart.AddDate(0, 0, lastDay.Day()-1)
		if sameDay.After(dayBefore) {
			sameDay = dayBefore
		}
		return previousMonthStart, sameDay
	}
	dayCount := int(lastDay.Sub(firstDay).Hours()/24+0.5) + 1
	return dayBefore.AddDate(0, 0, -(dayCount - 1)), dayBefore
}

func PeriodHeading(language string, firstDay time.Time, lastDay time.Time, isPrevious bool) string {
	dayAfter := lastDay.AddDate(0, 0, 1)
	isWholeMonth := firstDay.Day() == 1 && dayAfter.Day() == 1 && dayAfter.AddDate(0, -1, 0).Equal(firstDay)
	if isWholeMonth {
		return MonthYear(language, firstDay)
	}
	isWholeYear := firstDay.YearDay() == 1 && dayAfter.YearDay() == 1 && dayAfter.Year() == firstDay.Year()+1
	if isWholeYear {
		return firstDay.Format("2006")
	}
	isWithinOneMonth := firstDay.Year() == lastDay.Year() && firstDay.Month() == lastDay.Month()
	if isWithinOneMonth && firstDay.Equal(lastDay) {
		return FormatDate(language, firstDay)
	}
	if isWithinOneMonth {
		return strconv.Itoa(firstDay.Day()) + "–" + strconv.Itoa(lastDay.Day()) + " " + MonthYear(language, firstDay)
	}
	if isPrevious {
		return Label(language, "periodPrevious")
	}
	return Label(language, "periodThis")
}
