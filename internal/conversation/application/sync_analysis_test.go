package application

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"testing"
	"time"

	auditdomain "github.com/portfolio/auditor-ia/internal/audit/domain"
	batchdomain "github.com/portfolio/auditor-ia/internal/batch/domain"
	conversationdomain "github.com/portfolio/auditor-ia/internal/conversation/domain"
	dealdomain "github.com/portfolio/auditor-ia/internal/deal/domain"
	"github.com/portfolio/auditor-ia/internal/observability"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

type syncDealSourceStub struct{}

func (syncDealSourceStub) Get(context.Context, string) (dealdomain.Deal, error) {
	return dealdomain.Deal{BitrixDealID: 8620}, nil
}

func TestSyncAnalysisCompletesAndPreservesFindingsOnAccessDenied(t *testing.T) {
	repository := &assessmentRepositoryStub{}
	metrics := observability.NewMetrics()
	var logs bytes.Buffer
	useCase := SyncAnalysis{
		DealSource: syncDealSourceStub{}, DealRepository: syncDealRepositoryStub{},
		MessageRepository: &messageRepositoryStub{},
		Analyze: Analyze{
			Source: typedConversationSourceStub{conversation: conversationdomain.Conversation{
				Status: conversationdomain.ConversationAccessDenied, SessionID: "14", ErrorCode: "ACCESS_DENIED",
			}},
			FormSource: formSourceStub{}, StatusSource: statusSourceStub{status: conversationdomain.DealStatus{SemanticID: "P"}}, Analyzer: panicAnalyzerStub{},
		},
		AssessmentRepository: repository,
		Rules:                ruleEvaluatorStub{findings: []auditdomain.Finding{{Rule: "opportunity_without_value", Severity: auditdomain.SeverityHigh}}},
		Now:                  func() time.Time { return time.Date(2026, 8, 12, 12, 0, 0, 0, time.UTC) },
		Metrics:              metrics, Logger: slog.New(slog.NewJSONHandler(&logs, nil)),
	}
	result, err := useCase.Execute(context.Background(), "52")
	if err != nil || result.Status != "COMPLETED" || result.Score != 80 || len(repository.completed) != 1 || len(repository.failed) != 0 {
		t.Fatalf("auditoria determinística não concluída: result=%#v err=%v completed=%#v", result, err, repository.completed)
	}
	persisted := repository.completed[0]
	if len(persisted.Findings) != 1 || persisted.Analysis.ConversationStatus != conversationdomain.ConversationAccessDenied || persisted.Analysis.ErrorMessage != conversationAccessDeniedMessage {
		t.Fatalf("estado ACCESS_DENIED não persistido: %#v", persisted)
	}
	if got := testutil.ToFloat64(metrics.ConversationUnavailable.WithLabelValues("access_denied")); got != 1 {
		t.Fatalf("métrica access_denied=%v", got)
	}
	if !strings.Contains(logs.String(), `"session_id":"14"`) || !strings.Contains(logs.String(), `"error_code":"ACCESS_DENIED"`) || strings.Contains(logs.String(), "webhook") || strings.Contains(logs.String(), "token") {
		t.Fatalf("log inseguro ou incompleto: %s", logs.String())
	}
}

func TestSyncAnalysisReusesCompletedAssessmentByExecutionKey(t *testing.T) {
	repository := &completedExecutionRepositoryStub{id: 77, status: "COMPLETED"}
	useCase := SyncAnalysis{AssessmentRepository: repository}
	ctx := batchdomain.WithExecutionKey(context.Background(), "batch-item-7")
	result, err := useCase.Execute(ctx, "8620")
	if err != nil {
		t.Fatal(err)
	}
	if result.AssessmentID != 77 || result.Status != "COMPLETED" {
		t.Fatalf("resultado=%#v", result)
	}
	if repository.key != "batch-item-7" || repository.started {
		t.Fatalf("key=%q started=%t", repository.key, repository.started)
	}
}

