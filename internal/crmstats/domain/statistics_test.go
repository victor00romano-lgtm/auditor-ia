package domain

import (
	"testing"
	"time"
)

func TestLastTwelveCalendarMonthsUsesSaoPauloAndFillsZero(t *testing.T) {
	location, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		t.Fatal(err)
	}
	// Em UTC já é setembro, mas ainda é agosto em São Paulo.
	now := time.Date(2026, 9, 1, 2, 30, 0, 0, time.UTC)
	months := LastTwelveCalendarMonths(now, location, map[string]int{"2025-09": 4, "2026-08": 9})
	if len(months) != 12 || months[0] != (MonthlyCount{Month: "2025-09", Count: 4}) || months[1].Month != "2025-10" || months[1].Count != 0 || months[11] != (MonthlyCount{Month: "2026-08", Count: 9}) {
		t.Fatalf("meses=%#v", months)
	}
}

func TestPercentageBasisPointsHandlesZeroAndRounds(t *testing.T) {
	if got := PercentageBasisPoints(1, 0); got != 0 {
		t.Fatalf("divisor zero=%d", got)
	}
	if got := PercentageBasisPoints(2310, 4280); got != 5397 {
		t.Fatalf("percentual=%d", got)
	}
}

func TestLastTwelveMonthlyOutcomesFillsMissingMonths(t *testing.T) {
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	items := LastTwelveMonthlyOutcomes(now, time.UTC, map[string]int{"2026-08": 11}, map[string]int{"2026-07": 7})
	if len(items) != 12 || items[10] != (MonthlyOutcome{Month: "2026-07", Lost: 7}) || items[11] != (MonthlyOutcome{Month: "2026-08", Won: 11}) {
		t.Fatalf("resultados mensais=%#v", items)
	}
}
