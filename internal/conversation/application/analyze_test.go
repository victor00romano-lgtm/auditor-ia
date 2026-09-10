package application

import (
	"context"
	"errors"
	"testing"

	"github.com/portfolio/auditor-ia/internal/conversation/domain"
)

type messageSourceStub struct{}

func (messageSourceStub) Messages(context.Context, string) ([]domain.Message, error) {
	return []domain.Message{{ID: "1", Role: "CLIENTE", Text: "mensagem"}}, nil
}

type messageSourceValueStub struct {
	messages []domain.Message
}

type typedConversationSourceStub struct{ conversation domain.Conversation }

func (stub typedConversationSourceStub) Messages(context.Context, string) ([]domain.Message, error) {
	return stub.conversation.Messages, nil
}
func (stub typedConversationSourceStub) Conversation(context.Context, string) (domain.Conversation, error) {
	return stub.conversation, nil
}

func (stub messageSourceValueStub) Messages(context.Context, string) ([]domain.Message, error) {
	return stub.messages, nil
}

type formSourceStub struct {
	forms []domain.CRMForm
	err   error
}

func (stub formSourceStub) Forms(context.Context, string) ([]domain.CRMForm, error) {
	return stub.forms, stub.err
}

type analyzerStub struct{ state string }

func (stub analyzerStub) Analyze(context.Context, string, []domain.Message, []domain.CRMForm) (domain.ConversationAnalysis, error) {
	analysis := domain.ConversationAnalysis{
		ProbableResult:     stub.state,
		MainReason:         "teste",
		CustomerObjections: "indeterminado",
		ServiceQuality:     "boa",
		RecommendedAction:  "teste",
		Confidence:         "alta",
		PromptVersion:      domain.PromptVersion,
	}
	analysis.NormalizedResponse = analysis.Format()
	return analysis, nil
}

type statusSourceStub struct {
	status domain.DealStatus
	err    error
}

type assigneeRoleSourceStub struct{ support bool }

func (stub assigneeRoleSourceStub) IsSupport(context.Context, int64) (bool, error) {
	return stub.support, nil
}

func (stub statusSourceStub) Status(context.Context, string) (domain.DealStatus, error) {
	return stub.status, stub.err
}

func TestAnalyzeReconcilesCRMStageAndConversation(t *testing.T) {
	tests := []struct {
		name              string
		semanticID        string
		conversation      string
		finalResult       string
		resultSource      string
		observation       string
		recommendedAction string
	}{
		{
			name: "S com venda", semanticID: "S", conversation: "venda", finalResult: "venda",
			resultSource: "estágio ganho no Bitrix", observation: "Resultado do CRM consistente com a conversa.",
			recommendedAction: "Nenhuma correção necessária.",
		},
		{
			name: "S com negociação", semanticID: "S", conversation: "em negociação", finalResult: "venda",
			resultSource: "estágio ganho no Bitrix", observation: "O fechamento pode ter ocorrido fora do canal analisado ou não ter sido registrado na conversa.",
			recommendedAction: "Registrar no CRM o contato externo e a evidência do fechamento.",
		},
		{
			name: "F com não venda", semanticID: "F", conversation: "não venda", finalResult: "não venda",
			resultSource: "estágio perdido no Bitrix", recommendedAction: "teste",
		},
		{
			name: "F com venda", semanticID: "F", conversation: "venda", finalResult: "não venda",
			resultSource: "estágio perdido no Bitrix", observation: "O encerramento como perdido não está evidenciado na conversa analisada.",
			recommendedAction: "Registrar no CRM o motivo da perda.",
		},
		{
			name: "P com negociação", semanticID: "P", conversation: "em negociação", finalResult: "em negociação",
			resultSource: "análise da conversa; negócio ainda em andamento no Bitrix", recommendedAction: "teste",
		},
		{
			name: "P com venda", semanticID: "P", conversation: "venda", finalResult: "venda",
			resultSource: "análise da conversa; negócio ainda em andamento no Bitrix", observation: "Possível venda ainda não atualizada no CRM.",
			recommendedAction: "Validar o pagamento e, se confirmado, marcar o negócio como ganho.",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			useCase := Analyze{
				Source:       messageSourceStub{},
				StatusSource: statusSourceStub{status: domain.DealStatus{SemanticID: tt.semanticID}},
				Analyzer:     analyzerStub{state: tt.conversation},
			}
			got, err := useCase.Execute(context.Background(), "42")
			if err != nil {
				t.Fatal(err)
			}
			if got.FinalResult != tt.finalResult || got.ResultSource != tt.resultSource || got.ConversationState != tt.conversation || got.Observation != tt.observation || got.RecommendedAction != tt.recommendedAction {
				t.Fatalf("reconciliação inesperada: %#v", got)
			}
		})
	}
}

