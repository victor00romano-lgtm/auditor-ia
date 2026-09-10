package domain

import (
	"context"
	"time"

	auditdomain "github.com/portfolio/auditor-ia/internal/audit/domain"
)

type StoredConversationAnalysis struct {
	DealID                int64
	Status                string
	ProbableResult        string
	FinalResult           string
	ResultSource          string
	MainReason            string
	CustomerObjections    string
	ServiceQuality        string
	RecommendedAction     string
	Confidence            string
	Observation           string
	PossibleReceipt       bool
	MessageCount          int
	CollectedMessageCount int
	PersistedMessageCount int
	AnalyzedMessageCount  int
	Model                 string
	PromptVersion         string
	RawResponse           string
	ErrorMessage          string
	StartedAt             time.Time
	FinishedAt            time.Time
	Summary               string
	ConversationStatus    ConversationStatus
}

type AssessmentPersistence struct {
	Analysis StoredConversationAnalysis
	Findings []auditdomain.Finding
	Score    int
}

type AssessmentPersistenceResult struct {
	AnalysisID   int64
	FindingCount int
}

type AssessmentRepository interface {
	Start(context.Context, int64, time.Time) (int64, error)
	Complete(context.Context, int64, AssessmentPersistence) (AssessmentPersistenceResult, error)
	Fail(context.Context, int64, AssessmentPersistence) (AssessmentPersistenceResult, error)
}

// TracedAssessmentRepository is an optional extension for correlation metadata.
type TracedAssessmentRepository interface {
	StartTrace(context.Context, int64, time.Time, string) (int64, error)
}

type ConversationAnalysisRepository interface {
	Save(context.Context, int64, StoredConversationAnalysis) (int64, error)
}
