package application

import (
	"context"
	"errors"
	"fmt"

	"github.com/portfolio/auditor-ia/internal/conversation/domain"
)

type Analyze struct {
	Source        domain.Source
	FormSource    domain.CRMFormSource
	StatusSource  domain.DealStatusSource
	AssigneeRoles domain.AssigneeRoleSource
	Analyzer      domain.Analyzer
}

func (uc Analyze) Execute(ctx context.Context, dealID string) (*domain.Result, error) {
	if dealID == "" {
		return nil, errors.New("ID do negócio é obrigatório")
	}
	conversation, err := uc.CollectConversation(ctx, dealID)
	if err != nil {
		return nil, err
	}
	return uc.ExecuteCollected(ctx, dealID, conversation)
}

func (uc Analyze) CollectConversation(ctx context.Context, dealID string) (domain.Conversation, error) {
	conversation := domain.Conversation{Status: domain.ConversationNoConversation}
	var err error
	if source, ok := uc.Source.(domain.ConversationSource); ok {
		conversation, err = source.Conversation(ctx, dealID)
	} else {
		conversation.Messages, err = uc.Source.Messages(ctx, dealID)
		if len(conversation.Messages) > 0 {
			conversation.Status = domain.ConversationAvailable
		}
	}
	if err != nil {
		return domain.Conversation{}, fmt.Errorf("buscar conversa do negócio %s: %w", dealID, err)
	}
	return conversation, nil
}

func (uc Analyze) ExecuteCollected(ctx context.Context, dealID string, conversation domain.Conversation) (*domain.Result, error) {
	messages := conversation.Messages
	var err error
	formSource := uc.FormSource
	if formSource == nil {
		formSource, _ = uc.Source.(domain.CRMFormSource)
	}
	var forms []domain.CRMForm
	if formSource != nil {
		forms, err = formSource.Forms(ctx, dealID)
		if err != nil {
			return nil, fmt.Errorf("buscar formulários CRM do negócio %s: %w", dealID, err)
		}
	}
	statusSource := uc.StatusSource
	if statusSource == nil {
		statusSource, _ = uc.Source.(domain.DealStatusSource)
	}
	if statusSource == nil {
		return nil, errors.New("fonte de estágio do negócio não configurada")
	}
	status, err := statusSource.Status(ctx, dealID)
	if err != nil {
		return nil, fmt.Errorf("buscar estágio do negócio %s: %w", dealID, err)
	}
	isSupport := false
	if !status.Closed && status.AssignedByID > 0 && uc.AssigneeRoles != nil {
		isSupport, err = uc.AssigneeRoles.IsSupport(ctx, status.AssignedByID)
		if err != nil {
			return nil, fmt.Errorf("classificar responsável do negócio %s: %w", dealID, err)
		}
	}
	if conversation.Status == domain.ConversationAccessDenied || (len(messages) == 0 && len(forms) == 0) {
		return unavailableConversationResult(dealID, status, conversation, isSupport), nil
	}
	analysis, err := uc.Analyzer.Analyze(ctx, dealID, messages, forms)
	if err != nil {
		return nil, fmt.Errorf("analisar conversa do negócio %s: %w", dealID, err)
	}
	fieldCount := 0
	for _, form := range forms {
		fieldCount += len(form.Fields)
	}
	result := &domain.Result{
		DealID:                dealID,
		MessageCount:          len(messages),
		CollectedMessageCount: len(messages),
		AnalyzedMessageCount:  min(len(messages), domain.MaxAnalyzedMessages),
		FormCount:             len(forms),
		FormFieldCount:        fieldCount,
		Analysis:              analysis.NormalizedResponse,
		AnalysisSource:        analysisSource(messages, forms),
		ConversationState:     analysis.ProbableResult,
		RecommendedAction:     analysis.RecommendedAction,
		StructuredAnalysis:    analysis,
		ConversationStatus:    domain.ConversationAvailable,
	}
	reconcile(result, status, isSupport)
	return result, nil
}

const conversationUnavailableMessage = "Análise de conversa indisponível por ausência de mensagens IMOPENLINES e formulário CRM."
const conversationAccessDeniedMessage = "Conversa localizada, mas o Bitrix negou acesso ao histórico."
const conversationEmptyMessage = "Conversa localizada, mas não contém mensagens humanas analisáveis."

func unavailableConversationResult(dealID string, status domain.DealStatus, conversation domain.Conversation, isSupport bool) *domain.Result {
	finalResult := "indeterminado"
	resultSource := "regras determinísticas; conversa indisponível"
	switch status.SemanticID {
	case "S":
		finalResult = "venda"
		resultSource = "estágio ganho no Bitrix"
	case "F":
		finalResult = "não venda"
		resultSource = "estágio perdido no Bitrix"
	default:
		if !status.Closed && isSupport {
			finalResult = "pós-venda/suporte"
			resultSource = "responsável de suporte no Bitrix"
		}
	}
	analysis := domain.ConversationAnalysis{ProbableResult: "indeterminado"}
	message := conversationUnavailableMessage
	if conversation.Status == domain.ConversationAccessDenied {
		message = conversationAccessDeniedMessage
	} else if conversation.Status == domain.ConversationEmpty {
		message = conversationEmptyMessage
	}
	return &domain.Result{
		DealID:             dealID,
		MessageCount:       0,
		Analysis:           message,
		AnalysisSource:     "Análise de conversa indisponível",
		ConversationState:  "indeterminado",
		FinalResult:        finalResult,
		ResultSource:       resultSource,
		Observation:        message,
		StructuredAnalysis: analysis,
		ConversationStatus: conversation.Status,
		SessionID:          conversation.SessionID,
		ConversationError:  conversation.ErrorCode,
	}
}

func analysisSource(messages []domain.Message, forms []domain.CRMForm) string {
	if len(messages) > 0 && len(forms) > 0 {
		return "conversa IMOPENLINES e formulário CRM"
	}
	if len(forms) > 0 {
		return "formulário CRM"
	}
	return "conversa IMOPENLINES"
}

func reconcile(result *domain.Result, status domain.DealStatus, isSupport bool) {
	switch status.SemanticID {
	case "S":
		result.FinalResult = "venda"
		result.ResultSource = "estágio ganho no Bitrix"
		if result.ConversationState == "venda" {
			result.Observation = "Resultado do CRM consistente com a conversa."
			result.RecommendedAction = "Nenhuma correção necessária."
		} else {
			result.Observation = "O fechamento pode ter ocorrido fora do canal analisado ou não ter sido registrado na conversa."
			result.RecommendedAction = "Registrar no CRM o contato externo e a evidência do fechamento."
		}
	case "F":
		result.FinalResult = "não venda"
		result.ResultSource = "estágio perdido no Bitrix"
		if result.ConversationState != "não venda" {
			result.Observation = "O encerramento como perdido não está evidenciado na conversa analisada."
			result.RecommendedAction = "Registrar no CRM o motivo da perda."
		}
	case "P":
		if !status.Closed && isSupport {
			result.FinalResult = "pós-venda/suporte"
			result.ResultSource = "responsável de suporte no Bitrix"
			return
		}
		result.FinalResult = result.ConversationState
		result.ResultSource = "análise da conversa; negócio ainda em andamento no Bitrix"
		if result.ConversationState == "venda" {
			result.Observation = "Possível venda ainda não atualizada no CRM."
			result.RecommendedAction = "Validar o pagamento e, se confirmado, marcar o negócio como ganho."
		}
	}
}