func TestAnalyzeAppliesSupportAndCRMStagePrecedenceAfterOllama(t *testing.T) {
	tests := []struct {
		name, semanticID, aiResult, finalResult, resultSource string
		assignedByID                                          int64
		support                                               bool
	}{
		{"Camila aberta com negociação", "P", "em negociação", "pós-venda/suporte", "responsável de suporte no Bitrix", 3066, true},
		{"Camila aberta com indeterminado", "P", "indeterminado", "pós-venda/suporte", "responsável de suporte no Bitrix", 3066, true},
		{"outro responsável aberto", "P", "em negociação", "em negociação", "análise da conversa; negócio ainda em andamento no Bitrix", 952, false},
		{"perdido com negociação", "F", "em negociação", "não venda", "estágio perdido no Bitrix", 952, false},
		{"perdido com Camila", "F", "pós-venda/suporte", "não venda", "estágio perdido no Bitrix", 3066, true},
		{"ganho com Camila", "S", "pós-venda/suporte", "venda", "estágio ganho no Bitrix", 3066, true},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			got, err := (Analyze{
				Source:        messageSourceStub{},
				StatusSource:  statusSourceStub{status: domain.DealStatus{SemanticID: test.semanticID, Closed: test.semanticID != "P", AssignedByID: test.assignedByID}},
				AssigneeRoles: assigneeRoleSourceStub{support: test.support},
				Analyzer:      analyzerStub{state: test.aiResult},
			}).Execute(context.Background(), "34710")
			if err != nil {
				t.Fatal(err)
			}
			if got.FinalResult != test.finalResult || got.ResultSource != test.resultSource || got.ConversationState != test.aiResult || got.StructuredAnalysis.ProbableResult != test.aiResult {
				t.Fatalf("resultado=%#v", got)
			}
		})
	}
}

func TestAnalyzePropagatesDealStatusError(t *testing.T) {
	wantErr := errors.New("API indisponível")
	_, err := (Analyze{Source: messageSourceStub{}, StatusSource: statusSourceStub{err: wantErr}, Analyzer: analyzerStub{state: "venda"}}).Execute(context.Background(), "42")
	if !errors.Is(err, wantErr) {
		t.Fatalf("erro = %v; esperado %v", err, wantErr)
	}
}

func TestAnalyzeSupportsFormOnlyAndConversationWithForm(t *testing.T) {
	form := domain.CRMForm{ActivityID: "10", Fields: []domain.CRMFormField{{Caption: "Nome", Value: "Ana"}}}
	tests := []struct {
		name         string
		messages     []domain.Message
		wantSource   string
		wantMessages int
	}{
		{name: "somente formulário", wantSource: "formulário CRM", wantMessages: 0},
		{name: "formulário e conversa", messages: []domain.Message{{ID: "1", Text: "olá"}}, wantSource: "conversa IMOPENLINES e formulário CRM", wantMessages: 1},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := (Analyze{
				Source:       messageSourceValueStub{messages: tt.messages},
				FormSource:   formSourceStub{forms: []domain.CRMForm{form}},
				StatusSource: statusSourceStub{status: domain.DealStatus{SemanticID: "P"}},
				Analyzer:     analyzerStub{state: "em negociação"},
			}).Execute(context.Background(), "42")
			if err != nil {
				t.Fatal(err)
			}
			if got.AnalysisSource != tt.wantSource || got.MessageCount != tt.wantMessages || got.FormCount != 1 || got.FormFieldCount != 1 {
				t.Fatalf("resultado inesperado: %#v", got)
			}
		})
	}
}

