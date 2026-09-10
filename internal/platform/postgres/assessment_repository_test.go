package postgres

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	auditdomain "github.com/portfolio/auditor-ia/internal/audit/domain"
	conversationdomain "github.com/portfolio/auditor-ia/internal/conversation/domain"
	dealdomain "github.com/portfolio/auditor-ia/internal/deal/domain"
)

type assessmentTxStub struct {
	row        pgx.Row
	execErr    error
	queryArgs  []any
	execArgs   []any
	committed  bool
	rolledBack bool
}

func (tx *assessmentTxStub) QueryRow(_ context.Context, _ string, args ...any) pgx.Row {
	tx.queryArgs = args
	return tx.row
}

func (tx *assessmentTxStub) Exec(_ context.Context, _ string, args ...any) (pgconn.CommandTag, error) {
	tx.execArgs = args
	if tx.execErr != nil {
		return pgconn.CommandTag{}, tx.execErr
	}
	return pgconn.NewCommandTag("UPDATE 1"), nil
}

func (tx *assessmentTxStub) Commit(context.Context) error {
	tx.committed = true
	return nil
}

func (tx *assessmentTxStub) Rollback(context.Context) error {
	tx.rolledBack = true
	return nil
}

type assessmentTxStarterStub struct {
	tx assessmentTx
}

func (starter assessmentTxStarterStub) Begin(context.Context) (assessmentTx, error) {
	return starter.tx, nil
}

func storedAnalysisForTest() conversationdomain.StoredConversationAnalysis {
	started := time.Date(2026, 8, 7, 10, 0, 0, 0, time.UTC)
	return conversationdomain.StoredConversationAnalysis{
		DealID:             31,
		Status:             "COMPLETED",
		ProbableResult:     "em negociação",
		FinalResult:        "venda",
		ResultSource:       "estágio ganho no Bitrix",
		MainReason:         "motivo",
		CustomerObjections: "nenhuma",
		ServiceQuality:     "boa",
		RecommendedAction:  "nenhuma correção",
		Confidence:         "alta",
		Observation:        "consistente",
		PossibleReceipt:    true,
		MessageCount:       12,
		Model:              "modelo",
		PromptVersion:      conversationdomain.PromptVersion,
		RawResponse:        "resposta bruta",
		StartedAt:          started,
		FinishedAt:         started.Add(time.Minute),
		Summary:            "motivo",
		ConversationStatus: conversationdomain.ConversationAvailable,
	}
}

func TestAssessmentRepositoryPersistsCompletedAnalysis(t *testing.T) {
	tx := &assessmentTxStub{row: rowResult{id: 501}}
	repository := &AssessmentRepository{tx: assessmentTxStarterStub{tx: tx}}
	result, err := repository.Complete(context.Background(), 101, conversationdomain.AssessmentPersistence{Analysis: storedAnalysisForTest(), Score: 100})
	if err != nil {
		t.Fatal(err)
	}
	if result.AnalysisID != 501 || !tx.committed || tx.rolledBack {
		t.Fatalf("transação inesperada: result=%#v commit=%v rollback=%v", result, tx.committed, tx.rolledBack)
	}
	if len(tx.queryArgs) != 24 || tx.queryArgs[0] != int64(101) || tx.queryArgs[1] != int64(31) || tx.queryArgs[2] != "COMPLETED" || tx.queryArgs[3] != "em negociação" || tx.queryArgs[4] != "venda" || tx.queryArgs[20] != "AVAILABLE" {
		t.Fatalf("mapeamento da análise incorreto: %#v", tx.queryArgs)
	}
	if possibleReceipt, ok := tx.queryArgs[12].(bool); !ok || !possibleReceipt {
		t.Fatalf("possible_receipt não persistido como true: %#v", tx.queryArgs[12])
	}
	if len(tx.execArgs) != 7 || tx.execArgs[0] != int64(101) || tx.execArgs[1] != "COMPLETED" || tx.execArgs[2] != 100 {
		t.Fatalf("atualização da avaliação incorreta: %#v", tx.execArgs)
	}
}

func TestAssessmentRepositoryRollsBackWhenAnalysisInsertFails(t *testing.T) {
	tx := &assessmentTxStub{row: rowResult{err: errors.New("falha de inserção")}}
	repository := &AssessmentRepository{tx: assessmentTxStarterStub{tx: tx}}
	_, err := repository.Complete(context.Background(), 101, conversationdomain.AssessmentPersistence{Analysis: storedAnalysisForTest(), Score: 100})
	if err == nil || tx.committed || !tx.rolledBack {
		t.Fatalf("rollback não executado: err=%v commit=%v rollback=%v", err, tx.committed, tx.rolledBack)
	}
}