func TestSyncAnalysisPersistsSupportOverrideAndPreservesProbableResult(t *testing.T) {
	repository := &assessmentRepositoryStub{}
	useCase := SyncAnalysis{
		DealSource: syncDealSourceStub{}, DealRepository: syncDealRepositoryStub{},
		MessageRepository: &messageRepositoryStub{},
		Analyze: Analyze{
			Source:        messageSourceStub{},
			StatusSource:  statusSourceStub{status: conversationdomain.DealStatus{SemanticID: "P", AssignedByID: 3066}},
			AssigneeRoles: NewConfiguredAssigneeRoles([]int64{3066}),
			Analyzer:      syncAnalyzerStub{analysis: completeTypedAnalysis("em negociação")},
		},
		AssessmentRepository: repository,
		Rules:                ruleEvaluatorStub{},
	}
	result, err := useCase.Execute(context.Background(), "34710")
	if err != nil {
		t.Fatal(err)
	}
	if result.FinalResult != "pós-venda/suporte" || result.ResultSource != "responsável de suporte no Bitrix" || result.ProbableResult != "em negociação" {
		t.Fatalf("resultado=%#v", result)
	}
	persisted := repository.completed[0].Analysis
	if persisted.FinalResult != result.FinalResult || persisted.ResultSource != result.ResultSource || persisted.ProbableResult != "em negociação" {
		t.Fatalf("persistência=%#v", persisted)
	}
}

func TestReprocessingCreatesNewAssessmentAndPreservesHistory(t *testing.T) {
	repository := &assessmentRepositoryStub{}
	useCase := syncUseCase(repository, syncAnalyzerStub{analysis: completeTypedAnalysis("em negociação")}, "P")
	first, err := useCase.Execute(context.Background(), "34720")
	if err != nil {
		t.Fatal(err)
	}
	second, err := useCase.Execute(context.Background(), "34720")
	if err != nil {
		t.Fatal(err)
	}
	if first.AssessmentID == second.AssessmentID || len(repository.started) != 2 || len(repository.completed) != 2 {
		t.Fatalf("histórico sobrescrito: first=%d second=%d started=%d completed=%d", first.AssessmentID, second.AssessmentID, len(repository.started), len(repository.completed))
	}
}

type syncDealRepositoryStub struct{}

func (syncDealRepositoryStub) Upsert(context.Context, dealdomain.Deal) (int64, error) {
	return 31, nil
}

type messageRepositoryStub struct {
	messages []conversationdomain.Message
	err      error
	called   int
}

func (stub *messageRepositoryStub) UpsertBatch(_ context.Context, _ int64, messages []conversationdomain.Message) (conversationdomain.MessageUpsertResult, error) {
	stub.called++
	stub.messages = append([]conversationdomain.Message(nil), messages...)
	if stub.err != nil {
		return conversationdomain.MessageUpsertResult{}, stub.err
	}
	return conversationdomain.MessageUpsertResult{Processed: len(messages)}, nil
}

type syncAnalyzerStub struct {
	analysis conversationdomain.ConversationAnalysis
	err      error
	onCall   func()
}

func (stub syncAnalyzerStub) Analyze(context.Context, string, []conversationdomain.Message, []conversationdomain.CRMForm) (conversationdomain.ConversationAnalysis, error) {
	if stub.onCall != nil {
		stub.onCall()
	}
	return stub.analysis, stub.err
}

type assessmentRepositoryStub struct {
	nextAssessmentID int64
	nextAnalysisID   int64
	started          []int64
	completed        []conversationdomain.AssessmentPersistence
	failed           []conversationdomain.AssessmentPersistence
	transactionOpen  bool
	failContextErr   error
}

type completedExecutionRepositoryStub struct {
	assessmentRepositoryStub
	id      int64
	status  string
	key     string
	started bool
}

func (stub *completedExecutionRepositoryStub) StartTraceExecution(context.Context, int64, time.Time, string, string) (int64, error) {
	stub.started = true
	return 0, nil
}

func (stub *completedExecutionRepositoryStub) FindExecution(_ context.Context, key string) (int64, string, error) {
	stub.key = key
	return stub.id, stub.status, nil
}

func (stub *assessmentRepositoryStub) Start(_ context.Context, dealID int64, _ time.Time) (int64, error) {
	stub.nextAssessmentID++
	stub.started = append(stub.started, dealID)
	return stub.nextAssessmentID, nil
}

func (stub *assessmentRepositoryStub) Complete(_ context.Context, _ int64, persistence conversationdomain.AssessmentPersistence) (conversationdomain.AssessmentPersistenceResult, error) {
	stub.transactionOpen = true
	defer func() { stub.transactionOpen = false }()
	stub.nextAnalysisID++
	stub.completed = append(stub.completed, persistence)
	return conversationdomain.AssessmentPersistenceResult{AnalysisID: stub.nextAnalysisID, FindingCount: len(persistence.Findings)}, nil
}

