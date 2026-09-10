package application

import (
	"testing"

	auditdomain "github.com/portfolio/auditor-ia/internal/audit/domain"
	"github.com/portfolio/auditor-ia/internal/audit/infrastructure/rules"
	dealdomain "github.com/portfolio/auditor-ia/internal/deal/domain"
)

func TestOpportunityWithoutValueOnlyAppliesToOpenDeal(t *testing.T) {
	open, closed := false, true
	amount := "0.00"
	engine := &rules.Engine{Rules: []rules.Rule{{
		Name: "opportunity_without_value", Entity: "deal", Severity: auditdomain.SeverityHigh,
		Conditions: []rules.Condition{{Field: "closed", Operator: "equals", Value: "N"}, {Field: "amount", Operator: "less_than", Value: "0.01"}},
	}}}
	for _, test := range []struct {
		name   string
		closed *bool
		want   int
	}{{"aberto", &open, 1}, {"fechado", &closed, 0}} {
		t.Run(test.name, func(t *testing.T) {
			findings, err := engine.Evaluate([]auditdomain.Record{DealRecord(dealdomain.Deal{BitrixDealID: 8620, Closed: test.closed, Amount: &amount})})
			if err != nil {
				t.Fatal(err)
			}
			if len(findings) != test.want {
				t.Fatalf("achados = %d, esperado %d", len(findings), test.want)
			}
		})
	}
}
