package domain

import "testing"

func TestCalculateScoreUsesExistingSeverityPenalties(t *testing.T) {
	tests := []struct {
		name     string
		findings []Finding
		want     int
	}{
		{name: "sem achados", want: 100},
		{name: "low", findings: []Finding{{Severity: SeverityLow}}, want: 95},
		{name: "medium", findings: []Finding{{Severity: SeverityMedium}}, want: 90},
		{name: "high", findings: []Finding{{Severity: SeverityHigh}}, want: 80},
		{name: "critical", findings: []Finding{{Severity: SeverityCritical}}, want: 80},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := CalculateScore(test.findings).Value(); got != test.want {
				t.Fatalf("score = %d, esperado %d", got, test.want)
			}
		})
	}
}

func TestCalculateScoreNeverLeavesAllowedRange(t *testing.T) {
	findings := make([]Finding, 20)
	for index := range findings {
		findings[index].Severity = SeverityHigh
	}
	if got := CalculateScore(findings).Value(); got != 0 {
		t.Fatalf("score = %d, esperado 0", got)
	}
}
