package rules

import (
	"testing"
	"time"

	"github.com/portfolio/auditor-ia/internal/audit/domain"
)

func TestEvaluate(t *testing.T) {
	e := Engine{Rules: []Rule{{Name: "deal_without_owner", Entity: "deal", Field: "owner_id", Operator: "is_empty", Severity: domain.SeverityHigh}}, Now: time.Now}
	got, err := e.Evaluate([]domain.Record{{Type: "deal", ID: "D-1", Fields: map[string]any{"owner_id": ""}}})
	if err != nil {
		t.Fatalf("erro inesperado: %v", err)
	}
	if len(got) != 1 {
		t.Fatalf("quantidade de achados = %d, esperado 1", len(got))
	}

	if got[0].Rule != "deal_without_owner" {
		t.Fatalf(
			"regra encontrada = %q, esperado %q",
			got[0].Rule,
			"deal_without_owner",
		)

	}
}