func (stub *assessmentRepositoryStub) Fail(ctx context.Context, _ int64, persistence conversationdomain.AssessmentPersistence) (conversationdomain.AssessmentPersistenceResult, error) {
	stub.failContextErr = ctx.Err()
	stub.transactionOpen = true
	defer func() { stub.transactionOpen = false }()
	stub.nextAnalysisID++
	stub.failed = append(stub.failed, persistence)
	return conversationdomain.AssessmentPersistenceResult{AnalysisID: stub.nextAnalysisID, FindingCount: len(persistence.Findings)}, nil
}

func TestAnalysisFailureCategory(t *testing.T) {
	tests := []struct {
		name string
		err  error
		want string
	}{
		{"timeout", context.DeadlineExceeded, "timeout"},
		{"cancelamento", context.Canceled, "canceled"},
		{"transporte", errors.New("conexão recusada"), "transport"},
		{"resposta inválida", &conversationdomain.InvalidAnalysisResponseError{Cause: errors.New("inválida")}, "invalid_response"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if got := analysisFailureCategory(test.err); got != test.want {
				t.Fatalf("categoria=%q; esperada=%q", got, test.want)
			}
		})
	}
}

func TestCanceledAnalysisIsPersistedFailedWithIndependentContext(t *testing.T) {
	repository := &assessmentRepositoryStub{}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := syncUseCase(repository, syncAnalyzerStub{err: context.Canceled}, "P").Execute(ctx, "8620")
	if err == nil || len(repository.failed) != 1 {
		t.Fatalf("cancelamento não persistido: err=%v failed=%d", err, len(repository.failed))
	}
	if repository.failContextErr != nil || repository.failed[0].Analysis.Status != "FAILED" {
		t.Fatalf("persistência usou contexto cancelado: ctx=%v analysis=%#v", repository.failContextErr, repository.failed[0].Analysis)
	}
}

func TestTimedOutAnalysisIsPersistedFailed(t *testing.T) {
	repository := &assessmentRepositoryStub{}
	_, err := syncUseCase(repository, syncAnalyzerStub{err: context.DeadlineExceeded}, "P").Execute(context.Background(), "8620")
	if err == nil || len(repository.failed) != 1 || repository.failed[0].Analysis.Status != "FAILED" {
		t.Fatalf("timeout não persistido como FAILED: err=%v failed=%#v", err, repository.failed)
	}
}

type ruleEvaluatorStub struct{ findings []auditdomain.Finding }

func (stub ruleEvaluatorStub) Evaluate([]auditdomain.Record) ([]auditdomain.Finding, error) {
	return stub.findings, nil
}

func completeTypedAnalysis(result string) conversationdomain.ConversationAnalysis {
	analysis := conversationdomain.ConversationAnalysis{
		ProbableResult:     result,
		MainReason:         "motivo",
		CustomerObjections: "nenhuma",
		ServiceQuality:     "boa",
		RecommendedAction:  "acompanhar",
		Confidence:         "alta",
		RawResponse:        "resposta bruta",
		Model:              "modelo-teste",
		PromptVersion:      conversationdomain.PromptVersion,
	}
	analysis.NormalizedResponse = analysis.Format()
	return analysis
}

func syncUseCase(repository *assessmentRepositoryStub, analyzer conversationdomain.Analyzer, semanticID string) SyncAnalysis {
	return SyncAnalysis{
		DealSource:        syncDealSourceStub{},
		DealRepository:    syncDealRepositoryStub{},
		MessageRepository: &messageRepositoryStub{},
		Analyze: Analyze{
			Source:       messageSourceStub{},
			StatusSource: statusSourceStub{status: conversationdomain.DealStatus{SemanticID: semanticID}},
			Analyzer:     analyzer,
		},
		AssessmentRepository: repository,
		Rules:                ruleEvaluatorStub{findings: []auditdomain.Finding{{Rule: "teste", Severity: auditdomain.SeverityHigh}}},
		Now:                  func() time.Time { return time.Date(2026, 8, 7, 12, 0, 0, 0, time.UTC) },
	}
}

func TestSyncAnalysisPersistsAllMessagesBeforeAnalysisAndLimitsAnalyzedCount(t *testing.T) {
	messages := make([]conversationdomain.Message, 80)
	for index := range messages {
		messages[index] = conversationdomain.Message{ID: fmt.Sprint(index + 1), SessionID: fmt.Sprintf("session-%d", index%2+1), Role: "CLIENTE", Text: "mensagem"}
	}
	messageRepo := &messageRepositoryStub{}
	analyzerCalled := false
	useCase := syncUseCase(&assessmentRepositoryStub{}, syncAnalyzerStub{analysis: completeTypedAnalysis("em negociação"), onCall: func() {
		analyzerCalled = true
		if len(messageRepo.messages) != 80 {
			t.Fatalf("Ollama iniciado antes da persistência: persistidas=%d", len(messageRepo.messages))
		}
	}}, "P")
	useCase.Analyze.Source = messageSourceValueStub{messages: messages}
	useCase.MessageRepository = messageRepo
	result, err := useCase.Execute(context.Background(), "8620")
	if err != nil {
		t.Fatal(err)
	}
	if !analyzerCalled || len(messageRepo.messages) != 80 || result.CollectedMessageCount != 80 || result.PersistedMessageCount != 80 || result.AnalyzedMessageCount != 50 {
		t.Fatalf("contadores incorretos: result=%#v persistidas=%d", result, len(messageRepo.messages))
	}
}

