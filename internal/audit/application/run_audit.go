package application

import (
	"context"
	"fmt"
	"time"

	"github.com/portfolio/auditor-ia/internal/audit/domain"
)

type RunAudit struct {
	Repo     domain.Repository
	Source   domain.Source
	Rules    domain.RuleEvaluator
	AI       domain.AIAnalyzer
	Now      func() time.Time
	NewID    func() string
	Progress func(AuditProgress)
}

type AuditProgress struct {
	Phase                                         string
	Collected, Processed, Total, Findings, Errors int
}

func (uc RunAudit) Execute(ctx context.Context, companyID string) (*domain.Audit, error) {
	audit, err := domain.Start(uc.NewID(), companyID, uc.Now())
	if err != nil {
		return nil, err
	}
	if err = uc.Repo.Save(ctx, audit); err != nil {
		return nil, fmt.Errorf("salvar auditoria: %w", err)
	}
	records, err := uc.Source.Records(ctx)
	if err != nil {
		return nil, fmt.Errorf("carregar dados: %w", err)
	}
	if uc.Progress != nil {
		uc.Progress(AuditProgress{Phase: "analisando regras", Collected: len(records), Total: len(records)})
	}
	findings, err := uc.Rules.Evaluate(records)
	if err != nil {
		return nil, fmt.Errorf("avaliar regras: %w", err)
	}
	audit.ProcessedRecords = len(records)
	if uc.Progress != nil {
		uc.Progress(AuditProgress{Phase: "consultando IA", Collected: len(records), Processed: len(records), Total: len(records), Findings: len(findings)})
	}
	preview := *audit
	preview.Findings = findings
	analysis := ""
	if uc.AI != nil {
		if result, aiErr := uc.AI.Analyze(ctx, &preview); aiErr == nil {
			analysis = result
		} else {
			analysis = "ERRO OLLAMA: " + aiErr.Error()
		}
	}
	audit.Finish(findings, analysis, uc.Now())
	if uc.Progress != nil {
		uc.Progress(AuditProgress{Phase: "concluída", Collected: len(records), Processed: len(records), Total: len(records), Findings: len(findings)})
	}
	if err = uc.Repo.Save(ctx, audit); err != nil {
		return nil, fmt.Errorf("finalizar auditoria: %w", err)
	}
	return audit, nil
}
