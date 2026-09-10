package domain

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("recurso não encontrado")

type Audit struct {
	AssessmentID, BitrixDealID              int64
	DealTitle, Status                       string
	Score                                   *float64
	FinalResult, ResultSource, Summary      string
	TraceID                                 string
	ConversationStatus, ConversationMessage string
	FindingsCount                           int
	StartedAt                               time.Time
	FinishedAt                              *time.Time
	CreatedAt                               time.Time
}

type AuditFilter struct {
	Page, PageSize      int
	BitrixDealID        int64
	Status, FinalResult string
	DateFrom, DateTo    *time.Time
}

type Finding struct {
	ID, AssessmentID, BitrixDealID                    int64
	Rule, Description, EntityType, EntityID, Severity string
	CreatedAt                                         time.Time
}

type FindingFilter struct {
	Page, PageSize             int
	AssessmentID, BitrixDealID int64
	Severity, Rule             string
}

type Analysis struct {
	ID, AssessmentID, BitrixDealID                                     int64
	Status, ProbableResult, FinalResult, ResultSource                  string
	MainReason, CustomerObjections, ServiceQuality                     string
	RecommendedAction, Confidence                                      string
	PossibleReceipt                                                    bool
	MessageCount                                                       int
	CollectedMessageCount, PersistedMessageCount, AnalyzedMessageCount int
	Model, PromptVersion, RawResponse                                  string
	UnavailableReason                                                  string
	ConversationStatus                                                 string
	CreatedAt                                                          time.Time
}

type Page[T any] struct {
	Items                 []T
	Total, Page, PageSize int
}

type ReportData struct {
	Audit    Audit
	Findings []Finding
	Analysis *Analysis
}

type Repository interface {
	GetAudit(context.Context, int64) (Audit, error)
	ListAudits(context.Context, AuditFilter) (Page[Audit], error)
	ListFindings(context.Context, FindingFilter) (Page[Finding], error)
	GetAnalysis(context.Context, int64, int64) (Analysis, error)
	GetReportData(context.Context, int64) (ReportData, error)
}

type AuditExecution struct{ AssessmentID int64 }
type AuditRunner interface {
	Run(context.Context, int64) (AuditExecution, error)
}

type ReportRenderer interface {
	Render(context.Context, ReportData, string) ([]byte, error)
}
