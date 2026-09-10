package ollama

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/portfolio/auditor-ia/internal/conversation/domain"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (fn roundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return fn(request)
}

func TestAnalyzeSendsTemperatureZeroAndPreservesInvalidRawResponse(t *testing.T) {
	raw := completeResponse("possível venda", "cliente demonstrou interesse", "acompanhar")
	var temperaturePresent bool
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Options map[string]json.RawMessage `json:"options"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		encoded, ok := body.Options["temperature"]
		if ok {
			var temperature float64
			if err := json.Unmarshal(encoded, &temperature); err != nil {
				t.Fatal(err)
			}
			temperaturePresent = temperature == 0
		}
		writeOllamaResponse(t, w, raw)
	}))
	defer server.Close()

	_, err := (Analyzer{URL: server.URL, Model: "gemma3:1b", Client: server.Client()}).Analyze(
		context.Background(), "42", []domain.Message{{Role: "CLIENTE", Text: "tenho interesse"}}, nil,
	)
	if !temperaturePresent {
		t.Fatal("options.temperature = 0 não foi enviado")
	}
	var invalid *domain.InvalidAnalysisResponseError
	if !errors.As(err, &invalid) {
		t.Fatalf("errors.As = false: %T %v", err, err)
	}
	if invalid.RawResponse != raw || invalid.Model != "gemma3:1b" || invalid.PromptVersion != domain.PromptVersion || invalid.MessageCount != 1 || invalid.Category != "invalid_probable_result" {
		t.Fatalf("metadados da resposta inválida: %#v", invalid)
	}
	if strings.Contains(err.Error(), raw) {
		t.Fatalf("erro expôs raw_response: %q", err)
	}
}

func TestAnalyzeDoesNotRetryTransportTimeout(t *testing.T) {
	calls := 0
	client := &http.Client{Transport: roundTripFunc(func(*http.Request) (*http.Response, error) {
		calls++
		return nil, context.DeadlineExceeded
	})}
	_, err := (Analyzer{URL: "http://ollama.invalid", Model: "gemma3:1b", Client: client}).Analyze(context.Background(), "42", nil, nil)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("erro = %v", err)
	}
	if calls != 1 {
		t.Fatalf("requisições = %d; esperado 1", calls)
	}
}

func TestAnalyzeSendsOnlyLastFiftyMessages(t *testing.T) {
	var prompt string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Prompt string `json:"prompt"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		prompt = body.Prompt
		writeOllamaResponse(t, w, completeResponse("em negociação", "MSG-79", "acompanhar"))
	}))
	defer server.Close()
	messages := make([]domain.Message, 80)
	for index := range messages {
		messages[index] = domain.Message{Role: "CLIENTE", Text: fmt.Sprintf("MSG-%02d", index)}
	}
	if _, err := (Analyzer{URL: server.URL, Model: "gemma3:4b", Client: server.Client()}).Analyze(context.Background(), "42", messages, nil); err != nil {
		t.Fatal(err)
	}
	if strings.Contains(prompt, "MSG-29") || !strings.Contains(prompt, "MSG-30") || !strings.Contains(prompt, "MSG-79") {
		t.Fatalf("janela das últimas 50 incorreta")
	}
}

func TestBuildConversationLimitsSize(t *testing.T) {
	got := buildConversation([]domain.Message{{Text: strings.Repeat("x", maxConversationChars+1000)}})
	if len(got) > maxConversationChars+100 {
		t.Fatalf("conversa excedeu limite: %d", len(got))
	}
	if !strings.Contains(got, "início omitido") {
		t.Fatal("aviso ausente")
	}
}

func TestBuildConversationAnonymizesPIIAndRemovesSecrets(t *testing.T) {
	got := buildConversation([]domain.Message{{Role: "CLIENTE", Text: "Joao ligou de +55 (31) 99999-9999 para Ana; email joao@example.com URL https://crm.example/deal/1 access_token=segredo"}})
	for _, want := range []string{"TELEFONE_1", "EMAIL_1", "URL_1", "access_token=[token]"} {
		if !strings.Contains(got, want) {
			t.Fatalf("conteudo esperado %q ausente em %q", want, got)
		}
	}
	for _, forbidden := range []string{"joao@example.com", "99999-9999", "crm.example", "segredo"} {
		if strings.Contains(got, forbidden) {
			t.Fatalf("dado sensivel %q presente em %q", forbidden, got)
		}
	}
}

func TestNormalizeSixLinesRemovesMarkdownAndFillsMissingFields(t *testing.T) {
	analysis, err := domain.ParseConversationAnalysis("**Resultado provável:** venda\n- Motivo principal: boa condução\nObjeções do cliente: nenhuma\nQualidade do atendimento: boa\nPróxima ação recomendada: encerrar\nConfiança: alta")
	if err != nil {
		t.Fatal(err)
	}
	got := analysis.NormalizedResponse
	lines := strings.Split(got, "\n")
	if len(lines) != 6 {
		t.Fatalf("esperava exatamente seis linhas, obteve %q", got)
	}
	for _, line := range lines {
		if strings.ContainsAny(line, "*#") {
			t.Fatalf("Markdown não removido: %q", got)
		}
	}
}

