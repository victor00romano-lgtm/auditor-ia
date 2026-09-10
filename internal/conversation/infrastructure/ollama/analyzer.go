package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"regexp"
	"strconv"
	"strings"
	"time"

	"github.com/portfolio/auditor-ia/internal/conversation/domain"
	"github.com/portfolio/auditor-ia/internal/conversation/privacy"
	"github.com/portfolio/auditor-ia/internal/observability"
	"go.opentelemetry.io/otel/attribute"
)

const (
	maxConversationChars             = 6000
	possibleReceiptClosingWindow     = 5
	possibleReceiptContextWindow     = 3
	possibleReceiptSystemInstruction = "SISTEMA: há um possível comprovante anexado pelo cliente. O arquivo não foi validado; não considere pagamento confirmado sem outra evidência."
)

const possibleReceiptMarker = "[POSSÍVEL COMPROVANTE ENVIADO PELO CLIENTE — NÃO VALIDADO]"

type Analyzer struct {
	URL, Model  string
	PrivacyMode string
	Client      *http.Client
}

func (a Analyzer) Analyze(ctx context.Context, dealID string, messages []domain.Message, forms []domain.CRMForm) (analysis domain.ConversationAnalysis, returnErr error) {
	started := time.Now()
	messageCount := len(messages)
	ctx, span := observability.Tracer().Start(ctx, "ollama.generate")
	span.SetAttributes(attribute.String("dependency", "ollama"), attribute.String("operation", "generate"), attribute.Int("message.count", len(messages)), attribute.Int("form.count", len(forms)))
	defer func() {
		span.End()
		if metrics := observability.CurrentMetrics(); metrics != nil {
			status := map[bool]string{true: "error", false: "success"}[returnErr != nil]
			metrics.External.WithLabelValues("ollama", "generate", status).Inc()
			metrics.ExternalDuration.WithLabelValues("ollama", "generate", status).Observe(time.Since(started).Seconds())
			metrics.OllamaDuration.WithLabelValues(status).Observe(time.Since(started).Seconds())
		}
	}()
	_ = dealID // identificador técnico desnecessário ao modelo
	possibleReceipt := hasPossibleReceipt(messages)
	privacyMode := a.PrivacyMode
	if privacyMode == "" {
		privacyMode = "anonymize"
	}
	privacySession := privacy.NewSession(privacyMode)
	for _, message := range messages {
		privacySession.RegisterName(message.SenderName, message.Role)
	}
	for _, form := range forms {
		for _, field := range form.Fields {
			label := strings.ToLower(field.Code + " " + field.Caption)
			if strings.Contains(label, "nome") || strings.Contains(label, "name") {
				privacySession.RegisterName(fmt.Sprint(field.Value), "CLIENTE")
			}
		}
	}
	if len(messages) > domain.MaxAnalyzedMessages {
		messages = messages[len(messages)-domain.MaxAnalyzedMessages:]
	}
	receiptInstruction := ""
	if possibleReceipt {
		receiptInstruction = "\n" + possibleReceiptSystemInstruction + "\n"
	}
	prompt := fmt.Sprintf(`Você é um auditor comercial do Bitrix24. Analise os contextos disponíveis do negócio.
Use somente evidências presentes nos contextos. Não invente fatos nem use Markdown.
O Bitrix24 é apenas o sistema CRM, não o produto vendido.
Nunca suponha qual produto ou serviço está sendo vendido.
Quando isso não estiver explícito, diga "produto ou serviço não identificado".
O formulário CRM é um contexto separado e não é uma conversa.
Respostas de formulário demonstram somente interesse ou solicitação. Nunca comprovam pagamento, contratação ou venda.
Classifique o resultado:
- venda: pagamento explicitamente realizado, recebido ou aprovado, ou comprovante enviado;
- não venda: recusa, desistência ou perda explícita;
- em negociação: interesse, proposta, renovação ou compra ainda sem pagamento confirmado;
- pós-venda/suporte: atendimento sobre compra anterior, sem nova venda em andamento;
- indeterminado: evidência insuficiente.

REGRAS CRÍTICAS:
1. “Sim”, “quero comprar”, “vou pagar”, preço aceito, chave PIX enviada, matrícula criada ou finalizada NÃO comprovam venda.
2. Renovação, recompra, upgrade e extensão são novas vendas.
3. Renovação sem pagamento confirmado é EM NEGOCIAÇÃO.
4. Se classificar como VENDA, o Motivo principal deve citar a mensagem exata que comprova o pagamento. Sem essa mensagem, classifique como EM NEGOCIAÇÃO.
5. Pedido de certificado, acesso, entrega, suporte, reclamação ou reembolso referente a uma compra anterior é PÓS-VENDA/SUPORTE.
6. Não deduza fatos ausentes.
%s

Próxima ação:
- pagamento comprovado: validar o pagamento, marcar o negócio como ganho, confirmar ao cliente e encerrar;
- negociação sem pagamento: acompanhar ou enviar instruções de pagamento;
- não venda: registrar o motivo e encerrar;
- pós-venda/suporte: recomendar somente uma ação sustentada pela conversa;
- indeterminado: solicitar informação necessária.

Responda em exatamente seis linhas, sem texto adicional:
Resultado provável: [venda|não venda|em negociação|pós-venda/suporte|indeterminado]
Motivo principal: [inclua evidência concreta]
Objeções do cliente: [texto ou indeterminado]
Qualidade do atendimento: [boa|regular|ruim|indeterminada]
Próxima ação recomendada: [uma ação objetiva]
Confiança: [baixa|média|alta]

CONTEXTO DO FORMULÁRIO
%s

CONVERSA EM ORDEM CRONOLÓGICA
%s`, receiptInstruction, buildFormContext(forms, privacySession), buildConversation(messages, privacySession))
	body, err := json.Marshal(map[string]any{"model": a.Model, "prompt": prompt, "stream": false, "keep_alive": "10m", "options": map[string]any{"temperature": 0, "num_predict": 220}})
	if err != nil {
		return domain.ConversationAnalysis{}, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, a.URL+"/api/generate", bytes.NewReader(body))
	if err != nil {
		return domain.ConversationAnalysis{}, err
	}
	req.Header.Set("Content-Type", "application/json")
	client := a.Client
	if client == nil {
		client = &http.Client{Timeout: 3 * time.Minute}
	}
	resp, err := client.Do(req)
	if err != nil {
		return domain.ConversationAnalysis{}, err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return domain.ConversationAnalysis{}, fmt.Errorf("Ollama HTTP %d", resp.StatusCode)
	}
	var result struct {
		Response *string `json:"response"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return domain.ConversationAnalysis{}, err
	}
	if result.Response == nil {
		return domain.ConversationAnalysis{}, errors.New("resposta do Ollama sem campo response")
	}
	analysis, err = domain.ParseConversationAnalysis(*result.Response)
	if err != nil {
		var invalid *domain.InvalidAnalysisResponseError
		if errors.As(err, &invalid) {
			invalid.Model = a.Model
			invalid.PromptVersion = domain.PromptVersion
			invalid.MessageCount = messageCount
			invalid.PossibleReceipt = possibleReceipt
		}
		return domain.ConversationAnalysis{}, fmt.Errorf("interpretar resposta do Ollama: %w", err)
	}
	if analysis.ProbableResult == "venda" && !hasExplicitPaymentEvidence(messages) {
		analysis.ProbableResult = "em negociação"
		analysis.RecommendedAction = "acompanhar o pagamento e solicitar confirmação quando for realizado"
	}
	analysis.PossibleReceipt = possibleReceipt
	if analysis.ProbableResult == "em negociação" && analysis.PossibleReceipt {
		analysis.RecommendedAction = "Validar possível comprovante e, se confirmado, marcar negócio como ganho."
	}
	analysis.Model = a.Model
	analysis.PromptVersion = domain.PromptVersion
	analysis.NormalizedResponse = analysis.Format()
	return analysis, nil
}

func buildFormContext(forms []domain.CRMForm, sessions ...*privacy.Session) string {
	if len(forms) == 0 {
		return "(ausente)"
	}
	transform := privacy.NewSession("anonymize")
	if len(sessions) > 0 && sessions[0] != nil {
		transform = sessions[0]
	}
	blocks := make([]string, 0, len(forms))
	for _, form := range forms {
		lines := []string{"FORMULÁRIO:"}
		if form.Form != nil {
			lines = append(lines, "Formulário: "+transform.Transform(formatFormValue(form.Form)))
		}
		if form.VisitedPages != nil {
			lines = append(lines, "Páginas visitadas: "+transform.Transform(formatFormValue(form.VisitedPages)))
		}
		for _, field := range form.Fields {
			label := field.Caption
			if label == "" {
				label = field.Code
			}
			if label == "" {
				label = "Campo"
			}
			lines = append(lines, label+": "+transform.Transform(formatFormValue(field.Value)))
		}
		blocks = append(blocks, strings.Join(lines, "\n"))
	}
	return strings.Join(blocks, "\n\n")
}

func formatFormValue(value any) string {
	switch value := value.(type) {
	case nil:
		return "null"
	case string:
		return value
	case json.Number:
		return value.String()
	case bool:
		return strconv.FormatBool(value)
	case []any:
		values := make([]string, 0, len(value))
		for _, item := range value {
			values = append(values, formatFormValue(item))
		}
		return strings.Join(values, ", ")
	default:
		var buffer bytes.Buffer
		encoder := json.NewEncoder(&buffer)
		encoder.SetEscapeHTML(false)
		if err := encoder.Encode(value); err == nil {
			return strings.TrimSpace(buffer.String())
		}
		return fmt.Sprint(value)
	}
}

var (
	explicitPaymentPatterns = []*regexp.Regexp{
		regexp.MustCompile(`\b(?:eu\s+)?paguei\b`),
		regexp.MustCompile(`\bpagamento\s+(?:foi\s+)?(?:realizado|aprovado|recebido|confirmado|compensado)\b`),
		regexp.MustCompile(`\b(?:segue|enviei|mandei|encaminhei|anexei)\s+(?:o\s+)?comprovante\b`),
		regexp.MustCompile(`\bcomprovante\s+(?:foi\s+)?(?:enviado|recebido|anexado)\b`),
		regexp.MustCompile(`\b(?:recebemos|recebi|aprovamos|aprovei|confirmamos|confirmei)\s+(?:o\s+)?pagamento\b`),
	}
	negatedPaymentPattern = regexp.MustCompile(`\b(?:nao|nunca|nem)\b[^.!?;]{0,35}\b(?:paguei|pagamento|comprovante)\b`)
	futurePaymentPattern  = regexp.MustCompile(`\b(?:vou|irei|vamos|pretendo|devo)\s+(?:te\s+|lhe\s+)?(?:pagar|enviar|mandar|encaminhar)\b|\bpagamento\s+(?:sera|vai\s+ser)\b`)
)

// hasExplicitPaymentEvidence accepts only a statement that the payment or its
// receipt already happened. Intentions, generic agreement and workflow status
// are deliberately ignored.
func hasExplicitPaymentEvidence(messages []domain.Message) bool {
	for _, message := range messages {
		text := normalizePaymentText(message.Text)
		if negatedPaymentPattern.MatchString(text) || futurePaymentPattern.MatchString(text) {
			continue
		}
		for _, pattern := range explicitPaymentPatterns {
			if pattern.MatchString(text) {
				return true
			}
		}
	}
	return false
}

func normalizePaymentText(text string) string {
	text = strings.ToLower(text)
	return strings.NewReplacer(
		"á", "a", "à", "a", "â", "a", "ã", "a", "ä", "a",
		"é", "e", "è", "e", "ê", "e", "ë", "e",
		"í", "i", "ì", "i", "î", "i", "ï", "i",
		"ó", "o", "ò", "o", "ô", "o", "õ", "o", "ö", "o",
		"ú", "u", "ù", "u", "û", "u", "ü", "u", "ç", "c",
	).Replace(text)
}

func buildConversation(messages []domain.Message, sessions ...*privacy.Session) string {
	if len(messages) == 0 {
		return "(ausente)"
	}
	transform := privacy.NewSession("anonymize")
	if len(sessions) > 0 && sessions[0] != nil {
		transform = sessions[0]
	}
	lines := make([]string, 0, len(messages))
	receiptIndexes := possibleReceiptIndexes(messages)
	for index, message := range messages {
		text := transform.Transform(message.Text)
		if receiptIndexes[index] {
			text = strings.TrimSpace(text + " " + possibleReceiptMarker)
		}
		lines = append(lines, fmt.Sprintf("[%s] %s: %s", message.CreatedAt.Format("2006-01-02 15:04"), message.Role, text))
	}
	conversation := strings.Join(lines, "\n")
	if len(conversation) <= maxConversationChars {
		return conversation
	}
	return "[início omitido por limite de contexto]\n" + conversation[len(conversation)-maxConversationChars:]
}

var (
	paymentContextPattern  = regexp.MustCompile(`\b(?:pagamento|paguei|pago|pix|boleto|cartao|comprovante|transferencia|recibo)\b`)
	negativeContextPattern = regexp.MustCompile(`\b(?:nao|nunca|nem)\b[^.!?;]{0,35}\b(?:paguei|pago|pagamento|pix|boleto|cartao|comprovante|transferencia|recibo)\b`)
	futureContextPattern   = regexp.MustCompile(`\b(?:vou|irei|vamos|pretendo|devo|aguardar)\b[^.!?;]{0,25}\b(?:pagar|fazer|enviar|mandar|transferir)\b|\b(?:pagamento|pix|boleto|transferencia|pago)\b[^.!?;]{0,20}\b(?:sera|amanha|depois)\b`)
)

func hasPossibleReceipt(messages []domain.Message) bool {
	return len(possibleReceiptIndexes(messages)) > 0
}

func possibleReceiptIndexes(messages []domain.Message) map[int]bool {
	result := make(map[int]bool)
	var humanIndexes []int
	for index, message := range messages {
		if message.Role == "CLIENTE" || message.Role == "ATENDENTE" {
			humanIndexes = append(humanIndexes, index)
		}
		// Strong filenames are evaluated across the complete input, independently
		// from closing position and nearby text.
		if message.Role == "CLIENTE" && hasReceiptFilename(message) {
			result[index] = true
		}
	}
	start := len(humanIndexes) - possibleReceiptClosingWindow
	if start < 0 {
		start = 0
	}
	for position := start; position < len(humanIndexes); position++ {
		index := humanIndexes[position]
		message := messages[index]

		if message.Role != "CLIENTE" || len(message.Attachments) == 0 {
			continue
		}

		if result[index] {
			continue
		}
		if !hasReceiptCandidateAttachment(message) {
			continue
		}

		from, to := position-possibleReceiptContextWindow, position+possibleReceiptContextWindow
		if from < 0 {
			from = 0
		}
		if to >= len(humanIndexes) {
			to = len(humanIndexes) - 1
		}

		for contextPosition := from; contextPosition <= to; contextPosition++ {
			if contextPosition == position {
				continue
			}

			contextIndex := humanIndexes[contextPosition]
			text := normalizePaymentText(messages[contextIndex].Text)

			if paymentContextPattern.MatchString(text) &&
				!negativeContextPattern.MatchString(text) &&
				!futureContextPattern.MatchString(text) {
				result[index] = true
				break
			}
		}
	}
	return result
}

func hasReceiptFilename(message domain.Message) bool {
	for _, attachment := range message.Attachments {
		name := normalizePaymentText(strings.TrimSpace(strings.ToLower(attachment.Name)))

		if strings.Contains(name, "provante") ||
			strings.Contains(name, "recibo") {
			return true
		}
	}

	return false
}

func hasReceiptCandidateAttachment(message domain.Message) bool {
	for _, attachment := range message.Attachments {
		if attachment.IsImage {
			return true
		}

		name := strings.TrimSpace(strings.ToLower(attachment.Name))
		mimeType := strings.TrimSpace(strings.ToLower(attachment.MIMEType))
		if strings.HasSuffix(name, ".pdf") || mimeType == "application/pdf" || mimeType == "pdf" {
			return true
		}
		if mimeType == "image" || strings.HasPrefix(mimeType, "image/") {
			return true
		}
		for _, extension := range []string{".png", ".jpg", ".jpeg", ".webp"} {
			if strings.HasSuffix(name, extension) {
				return true
			}
		}
	}

	return false
}
