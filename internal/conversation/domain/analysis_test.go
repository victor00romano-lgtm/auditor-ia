package domain

import (
	"errors"
	"strings"
	"testing"
)

const validAnalysis = "Resultado provável: em negociação\nMotivo principal: pagamento pendente\nObjeções do cliente: preço\nQualidade do atendimento: boa\nPróxima ação recomendada: acompanhar\nConfiança: alta"

func TestParseConversationAnalysisParsesSixLines(t *testing.T) {
	analysis, err := ParseConversationAnalysis(validAnalysis)
	if err != nil {
		t.Fatal(err)
	}
	if analysis.ProbableResult != "em negociação" || analysis.MainReason != "pagamento pendente" || analysis.ServiceQuality != "boa" || analysis.Confidence != "alta" {
		t.Fatalf("análise inesperada: %#v", analysis)
	}
	if analysis.RawResponse != validAnalysis || analysis.NormalizedResponse != validAnalysis {
		t.Fatalf("resposta bruta/normalizada não preservada: %#v", analysis)
	}
}

func TestParseConversationAnalysisRejectsIncompleteResponse(t *testing.T) {
	_, err := ParseConversationAnalysis("Resultado provável: venda\nMotivo principal: pagou")
	if err == nil || !strings.Contains(err.Error(), "incompleta") {
		t.Fatalf("erro inesperado: %v", err)
	}
}

func TestParseConversationAnalysisValidatesClassifications(t *testing.T) {
	tests := []struct {
		name string
		from string
		to   string
		want string
	}{
		{name: "resultado", from: "Resultado provável: em negociação", to: "Resultado provável: talvez", want: "resultado provável inválido"},
		{name: "qualidade", from: "Qualidade do atendimento: boa", to: "Qualidade do atendimento: excelente", want: "qualidade do atendimento inválida"},
		{name: "confiança", from: "Confiança: alta", to: "Confiança: absoluta", want: "confiança inválida"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseConversationAnalysis(strings.Replace(validAnalysis, tt.from, tt.to, 1))
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("erro = %v; esperado conter %q", err, tt.want)
			}
		})
	}
}

func TestParseConversationAnalysisRemovesMarkdown(t *testing.T) {
	raw := strings.Replace(validAnalysis, "Resultado provável:", "**Resultado provável:**", 1)
	analysis, err := ParseConversationAnalysis(raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.ContainsAny(analysis.NormalizedResponse, "*#") {
		t.Fatalf("Markdown preservado: %q", analysis.NormalizedResponse)
	}
}

func TestParseConversationAnalysisAcceptsElevenPhysicalLines(t *testing.T) {
	raw := `Aqui está a análise:
Resultado provável: em negociação

Motivo principal: o cliente demonstrou interesse,
mas ainda não confirmou o pagamento.
Objeções do cliente: preço
e prazo de pagamento.
Qualidade do atendimento: regular
Próxima ação recomendada: enviar as instruções
e acompanhar a confirmação.
Confiança: média`
	analysis, err := ParseConversationAnalysis(raw)
	if err != nil {
		t.Fatal(err)
	}
	if analysis.MainReason != "o cliente demonstrou interesse, mas ainda não confirmou o pagamento." {
		t.Fatalf("motivo multiline = %q", analysis.MainReason)
	}
	if analysis.RecommendedAction != "enviar as instruções e acompanhar a confirmação." {
		t.Fatalf("ação multiline = %q", analysis.RecommendedAction)
	}
}

func TestParseConversationAnalysisIgnoresMarkdownAndOutsideText(t *testing.T) {
	raw := `Introdução que não pertence aos campos.
## Análise comercial
~~~
**Resultado provável:** em negociação

- Motivo principal: pagamento pendente
  cliente solicitou prazo.
Objeções do cliente: nenhuma
Qualidade do atendimento: boa
Próxima ação recomendada: acompanhar
Confiança: alta
Conclusão fora dos campos.`
	analysis, err := ParseConversationAnalysis(raw)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(analysis.NormalizedResponse, "Introdução") || strings.Contains(analysis.NormalizedResponse, "Conclusão") || strings.Contains(analysis.NormalizedResponse, "Análise comercial") {
		t.Fatalf("texto externo preservado: %q", analysis.NormalizedResponse)
	}
}

func TestParseConversationAnalysisRejectsDuplicateAndEmptyFields(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "duplicado", raw: validAnalysis + "\nRESULTADO PROVÁVEL: venda", want: "duplicado"},
		{name: "vazio", raw: strings.Replace(validAnalysis, "Motivo principal: pagamento pendente", "Motivo principal:", 1), want: "campo vazio"},
		{name: "ausente", raw: strings.Replace(validAnalysis, "Confiança: alta", "", 1), want: "obrigatório ausente"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseConversationAnalysis(tt.raw)
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("erro = %v; esperado conter %q", err, tt.want)
			}
		})
	}
}