func TestAnalyzePromptListsAcceptedCommercialResults(t *testing.T) {
	var prompt string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Prompt string `json:"prompt"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		prompt = body.Prompt
		w.Header().Set("Content-Type", "application/json")
		writeOllamaResponse(t, w, completeResponse("indeterminado", "evidência insuficiente", "solicitar informações"))
	}))
	defer server.Close()

	_, err := (Analyzer{URL: server.URL, Model: "test", Client: server.Client()}).Analyze(context.Background(), "42", nil, nil)
	if err != nil {
		t.Fatal(err)
	}
	want := "Resultado provável: [venda|não venda|em negociação|pós-venda/suporte|indeterminado]"
	if !strings.Contains(prompt, want) {
		t.Fatalf("classificações comerciais ausentes do prompt: %q", prompt)
	}
	if strings.Contains(prompt, "Resultado provável: em andamento") {
		t.Fatalf("classificação antiga ainda presente no prompt: %q", prompt)
	}
}

func TestHasExplicitPaymentEvidence(t *testing.T) {
	tests := []struct {
		name string
		text string
		want bool
	}{
		{name: "sim", text: "sim", want: false},
		{name: "futuro", text: "vou pagar", want: false},
		{name: "pago", text: "paguei", want: true},
		{name: "negado", text: "não paguei", want: false},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := hasExplicitPaymentEvidence([]domain.Message{{Text: tt.text}})
			if got != tt.want {
				t.Fatalf("hasExplicitPaymentEvidence(%q) = %v, want %v", tt.text, got, tt.want)
			}
		})
	}
}

func TestAnalyzeDowngradesSaleWithoutExplicitPaymentEvidence(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		writeOllamaResponse(t, w, completeResponse("venda", "cliente disse sim", "validar pagamento"))
	}))
	defer server.Close()

	got, err := (Analyzer{URL: server.URL, Model: "test", Client: server.Client()}).Analyze(
		context.Background(), "42", []domain.Message{{Text: "sim"}}, nil,
	)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(got.NormalizedResponse, "Resultado provável: em negociação") {
		t.Fatalf("resultado não foi corrigido: %q", got.NormalizedResponse)
	}
	if !strings.Contains(got.NormalizedResponse, "Próxima ação recomendada: acompanhar o pagamento") {
		t.Fatalf("recomendação não foi corrigida: %q", got.NormalizedResponse)
	}
	if strings.Contains(strings.ToLower(got.NormalizedResponse), "validar pagamento") {
		t.Fatalf("recomendação indevida preservada: %q", got.NormalizedResponse)
	}
}

func TestBuildConversationMarksPossibleClientReceipt(t *testing.T) {
	messages := []domain.Message{
		{Role: "ATENDENTE", Text: "Pode fazer o pagamento por PIX"},
		{Role: "CLIENTE", Attachments: []domain.Attachment{{Name: "imagem.png", IsImage: true}}},
	}
	got := buildConversation(messages)
	if !strings.Contains(got, possibleReceiptMarker) {
		t.Fatalf("marcador de possível comprovante ausente: %q", got)
	}
}

func TestPossibleReceiptAcceptsPDFWithPaymentContext(t *testing.T) {
	messages := []domain.Message{
		{Role: "CLIENTE", Text: "segue o comprovante do pagamento"},
		{
			Role: "CLIENTE",
			Attachments: []domain.Attachment{
				{Name: "comprovante.pdf", MIMEType: "file"},
			},
		},
	}

	if !hasPossibleReceipt(messages) {
		t.Fatal("PDF com contexto de pagamento deveria ser possível comprovante")
	}
}

func TestPossibleReceiptUsesStrongFilenameWithoutRequiringMessageText(t *testing.T) {
	tests := []struct {
		name       string
		role       string
		attachment domain.Attachment
		want       bool
	}{
		{name: "fragmento provante do Bitrix", role: "CLIENTE", attachment: domain.Attachment{Name: "mprovante-b365488d-147c-4ff7-92af-44a7569f177b.pdf", MIMEType: "file"}, want: true},
		{name: "comprovante", role: "CLIENTE", attachment: domain.Attachment{Name: "comprovante.pdf", MIMEType: "file"}, want: true},
		{name: "nome incompleto", role: "CLIENTE", attachment: domain.Attachment{Name: "  PROVANTE-123.PDF  ", MIMEType: "file"}, want: true},
		{name: "recibo", role: "CLIENTE", attachment: domain.Attachment{Name: "recibo.pdf", MIMEType: "file"}, want: true},
		{name: "atendente", role: "ATENDENTE", attachment: domain.Attachment{Name: "comprovante.pdf", MIMEType: "file"}, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			messages := []domain.Message{{Role: test.role, Text: "", Attachments: []domain.Attachment{test.attachment}}}
			if got := hasPossibleReceipt(messages); got != test.want {
				t.Fatalf("hasPossibleReceipt() = %v, esperado %v", got, test.want)
			}
		})
	}
}

func TestPossibleReceiptStrongFilenameIsIndependentFromClosingWindow(t *testing.T) {
	for _, trailingMessages := range []int{10, 15} {
		t.Run(fmt.Sprintf("%d mensagens antes do fim", trailingMessages), func(t *testing.T) {
			messages := []domain.Message{{
				Role: "CLIENTE",
				Text: "",
				Attachments: []domain.Attachment{{
					Name:     "arquivo-provante-8620.pdf",
					MIMEType: "file",
				}},
			}}
			for index := 0; index < trailingMessages; index++ {
				messages = append(messages, domain.Message{Role: "CLIENTE", Text: fmt.Sprintf("mensagem final %d", index)})
			}
			if !hasPossibleReceipt(messages) {
				t.Fatal("nome forte deixou de ser detectado fora da janela de encerramento")
			}
		})
	}
}

func TestPossibleReceiptOldGenericPDFWithoutContextIsRejected(t *testing.T) {
	messages := []domain.Message{{
		Role:        "CLIENTE",
		Attachments: []domain.Attachment{{Name: "documento.pdf", MIMEType: "file"}},
	}}
	for index := 0; index < 10; index++ {
		messages = append(messages, domain.Message{Role: "CLIENTE", Text: fmt.Sprintf("mensagem %d", index)})
	}
	if hasPossibleReceipt(messages) {
		t.Fatal("PDF genérico antigo foi tratado como possível comprovante")
	}
}

func TestPossibleReceiptGenericFilesRequireValidPaymentContext(t *testing.T) {
	tests := []struct {
		name    string
		context string
		file    domain.Attachment
		want    bool
	}{
		{name: "PDF sem contexto", context: "segue o arquivo", file: domain.Attachment{Name: "documento.pdf", MIMEType: "file"}, want: false},
		{name: "imagem por extensão com contexto", context: "pagamento via PIX", file: domain.Attachment{Name: "arquivo.jpeg", MIMEType: "file"}, want: true},
		{name: "imagem por MIME com contexto", context: "boleto pago", file: domain.Attachment{Name: "arquivo", MIMEType: "image/jpeg"}, want: true},
		{name: "imagem com intenção futura", context: "vou pagar", file: domain.Attachment{Name: "foto.jpg", IsImage: true}, want: false},
		{name: "imagem com negação", context: "não paguei", file: domain.Attachment{Name: "foto.jpg", IsImage: true}, want: false},
		{name: "imagem com pagamento posterior", context: "pago depois", file: domain.Attachment{Name: "foto.jpg", IsImage: true}, want: false},
		{name: "imagem aguardando pagar", context: "vou aguardar para pagar", file: domain.Attachment{Name: "foto.jpg", IsImage: true}, want: false},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			messages := []domain.Message{{Role: "CLIENTE", Text: test.context}, {Role: "CLIENTE", Attachments: []domain.Attachment{test.file}}}
			if got := hasPossibleReceipt(messages); got != test.want {
				t.Fatalf("hasPossibleReceipt() = %v, esperado %v", got, test.want)
			}
		})
	}
}
func TestPossibleReceiptRejectsInvalidCases(t *testing.T) {

	image := []domain.Attachment{{Name: "imagem.jpg", IsImage: true}}

	oldImageMessages := []domain.Message{
		{Role: "ATENDENTE", Text: "pagamento"},
		{Role: "CLIENTE", Attachments: image},
	}

	for i := 0; i < 15; i++ {
		oldImageMessages = append(oldImageMessages, domain.Message{
			Role: "CLIENTE",
			Text: fmt.Sprintf("mensagem %d", i+1),
		})
	}
	tests := []struct {
		name     string
		messages []domain.Message
	}{
		{name: "imagem do atendente", messages: []domain.Message{{Role: "CLIENTE", Text: "pagamento"}, {Role: "ATENDENTE", Attachments: image}}},
		{name: "sem contexto", messages: []domain.Message{{Role: "CLIENTE", Text: "segue"}, {Role: "CLIENTE", Attachments: image}}},
		{name: "contexto negado", messages: []domain.Message{{Role: "CLIENTE", Text: "não paguei"}, {Role: "CLIENTE", Attachments: image}}},
		{name: "contexto futuro", messages: []domain.Message{{Role: "CLIENTE", Text: "vou pagar"}, {Role: "CLIENTE", Attachments: image}}},
		{name: "imagem antiga", messages: oldImageMessages},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := hasPossibleReceipt(tt.messages); got {
				t.Fatalf("hasPossibleReceipt() = true para caso inválido")
			}
		})
	}
}

func TestAnalyzeRecommendsValidatingPossibleReceiptWithoutConfirmingSale(t *testing.T) {
	var prompt string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Prompt string `json:"prompt"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		prompt = body.Prompt
		w.Header().Set("Content-Type", "application/json")
		writeOllamaResponse(t, w, completeResponse("venda", "imagem anexada", "marcar como ganho"))
	}))
	defer server.Close()

	messages := []domain.Message{
		{Role: "ATENDENTE", Text: "Pagamento via boleto"},
		{Role: "CLIENTE", Attachments: []domain.Attachment{{Name: "foto.jpg", IsImage: true}}},
	}
	got, err := (Analyzer{URL: server.URL, Model: "test", Client: server.Client()}).Analyze(context.Background(), "42", messages, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(prompt, possibleReceiptMarker) {
		t.Fatalf("marcador ausente do prompt: %q", prompt)
	}
	if !strings.Contains(prompt, possibleReceiptSystemInstruction) {
		t.Fatalf("instrução não conclusiva ausente do prompt: %q", prompt)
	}
	if !got.PossibleReceipt {
		t.Fatal("possible_receipt determinístico foi sobrescrito")
	}
	if strings.Contains(got.NormalizedResponse, "Resultado provável: venda") {
		t.Fatalf("possível comprovante confirmou venda: %q", got.NormalizedResponse)
	}
	want := "Próxima ação recomendada: Validar possível comprovante e, se confirmado, marcar negócio como ganho."
	if !strings.Contains(got.NormalizedResponse, want) {
		t.Fatalf("recomendação = %q; esperado %q", got.NormalizedResponse, want)
	}
}

