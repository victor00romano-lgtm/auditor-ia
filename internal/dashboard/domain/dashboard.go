package domain

import (
	"context"
	"errors"
	"time"
)

var ErrNotFound = errors.New("negócio não encontrado")

type Summary struct {
	DealsSynced, DealsAnalyzed, Completed, Failed int
	AverageScore                                  float64
	Results, FindingsBySeverity, FindingsByRule   map[string]int
	PossibleReceipts                              int
}

type FindingFilter struct {
	Severity, Rule string
	BitrixDealID   int64
	Limit, Offset  int
}

type Finding struct {
	ID, AssessmentID, DealID, BitrixDealID int64
	Severity, Rule, Description, Title     string
}

type FindingPage struct {
	Items         []Finding
	Total         int
	Limit, Offset int
}

type DealDetail struct {
	BitrixDealID                                                                     int64
	Title, StageID, StageSemanticID, Currency                                        string
	Closed                                                                           bool
	Amount                                                                           *string
	SyncedAt                                                                         time.Time
	AssessmentID                                                                     int64
	HasAnalysis                                                                      bool
	AssessmentStatus                                                                 string
	Score                                                                            *float64
	MessageCount                                                                     int
	PossibleReceipt                                                                  bool
	ProbableResult, FinalResult, ResultSource, RecommendedAction                     string
	AnalysisStatusMessage                                                            string
	ConversationStatus                                                               string
	AssignedByID                                                                     *int64
	UpdatedAtBitrix, CreatedAtBitrix                                                 *time.Time
	MainReason, CustomerObjections, ServiceQuality, Confidence, Observation, TraceID string
	Findings                                                                         []Finding
}

type ConversationAnalysisDetail struct {
	ID, AssessmentID, BitrixDealID                        int64
	Title, Status, Model, PromptVersion                   string
	MessageCount                                          int
	PossibleReceipt                                       bool
	ProbableResult, FinalResult, ResultSource, MainReason string
	CustomerObjections, ServiceQuality, RecommendedAction string
	Confidence, Observation, RawResponse                  string
	StatusMessage                                         string
	ConversationStatus                                    string
}

type Repository interface {
	GetSummary(context.Context) (Summary, error)
	ListFindings(context.Context, FindingFilter) (FindingPage, error)
	ListRules(context.Context) ([]string, error)
	GetDealDetail(context.Context, int64) (DealDetail, error)
	GetLatestConversationAnalysis(context.Context, int64) (ConversationAnalysisDetail, error)
}
