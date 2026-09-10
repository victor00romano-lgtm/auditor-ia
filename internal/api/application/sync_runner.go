package application

import (
	"context"
	"strconv"

	apidomain "github.com/portfolio/auditor-ia/internal/api/domain"
	conversationapp "github.com/portfolio/auditor-ia/internal/conversation/application"
)

type SyncRunner struct{ Sync conversationapp.SyncAnalysis }

func (r SyncRunner) Run(ctx context.Context, bitrixDealID int64) (apidomain.AuditExecution, error) {
	result, err := r.Sync.Execute(ctx, strconv.FormatInt(bitrixDealID, 10))
	if err != nil {
		return apidomain.AuditExecution{}, err
	}
	return apidomain.AuditExecution{AssessmentID: result.AssessmentID}, nil
}

var _ apidomain.AuditRunner = SyncRunner{}
