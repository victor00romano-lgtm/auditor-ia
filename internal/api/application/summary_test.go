package application

import (
	"strings"
	"testing"

	apidomain "github.com/portfolio/auditor-ia/internal/api/domain"
)

func TestExecutiveSummaryIsDeterministic(t *testing.T) {
	score := 80.0
	data := apidomain.ReportData{Audit: apidomain.Audit{AssessmentID: 10, Status: "COMPLETED", Score: &score, FinalResult: "venda"}, Findings: []apidomain.Finding{{Severity: "HIGH"}, {Severity: "LOW"}}}
	first, second := ExecutiveSummary(data), ExecutiveSummary(data)
	if first != second || !strings.Contains(first, "score 80 de 100") || !strings.Contains(first, "1 altos") {
		t.Fatalf("resumo inesperado: %q", first)
	}
}
