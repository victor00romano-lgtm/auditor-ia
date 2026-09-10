package domain

import "time"

func LastTwelveCalendarMonths(now time.Time, location *time.Location, counts map[string]int) []MonthlyCount {
	if location == nil {
		location = time.UTC
	}
	local := now.In(location)
	start := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, location).AddDate(0, -11, 0)
	result := make([]MonthlyCount, 0, 12)
	for index := 0; index < 12; index++ {
		month := start.AddDate(0, index, 0).Format("2006-01")
		result = append(result, MonthlyCount{Month: month, Count: counts[month]})
	}
	return result
}

func LastTwelveMonthlyOutcomes(now time.Time, location *time.Location, won, lost map[string]int) []MonthlyOutcome {
	if location == nil {
		location = time.UTC
	}
	local := now.In(location)
	start := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, location).AddDate(0, -11, 0)
	result := make([]MonthlyOutcome, 0, 12)
	for index := 0; index < 12; index++ {
		month := start.AddDate(0, index, 0).Format("2006-01")
		result = append(result, MonthlyOutcome{Month: month, Won: won[month], Lost: lost[month]})
	}
	return result
}

func PercentageBasisPoints(part, total int) int64 {
	if part <= 0 || total <= 0 {
		return 0
	}
	return int64(part*10000+total/2) / int64(total)
}
