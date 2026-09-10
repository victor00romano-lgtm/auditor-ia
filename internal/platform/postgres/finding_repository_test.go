package postgres

import (
	"context"
	"errors"
	"testing"

	"github.com/jackc/pgx/v5/pgconn"
	auditdomain "github.com/portfolio/auditor-ia/internal/audit/domain"
	conversationdomain "github.com/portfolio/auditor-ia/internal/conversation/domain"
)

type findingExecutorStub struct {
	args [][]any
	err  error
}

func (stub *findingExecutorStub) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	stub.args = append(stub.args, args)
	return pgconn.NewCommandTag("INSERT 0 1"), stub.err
}

func TestSaveFindingsPersistsDifferentRulesAndScopesConflictToAssessment(t *testing.T) {
	executor := &findingExecutorStub{}
	findings := []auditdomain.Finding{
		{Rule: "one", Description: "primeiro", Severity: auditdomain.SeverityLow},
		{Rule: "two", Description: "segundo", Severity: auditdomain.SeverityCritical},
	}
	count, err := saveFindings(context.Background(), executor, 101, 31, findings)
	if err != nil || count != 2 || len(executor.args) != 2 {
		t.Fatalf("persistência inesperada: count=%d args=%#v err=%v", count, executor.args, err)
	}
	if executor.args[0][0] != int64(101) || executor.args[0][2] != "one" || executor.args[1][2] != "two" {
		t.Fatalf("mapeamento incorreto: %#v", executor.args)
	}
	other := &findingExecutorStub{}
	_, err = saveFindings(context.Background(), other, 102, 31, findings[:1])
	if err != nil || other.args[0][0] != int64(102) || other.args[0][2] != "one" {
		t.Fatalf("achado histórico não preservado: %#v err=%v", other.args, err)
	}
}

func TestAssessmentRollsBackWhenFindingBatchFails(t *testing.T) {
	tx := &assessmentTxStub{row: rowResult{id: 501}, execErr: errors.New("falha no lote")}
	repository := &AssessmentRepository{tx: assessmentTxStarterStub{tx: tx}}
	_, err := repository.Complete(context.Background(), 101, conversationdomain.AssessmentPersistence{
		Analysis: storedAnalysisForTest(), Findings: []auditdomain.Finding{conversationDomainFinding()}, Score: 80,
	})
	if err == nil || tx.committed || !tx.rolledBack {
		t.Fatalf("rollback não executado: err=%v commit=%v rollback=%v", err, tx.committed, tx.rolledBack)
	}
}

func conversationDomainFinding() auditdomain.Finding {
	return auditdomain.Finding{Rule: "one", Description: "primeiro", Severity: auditdomain.SeverityHigh}
}
