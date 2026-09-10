package postgres

import (
	"context"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	conversationdomain "github.com/portfolio/auditor-ia/internal/conversation/domain"
)

type conversationAnalysisQueryRow interface {
	QueryRow(context.Context, string, ...any) pgx.Row
}

type ConversationAnalysisRepository struct{ tx assessmentTxStarter }

func NewConversationAnalysisRepository(pool *pgxpool.Pool) *ConversationAnalysisRepository {
	return &ConversationAnalysisRepository{tx: poolAssessmentTxStarter{pool: pool}}
}

func (r *ConversationAnalysisRepository) Save(ctx context.Context, assessmentID int64, analysis conversationdomain.StoredConversationAnalysis) (int64, error) {
	if r == nil || r.tx == nil {
		return 0, fmt.Errorf("salvar análise: repositório PostgreSQL não configurado")
	}
	tx, err := r.tx.Begin(ctx)
	if err != nil {
		return 0, safeConnectionError("iniciar transação de análise", err, os.Getenv("DATABASE_URL"))
	}
	committed := false
	defer func() {
		if !committed {
			_ = tx.Rollback(context.Background())
		}
	}()
	id, err := insertConversationAnalysis(ctx, tx, assessmentID, analysis, analysis.Status)
	if err != nil {
		return 0, err
	}
	if err := tx.Commit(ctx); err != nil {
		return 0, safeConnectionError("confirmar análise", err, os.Getenv("DATABASE_URL"))
	}
	committed = true
	return id, nil
}

func insertConversationAnalysis(ctx context.Context, query conversationAnalysisQueryRow, assessmentID int64, analysis conversationdomain.StoredConversationAnalysis, status string) (int64, error) {
	var analysisID int64
	conversationStatus := analysis.ConversationStatus
	if conversationStatus == "" {
		conversationStatus = conversationdomain.ConversationAvailable
	}
	err := query.QueryRow(ctx, `
INSERT INTO conversation_analyses (
    assessment_id, deal_id, status, probable_result, final_result,
    result_source, main_reason, customer_objections, service_quality,
    recommended_action, confidence, observation, possible_receipt,
    message_count, model, prompt_version, raw_response, error_message,
    started_at, finished_at, conversation_status,
    collected_message_count, persisted_message_count, analyzed_message_count
) VALUES (
    $1, $2, $3, $4, $5, $6, $7, $8, $9, $10,
    $11, $12, $13, $14, $15, $16, $17, $18, $19, $20, $21, $22, $23, $24
)
RETURNING id`, assessmentID, analysis.DealID, status, nullIfEmpty(analysis.ProbableResult),
		nullIfEmpty(analysis.FinalResult), nullIfEmpty(analysis.ResultSource), nullIfEmpty(analysis.MainReason),
		nullIfEmpty(analysis.CustomerObjections), nullIfEmpty(analysis.ServiceQuality), nullIfEmpty(analysis.RecommendedAction),
		nullIfEmpty(analysis.Confidence), nullIfEmpty(analysis.Observation), analysis.PossibleReceipt, analysis.MessageCount,
		nullIfEmpty(analysis.Model), nullIfEmpty(analysis.PromptVersion), nullIfEmpty(analysis.RawResponse),
		nullIfEmpty(analysis.ErrorMessage), analysis.StartedAt, analysis.FinishedAt,
		string(conversationStatus), analysis.CollectedMessageCount, analysis.PersistedMessageCount, analysis.AnalyzedMessageCount).Scan(&analysisID)
	if err != nil {
		return 0, safeConnectionError("inserir análise de conversa", err, os.Getenv("DATABASE_URL"))
	}
	return analysisID, nil
}

var _ conversationdomain.ConversationAnalysisRepository = (*ConversationAnalysisRepository)(nil)
