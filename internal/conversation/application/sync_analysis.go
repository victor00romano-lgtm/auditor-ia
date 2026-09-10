package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	auditapp "github.com/portfolio/auditor-ia/internal/audit/application"
	auditdomain "github.com/portfolio/auditor-ia/internal/audit/domain"
	batchdomain "github.com/portfolio/auditor-ia/internal/batch/domain"
	conversationdomain "github.com/portfolio/auditor-ia/internal/conversation/domain"
	dealdomain "github.com/portfolio/auditor-ia/internal/deal/domain"
	"github.com/portfolio/auditor-ia/internal/observability"
	"go.opentelemetry.io/otel/attribute"
)

type SyncAnalysis struct {
	DealSource           dealdomain.DealSource
	DealRepository       dealdomain.DealRepository
	MessageRepository    conversationdomain.MessageRepository
	Analyze              Analyze
	AssessmentRepository conversationdomain.AssessmentRepository
	Rules                auditdomain.RuleEvaluator
	Now                  func() time.Time
	Logger               *slog.Logger
	Metrics              *observability.Metrics
}

type SyncAnalysisResult struct {
	DealID                int64
	AssessmentID          int64
	AnalysisID            int64
	ProbableResult        string
	FinalResult           string
	ResultSource          string
	Status                string
	FindingCount          int
	Score                 int
	CollectedMessageCount int
	PersistedMessageCount int
	AnalyzedMessageCount  int
}

type idempotentAssessmentRepository interface {
	StartTraceExecution(context.Context, int64, time.Time, string, string) (int64, error)
	FindExecution(context.Context, string) (int64, string, error)
}

