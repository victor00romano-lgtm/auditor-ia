package application

import (
	"context"
	"time"

	api "github.com/portfolio/auditor-ia/internal/api/domain"
)

type AuditProcessor struct {
	Runner  api.AuditRunner
	Timeout time.Duration
}

func (p AuditProcessor) Run(ctx context.Context, dealID int64) (int64, error) {
	if p.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, p.Timeout)
		defer cancel()
	}
	result, err := p.Runner.Run(ctx, dealID)
	return result.AssessmentID, err
}
