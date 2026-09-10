package postgres

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	auditdomain "github.com/portfolio/auditor-ia/internal/audit/domain"
)

type findingExecutor interface {
	Exec(context.Context, string, ...any) (pgconn.CommandTag, error)
}

type FindingRepository struct{ tx assessmentTxStarter }

func NewFindingRepository(pool *pgxpool.Pool) *FindingRepository {
	return &FindingRepository{tx: poolAssessmentTxStarter{pool: pool}}
}

func (r *FindingRepository) Save(ctx context.Context, assessmentID, dealID int64, findings []auditdomain.Finding) (int, error) {
	if r == nil || r.tx == nil {
		return 0, fmt.Errorf("salvar achados: repositório PostgreSQL não configurado")
	}
	tx, err := r.tx.Begin(ctx)
	if err != nil {
		return 0, safeConnectionError("iniciar transação de achados", err, os.Getenv("DATABASE_URL"))
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.Background())
		}
	}()
	count, err := saveFindings(ctx, tx, assessmentID, dealID, findings)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, safeConnectionError("confirmar achados", err, os.Getenv("DATABASE_URL"))
	}
	committed = true
	return count, nil
}

func saveFindings(ctx context.Context, executor findingExecutor, assessmentID, dealID int64, findings []auditdomain.Finding) (int, error) {
	for _, finding := range findings {
		severity, err := normalizedSeverity(finding.Severity)
		if err != nil {
			return 0, fmt.Errorf("salvar achado %q: %w", finding.Rule, err)
		}
		_, err = executor.Exec(ctx, `
INSERT INTO audit_findings (assessment_id, deal_id, rule_name, description, severity)
VALUES ($1, $2, $3, $4, $5)
ON CONFLICT (assessment_id, rule_name) WHERE assessment_id IS NOT NULL
DO UPDATE SET deal_id = EXCLUDED.deal_id, description = EXCLUDED.description, severity = EXCLUDED.severity`,
			assessmentID, dealID, finding.Rule, finding.Description, severity)
		if err != nil {
			return 0, safeConnectionError("inserir achado de auditoria", err, os.Getenv("DATABASE_URL"))
		}
	}
	return len(findings), nil
}

func normalizedSeverity(severity auditdomain.Severity) (string, error) {
	switch severity {
	case auditdomain.SeverityLow, auditdomain.SeverityMedium, auditdomain.SeverityHigh, auditdomain.SeverityCritical:
		return string(severity), nil
	default:
		return "", fmt.Errorf("severidade inválida: %q", severity)
	}
}

var _ auditdomain.FindingRepository = (*FindingRepository)(nil)