func TestParseConversationAnalysisNormalizesEnumerationsAndPreservesRaw(t *testing.T) {
	raw := "  RESULTADO PROVÁVEL:   EM NEGOCIAÇÃO  \nMotivo principal: pagamento pendente\nObjeções do cliente: nenhuma\nQUALIDADE DO ATENDIMENTO:  BOA \nPróxima ação recomendada: acompanhar\nCONFIANÇA: ALTA  "
	analysis, err := ParseConversationAnalysis(raw)
	if err != nil {
		t.Fatal(err)
	}
	if analysis.ProbableResult != "em negociação" || analysis.ServiceQuality != "boa" || analysis.Confidence != "alta" {
		t.Fatalf("enumerações não normalizadas: %#v", analysis)
	}
	if analysis.RawResponse != raw {
		t.Fatalf("raw_response alterada: %q", analysis.RawResponse)
	}
	lines := strings.Split(analysis.NormalizedResponse, "\n")
	if len(lines) != 6 {
		t.Fatalf("linhas normalizadas = %d: %q", len(lines), analysis.NormalizedResponse)
	}
}

func TestInvalidAnalysisResponsePreservesRawForEveryParseFailure(t *testing.T) {
	tests := []struct {
		name string
		raw  string
		want string
	}{
		{name: "resultado", raw: strings.Replace(validAnalysis, "em negociação", "possível venda", 1), want: "invalid_probable_result"},
		{name: "qualidade", raw: strings.Replace(validAnalysis, "boa", "excelente", 1), want: "invalid_service_quality"},
		{name: "confiança", raw: strings.Replace(validAnalysis, "alta", "absoluta", 1), want: "invalid_confidence"},
		{name: "ausente", raw: strings.Replace(validAnalysis, "Confiança: alta", "", 1), want: "missing_field"},
		{name: "duplicado", raw: validAnalysis + "\nResultado provável: venda", want: "duplicate_field"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			_, err := ParseConversationAnalysis(tt.raw)
			var invalid *InvalidAnalysisResponseError
			if !errors.As(err, &invalid) {
				t.Fatalf("errors.As = false: %T %v", err, err)
			}
			if invalid.RawResponse != tt.raw || invalid.Category != tt.want {
				t.Fatalf("erro tipado incorreto: %#v", invalid)
			}
			if strings.Contains(invalid.Error(), tt.raw) {
				t.Fatalf("Error expôs raw_response: %q", invalid.Error())
			}
		})
	}
}

func TestParseConversationAnalysisAcceptsOnlySimpleTerminalPunctuation(t *testing.T) {
	raw := strings.NewReplacer(
		"em negociação", "EM NEGOCIAÇÃO.",
		"boa", "BOA.",
		"alta", "MÉDIA.",
	).Replace(validAnalysis)
	analysis, err := ParseConversationAnalysis(raw)
	if err != nil {
		t.Fatal(err)
	}
	if analysis.ProbableResult != "em negociação" || analysis.ServiceQuality != "boa" || analysis.Confidence != "média" {
		t.Fatalf("pontuação simples não normalizada: %#v", analysis)
	}
	for _, approximate := range []string{"possível venda", "quase venda"} {
		_, err := ParseConversationAnalysis(strings.Replace(validAnalysis, "em negociação", approximate, 1))
		if err == nil {
			t.Fatalf("categoria aproximada aceita: %q", approximate)
		}
	}
}
