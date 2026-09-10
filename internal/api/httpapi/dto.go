package httpapi

import (
	"time"

	apidomain "github.com/portfolio/auditor-ia/internal/api/domain"
)

type envelope[T any] struct {
	Data T `json:"data"`
}
type listEnvelope[T any] struct {
	Data       []T           `json:"data"`
	Pagination paginationDTO `json:"pagination"`
}
type paginationDTO struct {
	Page       int `json:"page"`
	PageSize   int `json:"page_size"`
	Total      int `json:"total"`
	TotalPages int `json:"total_pages"`
}
type errorEnvelope struct {
	Error errorDTO `json:"error"`
}
type errorDTO struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type auditDTO struct {
	AssessmentID        int64    `json:"assessment_id"`
	BitrixDealID        int64    `json:"bitrix_deal_id"`
	DealTitle           string   `json:"deal_title"`
	Status              string   `json:"status"`
	Score               *float64 `json:"score"`
	FinalResult         string   `json:"final_result"`
	ResultSource        string   `json:"result_source"`
	Summary             string   `json:"summary"`
	FindingsCount       int      `json:"findings_count"`
	StartedAt           string   `json:"started_at"`
	FinishedAt          *string  `json:"finished_at"`
	CreatedAt           string   `json:"created_at"`
	TraceID             string   `json:"trace_id,omitempty"`
	ConversationStatus  string   `json:"conversation_status"`
	ConversationMessage string   `json:"conversation_message,omitempty"`
}

func toAuditDTO(a apidomain.Audit) auditDTO {
	out := auditDTO{AssessmentID: a.AssessmentID, BitrixDealID: a.BitrixDealID, DealTitle: a.DealTitle, Status: a.Status, Score: a.Score, FinalResult: a.FinalResult, ResultSource: a.ResultSource, Summary: a.Summary, FindingsCount: a.FindingsCount, StartedAt: a.StartedAt.Format(time.RFC3339), CreatedAt: a.CreatedAt.Format(time.RFC3339), TraceID: a.TraceID, ConversationStatus: a.ConversationStatus, ConversationMessage: a.ConversationMessage}
	if a.FinishedAt != nil {
		v := a.FinishedAt.Format(time.RFC3339)
		out.FinishedAt = &v
	}
	return out
}

type findingDTO struct {
	ID           int64  `json:"id"`
	AssessmentID int64  `json:"assessment_id"`
	BitrixDealID int64  `json:"bitrix_deal_id"`
	Rule         string `json:"rule"`
	Description  string `json:"description"`
	EntityType   string `json:"entity_type"`
	EntityID     string `json:"entity_id"`
	Severity     string `json:"severity"`
	CreatedAt    string `json:"created_at"`
}

func toFindingDTO(f apidomain.Finding) findingDTO {
	return findingDTO{f.ID, f.AssessmentID, f.BitrixDealID, f.Rule, f.Description, f.EntityType, f.EntityID, f.Severity, f.CreatedAt.Format(time.RFC3339)}
}

type analysisDTO struct {
	ID                    int64   `json:"id"`
	AssessmentID          int64   `json:"assessment_id"`
	BitrixDealID          int64   `json:"bitrix_deal_id"`
	Status                string  `json:"status"`
	ProbableResult        string  `json:"probable_result"`
	FinalResult           string  `json:"final_result"`
	ResultSource          string  `json:"result_source"`
	MainReason            string  `json:"main_reason"`
	CustomerObjections    string  `json:"customer_objections"`
	ServiceQuality        string  `json:"service_quality"`
	RecommendedAction     string  `json:"recommended_action"`
	Confidence            string  `json:"confidence"`
	PossibleReceipt       bool    `json:"possible_receipt"`
	MessageCount          int     `json:"message_count"`
	CollectedMessageCount int     `json:"collected_message_count"`
	PersistedMessageCount int     `json:"persisted_message_count"`
	AnalyzedMessageCount  int     `json:"analyzed_message_count"`
	Model                 string  `json:"model"`
	PromptVersion         string  `json:"prompt_version"`
	CreatedAt             string  `json:"created_at"`
	RawResponse           *string `json:"raw_response,omitempty"`
	AnalysisAvailable     bool    `json:"analysis_available"`
	AnalysisStatus        string  `json:"analysis_status"`
	ConversationStatus    string  `json:"conversation_status"`
}

func toAnalysisDTO(a apidomain.Analysis, raw bool) analysisDTO {
	out := analysisDTO{ID: a.ID, AssessmentID: a.AssessmentID, BitrixDealID: a.BitrixDealID, Status: a.Status, ProbableResult: a.ProbableResult, FinalResult: a.FinalResult, ResultSource: a.ResultSource, MainReason: a.MainReason, CustomerObjections: a.CustomerObjections, ServiceQuality: a.ServiceQuality, RecommendedAction: a.RecommendedAction, Confidence: a.Confidence, PossibleReceipt: a.PossibleReceipt, MessageCount: a.MessageCount, CollectedMessageCount: a.CollectedMessageCount, PersistedMessageCount: a.PersistedMessageCount, AnalyzedMessageCount: a.AnalyzedMessageCount, Model: a.Model, PromptVersion: a.PromptVersion, CreatedAt: a.CreatedAt.Format(time.RFC3339), AnalysisAvailable: a.ConversationStatus == "AVAILABLE", AnalysisStatus: a.UnavailableReason, ConversationStatus: a.ConversationStatus}
	if raw {
		out.RawResponse = &a.RawResponse
	}
	return out
}
