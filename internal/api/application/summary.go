package application

import (
	"fmt"
	"strings"

	apidomain "github.com/portfolio/auditor-ia/internal/api/domain"
)

func ExecutiveSummary(data apidomain.ReportData) string {
	score := "não calculado"
	if data.Audit.Score != nil {
		score = fmt.Sprintf("%.0f de 100", *data.Audit.Score)
	}
	counts := map[string]int{}
	for _, finding := range data.Findings {
		counts[finding.Severity]++
	}
	return fmt.Sprintf("A avaliação %d terminou com status %s, score %s e resultado final %s. Foram registrados %d achados: %d críticos, %d altos, %d médios e %d baixos.", data.Audit.AssessmentID, data.Audit.Status, score, valueOr(data.Audit.FinalResult, "indeterminado"), len(data.Findings), counts["CRITICAL"], counts["HIGH"], counts["MEDIUM"], counts["LOW"])
}

func valueOr(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}