func TestAnalyzeUsesUnmaskedFormWithoutTreatingItAsSaleEvidence(t *testing.T) {
	var prompt string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var body struct {
			Prompt string `json:"prompt"`
		}
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		prompt = body.Prompt
		w.Header().Set("Content-Type", "application/json")
		writeOllamaResponse(t, w, completeResponse("venda", "formulário diz que pagou", "validar pagamento"))
	}))
	defer server.Close()

	forms := []domain.CRMForm{{
		ActivityID:   "90",
		Form:         "Captação Ágil",
		VisitedPages: []any{"https://exemplo.test/página?email=ana@example.com"},
		Fields: []domain.CRMFormField{
			{Caption: "Nome", Value: "Ana Gonçalves"},
			{Caption: "Mensagem", Value: "Paguei; contato ana@example.com; IP 192.0.2.10"},
		},
	}}
	got, err := (Analyzer{URL: server.URL, Model: "test", PrivacyMode: "preserve", Client: server.Client()}).Analyze(context.Background(), "42", nil, forms)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{
		"CONTEXTO DO FORMULÁRIO", "FORMULÁRIO:", "Ana Gonçalves", "ana@example.com", "192.0.2.10",
		"https://exemplo.test/página?email=ana@example.com", "CONVERSA EM ORDEM CRONOLÓGICA\n(ausente)",
	} {
		if !strings.Contains(prompt, want) {
			t.Fatalf("valor original %q ausente do prompt: %q", want, prompt)
		}
	}
	if !strings.Contains(got.NormalizedResponse, "Resultado provável: em negociação") {
		t.Fatalf("formulário confirmou venda indevidamente: %q", got.NormalizedResponse)
	}
}

func completeResponse(result, reason, action string) string {
	return "Resultado provável: " + result +
		"\nMotivo principal: " + reason +
		"\nObjeções do cliente: indeterminado" +
		"\nQualidade do atendimento: boa" +
		"\nPróxima ação recomendada: " + action +
		"\nConfiança: alta"
}

func writeOllamaResponse(t *testing.T, w http.ResponseWriter, response string) {
	t.Helper()
	if err := json.NewEncoder(w).Encode(map[string]string{"response": response}); err != nil {
		t.Fatal(err)
	}
}

func TestPromptSeparatesFormAndConversation(t *testing.T) {
	formContext := buildFormContext([]domain.CRMForm{{ActivityID: "1", Fields: []domain.CRMFormField{{Caption: "Nome", Value: "Bia"}}}})
	conversation := buildConversation([]domain.Message{{Role: "CLIENTE", Text: "Olá"}})
	if !strings.Contains(formContext, "Nome: Bia") || !strings.Contains(conversation, "CLIENTE: Olá") {
		t.Fatalf("contextos não foram construídos separadamente: formulário=%q conversa=%q", formContext, conversation)
	}
}