func TestSyncAnalysisMessagePersistenceFailurePreventsOllamaAndCompleted(t *testing.T) {
	repository := &assessmentRepositoryStub{}
	analyzerCalled := false
	useCase := syncUseCase(repository, syncAnalyzerStub{onCall: func() { analyzerCalled = true }}, "P")
	useCase.MessageRepository = &messageRepositoryStub{err: errors.New("falha simulada")}
	_, err := useCase.Execute(context.Background(), "8620")
	var persistenceErr *conversationdomain.MessagePersistenceError
	if !errors.As(err, &persistenceErr) || analyzerCalled || len(repository.completed) != 0 {
		t.Fatalf("falha obrigatória incorreta: err=%v analyzer=%t completed=%d", err, analyzerCalled, len(repository.completed))
	}
}

func TestSyncAnalysisPersistsCompletedAnalysisWithoutTransactionDuringOllama(t *testing.T) {
	repository := &assessmentRepositoryStub{nextAssessmentID: 100, nextAnalysisID: 200}
	analyzer := syncAnalyzerStub{analysis: completeTypedAnalysis("em negociação")}
	analyzer.onCall = func() {
		if repository.transactionOpen {
			t.Fatal("transação permaneceu aberta durante o Ollama")
		}
		time.Sleep(10 * time.Millisecond) // simula inferência lenta fora da transação
	}
	result, err := syncUseCase(repository, analyzer, "P").Execute(context.Background(), "8620")
	if err != nil {
		t.Fatal(err)
	}
	if result.AssessmentID != 101 || result.AnalysisID != 201 || result.Status != "COMPLETED" || len(repository.completed) != 1 {
		t.Fatalf("resultado inesperado: %#v completed=%#v", result, repository.completed)
	}
	stored := repository.completed[0].Analysis
	if stored.DealID != 31 || stored.ProbableResult != "em negociação" || stored.FinalResult != "em negociação" || stored.PromptVersion != conversationdomain.PromptVersion || stored.RawResponse != "resposta bruta" {
		t.Fatalf("análise persistida incorretamente: %#v", stored)
	}
}

func TestSyncAnalysisMarksAssessmentFailedWhenOllamaFails(t *testing.T) {
	repository := &assessmentRepositoryStub{}
	_, err := syncUseCase(repository, syncAnalyzerStub{err: errors.New("Ollama indisponível")}, "P").Execute(context.Background(), "8620")
	if err == nil || len(repository.failed) != 1 || len(repository.completed) != 0 {
		t.Fatalf("falha não persistida: err=%v failed=%#v", err, repository.failed)
	}
	if repository.failed[0].Analysis.Status != "FAILED" || repository.failed[0].Analysis.ErrorMessage != "análise da conversa não concluída" || repository.failed[0].Score != 80 || len(repository.failed[0].Findings) != 1 {
		t.Fatalf("erro inseguro ou status incorreto: %#v", repository.failed[0])
	}
}