type panicAnalyzerStub struct{}

func (panicAnalyzerStub) Analyze(context.Context, string, []domain.Message, []domain.CRMForm) (domain.ConversationAnalysis, error) {
	panic("Ollama não deveria ser chamado")
}

func TestAnalyzeSupportsAbsenceOfConversationAndFormWithoutOllama(t *testing.T) {
	for _, test := range []struct{ semanticID, finalResult, resultSource string }{{"P", "indeterminado", "regras determinísticas; conversa indisponível"}, {"S", "venda", "estágio ganho no Bitrix"}, {"F", "não venda", "estágio perdido no Bitrix"}} {
		t.Run(test.semanticID, func(t *testing.T) {
			got, err := (Analyze{
				Source:       messageSourceValueStub{},
				FormSource:   formSourceStub{},
				StatusSource: statusSourceStub{status: domain.DealStatus{SemanticID: test.semanticID}},
				Analyzer:     panicAnalyzerStub{},
			}).Execute(context.Background(), "42")
			if err != nil {
				t.Fatal(err)
			}
			if got.MessageCount != 0 || got.ConversationState != "indeterminado" || got.FinalResult != test.finalResult || got.ResultSource != test.resultSource || got.AnalysisSource != "Análise de conversa indisponível" {
				t.Fatalf("resultado indisponível incorreto: %#v", got)
			}
			analysis := got.StructuredAnalysis
			if analysis.MainReason != "" || analysis.CustomerObjections != "" || analysis.ServiceQuality != "" {
				t.Fatalf("campos conversacionais fabricados: %#v", analysis)
			}
		})
	}
}

func TestAnalyzeAccessDeniedCompletesWithoutOllama(t *testing.T) {
	got, err := (Analyze{
		Source: typedConversationSourceStub{conversation: domain.Conversation{
			Status: domain.ConversationAccessDenied, SessionID: "14", ErrorCode: "ACCESS_DENIED",
		}},
		FormSource:   formSourceStub{},
		StatusSource: statusSourceStub{status: domain.DealStatus{SemanticID: "P"}},
		Analyzer:     panicAnalyzerStub{},
	}).Execute(context.Background(), "52")
	if err != nil {
		t.Fatal(err)
	}
	if got.ConversationStatus != domain.ConversationAccessDenied || got.MessageCount != 0 || got.SessionID != "14" || got.Observation != conversationAccessDeniedMessage || got.FinalResult != "indeterminado" {
		t.Fatalf("resultado ACCESS_DENIED incorreto: %#v", got)
	}
}

func TestAnalyzeDistinguishesEmptyConversation(t *testing.T) {
	got, err := (Analyze{
		Source:       typedConversationSourceStub{conversation: domain.Conversation{Status: domain.ConversationEmpty, SessionID: "14"}},
		FormSource:   formSourceStub{},
		StatusSource: statusSourceStub{status: domain.DealStatus{SemanticID: "S"}},
		Analyzer:     panicAnalyzerStub{},
	}).Execute(context.Background(), "52")
	if err != nil || got.ConversationStatus != domain.ConversationEmpty || got.Observation != conversationEmptyMessage || got.FinalResult != "venda" {
		t.Fatalf("resultado EMPTY_CONVERSATION incorreto: %#v err=%v", got, err)
	}
}