func (uc SyncAnalysis) Execute(ctx context.Context, dealID string) (*SyncAnalysisResult, error) {
	operationStarted := time.Now()
	ctx, span := observability.Tracer().Start(ctx, "auditor.analysis")
	span.SetAttributes(attribute.String("deal.bitrix_id", dealID), attribute.String("operation", "analysis"))
	metricStatus := "completed"
	if uc.Metrics != nil {
		uc.Metrics.InProgress.WithLabelValues().Inc()
		defer uc.Metrics.InProgress.WithLabelValues().Dec()
	}
	defer func() {
		span.End()
		if uc.Metrics != nil {
			uc.Metrics.Audits.WithLabelValues(metricStatus).Inc()
			uc.Metrics.AuditDuration.WithLabelValues(metricStatus).Observe(time.Since(operationStarted).Seconds())
		}
	}()
	uc.log(ctx, slog.LevelInfo, "audit_started", "deal_id", dealID, "operation", "analysis", "status", "started")
	executionKey := batchdomain.ExecutionKey(ctx)
	if executionKey != "" {
		if repo, ok := uc.AssessmentRepository.(idempotentAssessmentRepository); ok {
			if existingID, status, findErr := repo.FindExecution(ctx, executionKey); findErr == nil && status == "COMPLETED" {
				return &SyncAnalysisResult{AssessmentID: existingID, Status: "COMPLETED"}, nil
			}
		}
	}
	dealCtx, dealSpan := observability.Tracer().Start(ctx, "bitrix.deal.get")
	deal, err := uc.DealSource.Get(dealCtx, dealID)
	dealSpan.End()
	if err != nil {
		metricStatus = "failed"
		span.RecordError(err)
		return nil, fmt.Errorf("validar negócio: %w", err)
	}
	postgresCtx, postgresSpan := observability.Tracer().Start(ctx, "postgres.deal.upsert")
	postgresStarted := time.Now()
	internalDealID, err := uc.DealRepository.Upsert(postgresCtx, deal)
	postgresSpan.End()
	uc.observePostgres("deal.upsert", postgresStarted, err)
	if err != nil {
		metricStatus = "failed"
		return nil, fmt.Errorf("sincronizar negócio: %w", err)
	}
	messagesCtx, messagesSpan := observability.Tracer().Start(ctx, "bitrix.messages.collect")
	conversation, err := uc.Analyze.CollectConversation(messagesCtx, dealID)
	messagesSpan.SetAttributes(attribute.Int("message.count", len(conversation.Messages)))
	messagesSpan.End()
	if err != nil {
		metricStatus = "failed"
		return nil, err
	}
	collectedCount := len(conversation.Messages)
	if uc.Metrics != nil {
		uc.Metrics.Messages.WithLabelValues("valid").Add(float64(collectedCount))
	}
	messageUpsert := conversationdomain.MessageUpsertResult{}
	if collectedCount > 0 {
		if uc.MessageRepository == nil {
			err = errors.New("repositório de mensagens não configurado")
		} else {
			messageUpsert, err = uc.MessageRepository.UpsertBatch(ctx, internalDealID, conversation.Messages)
		}
		if err != nil {
			metricStatus = "failed"
			persistenceErr := &conversationdomain.MessagePersistenceError{Operation: "messages.upsert", Cause: err}
			uc.log(ctx, slog.LevelError, "message_persistence_failed", "deal_id", dealID, "operation", "messages.upsert", "collected_message_count", collectedCount, "persisted_message_count", 0, "error", observability.SafeError(persistenceErr))
			return nil, persistenceErr
		}
	}
	uc.log(ctx, slog.LevelInfo, "messages_persisted", "deal_id", dealID, "operation", "messages.upsert", "collected_message_count", collectedCount, "persisted_message_count", messageUpsert.Processed)
	now := uc.Now
	if now == nil {
		now = time.Now
	}
	startedAt := now().UTC()
	assessmentCtx, assessmentSpan := observability.Tracer().Start(ctx, "postgres.assessment.start")
	assessmentStarted := time.Now()
	var assessmentID int64
	if idempotent, ok := uc.AssessmentRepository.(idempotentAssessmentRepository); ok && executionKey != "" {
		assessmentID, err = idempotent.StartTraceExecution(assessmentCtx, internalDealID, startedAt, observability.TraceID(ctx), executionKey)
	} else if traced, ok := uc.AssessmentRepository.(conversationdomain.TracedAssessmentRepository); ok {
		assessmentID, err = traced.StartTrace(assessmentCtx, internalDealID, startedAt, observability.TraceID(ctx))
	} else {
		assessmentID, err = uc.AssessmentRepository.Start(assessmentCtx, internalDealID, startedAt)
	}
	assessmentSpan.End()
	uc.observePostgres("assessment.start", assessmentStarted, err)
	if err != nil {
		metricStatus = "failed"
		return nil, fmt.Errorf("criar avaliação: %w", err)
	}
	span.SetAttributes(attribute.Int64("assessment.id", assessmentID), attribute.Int64("deal.id", internalDealID))
	uc.log(ctx, slog.LevelInfo, "assessment_created", "assessment_id", assessmentID, "deal_id", dealID, "status", "PROCESSING")
	if uc.Rules == nil {
		return nil, fmt.Errorf("executar auditoria determinística: motor de regras não configurado")
	}
	rulesStarted := time.Now()
	_, rulesSpan := observability.Tracer().Start(ctx, "rules.evaluate")
	findings, rulesErr := uc.Rules.Evaluate([]auditdomain.Record{auditapp.DealRecord(deal)})
	rulesSpan.SetAttributes(attribute.Int("finding.count", len(findings)))
	rulesSpan.End()
	if uc.Metrics != nil {
		uc.Metrics.RulesDuration.WithLabelValues(map[bool]string{true: "failed", false: "completed"}[rulesErr != nil]).Observe(time.Since(rulesStarted).Seconds())
		for _, finding := range findings {
			uc.Metrics.Findings.WithLabelValues(string(finding.Severity)).Inc()
		}
	}
	uc.log(ctx, slog.LevelInfo, "rules_evaluated", "assessment_id", assessmentID, "finding_count", len(findings), "duration_ms", observability.DurationMilliseconds(rulesStarted))
	score := auditdomain.CalculateScore(findings).Value()
	if rulesErr != nil {
		metricStatus = "failed"
		failure := conversationdomain.StoredConversationAnalysis{DealID: internalDealID, Status: "FAILED", ErrorMessage: "auditoria determinística não concluída", StartedAt: startedAt, FinishedAt: now().UTC(), Summary: "Auditoria determinística não concluída."}
		failureCtx, cancelFailure := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelFailure()
		_, _ = uc.AssessmentRepository.Fail(failureCtx, assessmentID, conversationdomain.AssessmentPersistence{Analysis: failure, Findings: findings, Score: score})
		return nil, fmt.Errorf("executar auditoria determinística: %w", rulesErr)
	}

	result, analysisErr := uc.Analyze.ExecuteCollected(ctx, dealID, conversation)
	if result != nil {
		result.PersistedMessageCount = messageUpsert.Processed
		if uc.Metrics != nil {
			uc.Metrics.MessageCollectionGap.Observe(float64(result.CollectedMessageCount - result.AnalyzedMessageCount))
		}
	}
	finishedAt := now().UTC()
	if analysisErr != nil {
		metricStatus = "failed"
		failureCategory := analysisFailureCategory(analysisErr)
		failureDetail := ""
		responseReceived := false
		failure := conversationdomain.StoredConversationAnalysis{
			DealID:                internalDealID,
			Status:                "FAILED",
			ErrorMessage:          "análise da conversa não concluída",
			StartedAt:             startedAt,
			FinishedAt:            finishedAt,
			Summary:               "Análise da conversa não concluída.",
			MessageCount:          collectedCount,
			CollectedMessageCount: collectedCount,
			PersistedMessageCount: messageUpsert.Processed,
			AnalyzedMessageCount:  min(collectedCount, conversationdomain.MaxAnalyzedMessages),
		}
		var invalid *conversationdomain.InvalidAnalysisResponseError
		if errors.As(analysisErr, &invalid) {
			failureDetail = invalid.Category
			responseReceived = true
			failure.RawResponse = invalid.RawResponse
			failure.Model = invalid.Model
			failure.PromptVersion = invalid.PromptVersion
			failure.MessageCount = invalid.MessageCount
			failure.PossibleReceipt = invalid.PossibleReceipt
			failure.ErrorMessage = observability.SafeError(invalid)
		}
		uc.log(ctx, slog.LevelError, "audit_failed",
			"assessment_id", assessmentID,
			"deal_id", dealID,
			"operation", "ollama.generate",
			"model", failure.Model,
			"prompt_version", failure.PromptVersion,
			"failure_category", failureCategory,
			"failure_detail", failureDetail,
			"response_received", responseReceived,
			"retry_count", 0,
			"duration_ms", observability.DurationMilliseconds(operationStarted),
			"error", observability.SafeError(analysisErr),
		)
		failureCtx, cancelFailure := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelFailure()
		if _, failErr := uc.AssessmentRepository.Fail(failureCtx, assessmentID, conversationdomain.AssessmentPersistence{Analysis: failure, Findings: findings, Score: score}); failErr != nil {
			return nil, fmt.Errorf("analisar negócio: %v; marcar avaliação como FAILED: %w", analysisErr, failErr)
		}
		return nil, fmt.Errorf("analisar negócio: %w", analysisErr)
	}

	analysis := result.StructuredAnalysis
	conversationUnavailable := result.AnalysisSource == "Análise de conversa indisponível"
	stored := conversationdomain.StoredConversationAnalysis{
		DealID:                internalDealID,
		Status:                "COMPLETED",
		ProbableResult:        analysis.ProbableResult,
		FinalResult:           result.FinalResult,
		ResultSource:          result.ResultSource,
		MainReason:            analysis.MainReason,
		CustomerObjections:    analysis.CustomerObjections,
		ServiceQuality:        analysis.ServiceQuality,
		RecommendedAction:     result.RecommendedAction,
		Confidence:            analysis.Confidence,
		Observation:           result.Observation,
		PossibleReceipt:       analysis.PossibleReceipt,
		MessageCount:          result.MessageCount,
		CollectedMessageCount: result.CollectedMessageCount,
		PersistedMessageCount: result.PersistedMessageCount,
		AnalyzedMessageCount:  result.AnalyzedMessageCount,
		Model:                 analysis.Model,
		PromptVersion:         analysis.PromptVersion,
		RawResponse:           analysis.RawResponse,
		StartedAt:             startedAt,
		FinishedAt:            finishedAt,
		Summary:               analysis.MainReason,
		ConversationStatus:    result.ConversationStatus,
	}
	if conversationUnavailable {
		stored.ErrorMessage = result.Observation
		stored.Summary = "Análise de conversa indisponível"
		reason := conversationUnavailableReason(result.ConversationStatus)
		if uc.Metrics != nil {
			uc.Metrics.ConversationUnavailable.WithLabelValues(reason).Inc()
		}
		uc.log(ctx, slog.LevelWarn, "conversation_unavailable",
			"deal_id", dealID,
			"session_id", result.SessionID,
			"error_code", result.ConversationError,
		)
	}
	completeCtx, completeSpan := observability.Tracer().Start(ctx, "postgres.assessment.complete")
	completeStarted := time.Now()
	persisted, err := uc.AssessmentRepository.Complete(completeCtx, assessmentID, conversationdomain.AssessmentPersistence{Analysis: stored, Findings: findings, Score: score})
	completeSpan.End()
	uc.observePostgres("assessment.complete", completeStarted, err)
	if err != nil {
		metricStatus = "failed"
		failure := stored
		failure.Status = "FAILED"
		failure.ErrorMessage = "falha ao persistir análise concluída"
		failure.Summary = "Análise concluída, mas não persistida."
		failureCtx, cancelFailure := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancelFailure()
		if _, failErr := uc.AssessmentRepository.Fail(failureCtx, assessmentID, conversationdomain.AssessmentPersistence{Analysis: failure, Findings: findings, Score: score}); failErr != nil {
			return nil, fmt.Errorf("persistir análise: %v; marcar avaliação como FAILED: %w", err, failErr)
		}
		return nil, fmt.Errorf("persistir análise: %w", err)
	}
	uc.log(ctx, slog.LevelInfo, "assessment_completed", "assessment_id", assessmentID, "deal_id", dealID, "finding_count", persisted.FindingCount, "score", score, "status", "COMPLETED", "duration_ms", observability.DurationMilliseconds(operationStarted))
	return &SyncAnalysisResult{
		DealID:                deal.BitrixDealID,
		AssessmentID:          assessmentID,
		AnalysisID:            persisted.AnalysisID,
		ProbableResult:        analysis.ProbableResult,
		FinalResult:           result.FinalResult,
		ResultSource:          result.ResultSource,
		Status:                "COMPLETED",
		FindingCount:          persisted.FindingCount,
		Score:                 score,
		CollectedMessageCount: result.CollectedMessageCount,
		PersistedMessageCount: result.PersistedMessageCount,
		AnalyzedMessageCount:  result.AnalyzedMessageCount,
	}, nil
}

func conversationUnavailableReason(status conversationdomain.ConversationStatus) string {
	switch status {
	case conversationdomain.ConversationAccessDenied:
		return "access_denied"
	case conversationdomain.ConversationEmpty:
		return "empty"
	default:
		return "not_found"
	}
}

func analysisFailureCategory(err error) string {
	var invalid *conversationdomain.InvalidAnalysisResponseError
	if errors.As(err, &invalid) {
		return "invalid_response"
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return "timeout"
	}
	if errors.Is(err, context.Canceled) {
		return "canceled"
	}
	return "transport"
}

func (uc SyncAnalysis) log(ctx context.Context, level slog.Level, event string, attrs ...any) {
	if uc.Logger != nil {
		uc.Logger.Log(ctx, level, event, observability.LogAttrs(ctx, attrs...)...)
	}
}

func (uc SyncAnalysis) observePostgres(operation string, started time.Time, err error) {
	if uc.Metrics == nil {
		return
	}
	status := map[bool]string{true: "error", false: "success"}[err != nil]
	uc.Metrics.PostgresDuration.WithLabelValues(operation, status).Observe(time.Since(started).Seconds())
}
