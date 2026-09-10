package postgres

import (
	"context"
	"fmt"
	"os"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	conversationdomain "github.com/portfolio/auditor-ia/internal/conversation/domain"
)

type assessmentTx interface {
	QueryRow(context.Context, string, ...any) pgx.Row
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
	Commit(context.Context) error
	Rollback(context.Context) error
}

type assessmentTxStarter interface {
	Begin(context.Context) (assessmentTx, error)
}
type poolAssessmentTxStarter struct{ pool *pgxpool.Pool }

func (starter poolAssessmentTxStarter) Begin(ctx context.Context) (assessmentTx, error) {
	return starter.pool.Begin(ctx)
}

type AssessmentRepository struct {
	pool rowQuerier
	tx   assessmentTxStarter
}

func NewAssessmentRepository(pool *pgxpool.Pool) *AssessmentRepository {
	return &AssessmentRepository{pool: pool, tx: poolAssessmentTxStarter{pool: pool}}
}

func (r *AssessmentRepository) Start(ctx context.Context, dealID int64, startedAt time.Time) (int64, error) {
	return r.StartTrace(ctx, dealID, startedAt, "")
}

func (r *AssessmentRepository) StartTrace(ctx context.Context, dealID int64, startedAt time.Time, traceID string) (int64, error) {
	return r.StartTraceExecution(ctx, dealID, startedAt, traceID, "")
}

func (r *AssessmentRepository) StartTraceExecution(ctx context.Context, dealID int64, startedAt time.Time, traceID, executionKey string) (int64, error) {
	if r == nil || r.pool == nil {
		return 0, fmt.Errorf("criar avaliação: repositório PostgreSQL não configurado")
	}
	var id int64
	err := r.pool.QueryRow(ctx, `
INSERT INTO deal_assessments (deal_id, status, started_at, trace_id, execution_key)
VALUES ($1, 'PROCESSING', $2, NULLIF($3, ''), NULLIF($4, ''))
ON CONFLICT (execution_key) WHERE execution_key IS NOT NULL DO UPDATE SET execution_key=EXCLUDED.execution_key
RETURNING id`, dealID, startedAt, traceID, executionKey).Scan(&id)
	if err != nil {
		return 0, safeConnectionError("criar avaliação no PostgreSQL", err, os.Getenv("DATABASE_URL"))
	}
	return id, nil
}

func (r *AssessmentRepository) FindExecution(ctx context.Context, key string) (int64, string, error) {
	var id int64
	var status string
	err := r.pool.QueryRow(ctx, `SELECT id,status FROM deal_assessments WHERE execution_key=$1`, key).Scan(&id, &status)
	return id, status, err
}

func (r *AssessmentRepository) Complete(ctx context.Context, assessmentID int64, persistence conversationdomain.AssessmentPersistence) (conversationdomain.AssessmentPersistenceResult, error) {
	return r.finish(ctx, assessmentID, persistence, "COMPLETED")
}

func (r *AssessmentRepository) Fail(ctx context.Context, assessmentID int64, persistence conversationdomain.AssessmentPersistence) (conversationdomain.AssessmentPersistenceResult, error) {
	return r.finish(ctx, assessmentID, persistence, "FAILED")
}

func (r *AssessmentRepository) finish(ctx context.Context, assessmentID int64, persistence conversationdomain.AssessmentPersistence, status string) (conversationdomain.AssessmentPersistenceResult, error) {
	if r == nil || r.tx == nil {
		return conversationdomain.AssessmentPersistenceResult{}, fmt.Errorf("finalizar avaliação: repositório PostgreSQL não configurado")
	}
	tx, err := r.tx.Begin(ctx)
	if err != nil {
		return conversationdomain.AssessmentPersistenceResult{}, safeConnectionError("iniciar transação de avaliação", err, os.Getenv("DATABASE_URL"))
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.Background())
		}
	}()

	analysis := persistence.Analysis
	findingCount, err := saveFindings(ctx, tx, assessmentID, analysis.DealID, persistence.Findings)
	if err != nil {
		return conversationdomain.AssessmentPersistenceResult{}, err
	}
	analysisID, err := insertConversationAnalysis(ctx, tx, assessmentID, analysis, status)
	if err != nil {
		return conversationdomain.AssessmentPersistenceResult{}, safeConnectionError("inserir análise de conversa", err, os.Getenv("DATABASE_URL"))
	}

	commandTag, err := tx.Exec(ctx, `
UPDATE deal_assessments
SET status = $2, score = $3, final_result = $4, result_source = $5, summary = $6, finished_at = $7
WHERE id = $1`, assessmentID, status, persistence.Score, nullIfEmpty(analysis.FinalResult),
		nullIfEmpty(analysis.ResultSource), nullIfEmpty(analysis.Summary), analysis.FinishedAt)
	if err != nil {
		return conversationdomain.AssessmentPersistenceResult{}, safeConnectionError("atualizar avaliação", err, os.Getenv("DATABASE_URL"))
	}
	if commandTag.RowsAffected() != 1 {
		return conversationdomain.AssessmentPersistenceResult{}, fmt.Errorf("atualizar avaliação: assessment_id inexistente")
	}
	if err := tx.Commit(ctx); err != nil {
		return conversationdomain.AssessmentPersistenceResult{}, safeConnectionError("confirmar avaliação", err, os.Getenv("DATABASE_URL"))
	}
	committed = true
	return conversationdomain.AssessmentPersistenceResult{AnalysisID: analysisID, FindingCount: findingCount}, nil
}

var _ conversationdomain.AssessmentRepository = (*AssessmentRepository)(nil)