func TestAssessmentRepositoryPersistsFailedRawResponseAtomically(t *testing.T) {
	tx := &assessmentTxStub{row: rowResult{id: 502}}
	repository := &AssessmentRepository{tx: assessmentTxStarterStub{tx: tx}}
	analysis := storedAnalysisForTest()
	analysis.Status = "FAILED"
	analysis.RawResponse = "resposta original inválida"
	analysis.ErrorMessage = "resultado provável inválido"
	result, err := repository.Fail(context.Background(), 102, conversationdomain.AssessmentPersistence{
		Analysis: analysis,
		Findings: []auditdomain.Finding{{Rule: "regra", Severity: auditdomain.SeverityHigh}},
		Score:    80,
	})
	if err != nil {
		t.Fatal(err)
	}
	if result.AnalysisID != 502 || result.FindingCount != 1 || !tx.committed || tx.rolledBack {
		t.Fatalf("transação FAILED incorreta: result=%#v commit=%v rollback=%v", result, tx.committed, tx.rolledBack)
	}
	if tx.queryArgs[2] != "FAILED" || tx.queryArgs[14] != "modelo" || tx.queryArgs[15] != conversationdomain.PromptVersion || tx.queryArgs[16] != "resposta original inválida" {
		t.Fatalf("análise FAILED não preservada: %#v", tx.queryArgs)
	}
	if tx.execArgs[1] != "FAILED" || tx.execArgs[2] != 80 {
		t.Fatalf("assessment FAILED/score incorreto: %#v", tx.execArgs)
	}
}

func TestAssessmentRepositoryRollsBackFailedAnalysisPersistence(t *testing.T) {
	tx := &assessmentTxStub{row: rowResult{err: errors.New("falha de inserção")}}
	repository := &AssessmentRepository{tx: assessmentTxStarterStub{tx: tx}}
	analysis := storedAnalysisForTest()
	analysis.Status = "FAILED"
	_, err := repository.Fail(context.Background(), 103, conversationdomain.AssessmentPersistence{Analysis: analysis, Score: 80})
	if err == nil || tx.committed || !tx.rolledBack {
		t.Fatalf("rollback FAILED não executado: err=%v commit=%v rollback=%v", err, tx.committed, tx.rolledBack)
	}
}

func TestAssessmentRepositoryCreatesHistoricalAssessmentsWithPostgreSQL(t *testing.T) {
	if strings.TrimSpace(os.Getenv("DATABASE_URL")) == "" {
		t.Skip("DATABASE_URL não configurada; teste de integração ignorado")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	bitrixDealID := time.Now().UnixNano()
	title := "Histórico de avaliações"
	dealID, err := NewDealRepository(pool).Upsert(ctx, dealdomain.Deal{BitrixDealID: bitrixDealID, Title: &title})
	if err != nil {
		t.Fatal(err)
	}
	defer func() {
		cleanupCtx, cleanupCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cleanupCancel()
		_, _ = pool.Exec(cleanupCtx, `DELETE FROM deals WHERE id = $1`, dealID)
	}()

	repository := NewAssessmentRepository(pool)
	for index := 0; index < 2; index++ {
		started := time.Now().UTC()
		assessmentID, err := repository.Start(ctx, dealID, started)
		if err != nil {
			t.Fatal(err)
		}
		analysis := storedAnalysisForTest()
		analysis.DealID = dealID
		analysis.StartedAt = started
		analysis.FinishedAt = started.Add(time.Second)
		finding := auditdomain.Finding{Rule: "historical_rule", Description: "descrição", Severity: auditdomain.SeverityMedium}
		if _, err := repository.Complete(ctx, assessmentID, conversationdomain.AssessmentPersistence{Analysis: analysis, Findings: []auditdomain.Finding{finding}, Score: 90}); err != nil {
			t.Fatal(err)
		}
		// Repetir o UPSERT na mesma avaliação deve atualizar, não duplicar.
		if _, err := NewFindingRepository(pool).Save(ctx, assessmentID, dealID, []auditdomain.Finding{finding}); err != nil {
			t.Fatal(err)
		}
	}

	var assessments, analyses, assessmentsWithOneAnalysis, findings int
	err = pool.QueryRow(ctx, `
SELECT
    COUNT(DISTINCT a.id),
    COUNT(c.id),
    COUNT(*) FILTER (WHERE analysis_count = 1)
FROM deal_assessments a
JOIN LATERAL (
    SELECT COUNT(*) AS analysis_count
    FROM conversation_analyses c2
    WHERE c2.assessment_id = a.id
) counts ON TRUE
LEFT JOIN conversation_analyses c ON c.assessment_id = a.id
WHERE a.deal_id = $1`, dealID).Scan(&assessments, &analyses, &assessmentsWithOneAnalysis)
	if err != nil {
		t.Fatal(err)
	}
	if assessments != 2 || analyses != 2 || assessmentsWithOneAnalysis != 2 {
		t.Fatalf("histórico inesperado: assessments=%d analyses=%d comUma=%d", assessments, analyses, assessmentsWithOneAnalysis)
	}
	if err := pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_findings WHERE deal_id = $1`, dealID).Scan(&findings); err != nil {
		t.Fatal(err)
	}
	if findings != 2 {
		t.Fatalf("achados históricos = %d, esperado 2 (um por avaliação)", findings)
	}
}