func TestSyncAnalysisPersistsInvalidRawResponseWithoutLoggingIt(t *testing.T) {
	const raw = "Resultado provável: possível venda\nconteúdo privado que não pode aparecer no log"
	repository := &assessmentRepositoryStub{}
	invalid := &conversationdomain.InvalidAnalysisResponseError{
		RawResponse:     raw,
		Cause:           errors.New("resultado provável inválido"),
		Category:        "invalid_probable_result",
		Model:           "gemma3:1b",
		PromptVersion:   conversationdomain.PromptVersion,
		MessageCount:    1,
		PossibleReceipt: true,
	}
	var logs bytes.Buffer
	useCase := syncUseCase(repository, syncAnalyzerStub{err: invalid}, "P")
	useCase.Logger = slog.New(slog.NewJSONHandler(&logs, nil))
	_, err := useCase.Execute(context.Background(), "8620")
	if err == nil || len(repository.failed) != 1 {
		t.Fatalf("falha não persistida: err=%v failed=%#v", err, repository.failed)
	}
	persisted := repository.failed[0]
	analysis := persisted.Analysis
	if analysis.Status != "FAILED" || analysis.RawResponse != raw || analysis.Model != "gemma3:1b" || analysis.PromptVersion != conversationdomain.PromptVersion || analysis.MessageCount != 1 || !analysis.PossibleReceipt {
		t.Fatalf("análise FAILED incorreta: %#v", analysis)
	}
	if persisted.Score != 80 || len(persisted.Findings) != 1 {
		t.Fatalf("score/achados perdidos: %#v", persisted)
	}
	if strings.Contains(logs.String(), raw) || strings.Contains(logs.String(), "conteúdo privado") {
		t.Fatalf("raw_response apareceu no log: %s", logs.String())
	}
	for _, metadata := range []string{"invalid_response", "invalid_probable_result", "gemma3:1b", "response_received"} {
		if !strings.Contains(logs.String(), metadata) {
			t.Fatalf("metadado seguro %q ausente do log: %s", metadata, logs.String())
		}
	}
}

func TestSyncAnalysisCreatesHistoryOnEveryExecution(t *testing.T) {
	repository := &assessmentRepositoryStub{}
	useCase := syncUseCase(repository, syncAnalyzerStub{analysis: completeTypedAnalysis("venda")}, "S")
	first, err := useCase.Execute(context.Background(), "8620")
	if err != nil {
		t.Fatal(err)
	}
	second, err := useCase.Execute(context.Background(), "8620")
	if err != nil {
		t.Fatal(err)
	}
	if first.AssessmentID == second.AssessmentID || first.AnalysisID == second.AnalysisID || len(repository.started) != 2 || len(repository.completed) != 2 {
		t.Fatalf("histórico não criado: first=%#v second=%#v", first, second)
	}
}

func TestSyncAnalysisCompletesDeterministicAuditWithoutConversation(t *testing.T) {
	tests := []struct {
		name, semanticID, finalResult, resultSource string
		findings                                    []auditdomain.Finding
		wantScore                                   int
	}{
		{name: "em andamento com achado", semanticID: "P", finalResult: "indeterminado", resultSource: "regras determinísticas; conversa indisponível", findings: []auditdomain.Finding{{Rule: "opportunity_without_value", Severity: auditdomain.SeverityHigh}}, wantScore: 80},
		{name: "em andamento sem achados", semanticID: "P", finalResult: "indeterminado", resultSource: "regras determinísticas; conversa indisponível", wantScore: 100},
		{name: "estágio ganho", semanticID: "S", finalResult: "venda", resultSource: "estágio ganho no Bitrix", wantScore: 100},
		{name: "estágio perdido", semanticID: "F", finalResult: "não venda", resultSource: "estágio perdido no Bitrix", wantScore: 100},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			repository := &assessmentRepositoryStub{}
			useCase := SyncAnalysis{
				DealSource: syncDealSourceStub{}, DealRepository: syncDealRepositoryStub{},
				Analyze:              Analyze{Source: messageSourceValueStub{}, FormSource: formSourceStub{}, StatusSource: statusSourceStub{status: conversationdomain.DealStatus{SemanticID: test.semanticID}}, Analyzer: panicAnalyzerStub{}},
				AssessmentRepository: repository, Rules: ruleEvaluatorStub{findings: test.findings},
				Now: func() time.Time { return time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC) },
			}
			result, err := useCase.Execute(context.Background(), "8620")
			if err != nil {
				t.Fatal(err)
			}
			if result.Status != "COMPLETED" || result.Score != test.wantScore || result.FinalResult != test.finalResult || result.ResultSource != test.resultSource || result.ProbableResult != "indeterminado" {
				t.Fatalf("resultado incorreto: %#v", result)
			}
			if len(repository.failed) != 0 || len(repository.completed) != 1 {
				t.Fatalf("persistência incorreta: completed=%#v failed=%#v", repository.completed, repository.failed)
			}
			persisted := repository.completed[0]
			if persisted.Score != test.wantScore || len(persisted.Findings) != len(test.findings) || persisted.Analysis.Status != "COMPLETED" || persisted.Analysis.MessageCount != 0 || persisted.Analysis.ErrorMessage != conversationUnavailableMessage || persisted.Analysis.MainReason != "" || persisted.Analysis.CustomerObjections != "" || persisted.Analysis.ServiceQuality != "" {
				t.Fatalf("avaliação indisponível incorreta: %#v", persisted)
			}
		})
	}
}
