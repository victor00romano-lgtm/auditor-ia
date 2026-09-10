package domain

import (
	"errors"
	"fmt"
	"strings"
)

const PromptVersion = "conversation-v1"

var analysisLabels = []string{
	"Resultado provável:",
	"Motivo principal:",
	"Objeções do cliente:",
	"Qualidade do atendimento:",
	"Próxima ação recomendada:",
	"Confiança:",
}

type ConversationAnalysis struct {
	ProbableResult     string
	MainReason         string
	CustomerObjections string
	ServiceQuality     string
	RecommendedAction  string
	Confidence         string
	RawResponse        string
	NormalizedResponse string
	PossibleReceipt    bool
	Model              string
	PromptVersion      string
}

type InvalidAnalysisResponseError struct {
	RawResponse     string
	Cause           error
	Category        string
	Model           string
	PromptVersion   string
	MessageCount    int
	PossibleReceipt bool
}

func (err *InvalidAnalysisResponseError) Error() string {
	if err == nil {
		return "resposta do Ollama inválida"
	}
	if err.Cause == nil {
		return "resposta do Ollama inválida"
	}
	message := err.Cause.Error()
	if err.RawResponse != "" {
		message = strings.ReplaceAll(message, err.RawResponse, "[conteúdo omitido]")
	}
	return "resposta do Ollama inválida: " + message
}

func (err *InvalidAnalysisResponseError) Unwrap() error {
	if err == nil {
		return nil
	}
	return err.Cause
}

func newInvalidAnalysisResponse(raw, category string, cause error) error {
	return &InvalidAnalysisResponseError{RawResponse: raw, Cause: cause, Category: category}
}

func ParseConversationAnalysis(raw string) (ConversationAnalysis, error) {
	values := make([]string, len(analysisLabels))
	seen := make([]bool, len(analysisLabels))
	current := -1

	for _, rawLine := range strings.Split(normalizeNewlines(raw), "\n") {
		line := normalizeAnalysisLine(rawLine)
		if line == "" {
			continue
		}
		index, value, matched := matchAnalysisField(line)
		if matched {
			if seen[index] {
				return ConversationAnalysis{}, newInvalidAnalysisResponse(raw, "duplicate_field", fmt.Errorf("campo duplicado: %s", strings.TrimSuffix(analysisLabels[index], ":")))
			}
			seen[index] = true
			values[index] = value
			current = index
			continue
		}
		if strings.HasPrefix(strings.TrimSpace(rawLine), "#") {
			continue
		}

		// Prose outside the six semantic fields is not part of the contract.
		if current < 0 || current == len(analysisLabels)-1 || allFieldsSeen(seen) {
			continue
		}
		// Enumerated fields never absorb prose: accepting it could turn an
		// unsupported category into an apparently valid classification.
		if current == 0 || current == 3 || current == 5 {
			continue
		}
		values[current] = strings.TrimSpace(strings.Join([]string{values[current], line}, " "))
	}

	for index, present := range seen {
		field := strings.TrimSuffix(analysisLabels[index], ":")
		if !present {
			return ConversationAnalysis{}, newInvalidAnalysisResponse(raw, "missing_field", fmt.Errorf("resposta incompleta: campo obrigatório ausente: %s", field))
		}
		values[index] = strings.TrimSpace(values[index])
		if values[index] == "" {
			return ConversationAnalysis{}, newInvalidAnalysisResponse(raw, "empty_field", fmt.Errorf("resposta incompleta: campo vazio: %s", field))
		}
	}

	analysis := ConversationAnalysis{
		ProbableResult:     normalizeEnumeratedValue(values[0]),
		MainReason:         values[1],
		CustomerObjections: values[2],
		ServiceQuality:     normalizeEnumeratedValue(values[3]),
		RecommendedAction:  values[4],
		Confidence:         normalizeEnumeratedValue(values[5]),
		RawResponse:        raw,
	}
	if !oneOf(analysis.ProbableResult, "venda", "não venda", "em negociação", "pós-venda/suporte", "indeterminado") {
		return ConversationAnalysis{}, newInvalidAnalysisResponse(raw, "invalid_probable_result", errors.New("resultado provável inválido"))
	}
	if !oneOf(analysis.ServiceQuality, "boa", "regular", "ruim", "indeterminada") {
		return ConversationAnalysis{}, newInvalidAnalysisResponse(raw, "invalid_service_quality", errors.New("qualidade do atendimento inválida"))
	}
	if !oneOf(analysis.Confidence, "baixa", "média", "alta") {
		return ConversationAnalysis{}, newInvalidAnalysisResponse(raw, "invalid_confidence", errors.New("confiança inválida"))
	}
	analysis.NormalizedResponse = analysis.Format()
	return analysis, nil
}

func normalizeEnumeratedValue(value string) string {
	value = strings.ToLower(strings.TrimSpace(value))
	return strings.TrimSpace(strings.TrimSuffix(value, "."))
}

func normalizeNewlines(value string) string {
	return strings.ReplaceAll(strings.ReplaceAll(value, "\r\n", "\n"), "\r", "\n")
}

func normalizeAnalysisLine(raw string) string {
	line := strings.TrimSpace(raw)
	if line == "" || strings.HasPrefix(line, "```") || isDecorativeLine(line) {
		return ""
	}
	line = strings.TrimSpace(strings.TrimLeft(line, "#>"))
	line = strings.TrimSpace(strings.TrimLeft(line, "-• 0123456789."))
	line = strings.ReplaceAll(line, "**", "")
	line = strings.ReplaceAll(line, "__", "")
	line = strings.ReplaceAll(line, "`", "")
	return strings.TrimSpace(line)
}

func isDecorativeLine(line string) bool {
	trimmed := strings.TrimSpace(line)
	return trimmed == "" || strings.Trim(trimmed, "-*_#=~• ") == ""
}

func matchAnalysisField(line string) (int, string, bool) {
	lower := strings.ToLower(line)
	for index, label := range analysisLabels {
		if strings.HasPrefix(lower, strings.ToLower(label)) {
			return index, strings.TrimSpace(line[len(label):]), true
		}
	}
	return -1, "", false
}

func allFieldsSeen(seen []bool) bool {
	for _, present := range seen {
		if !present {
			return false
		}
	}
	return true
}

func (analysis ConversationAnalysis) Format() string {
	values := []string{
		analysis.ProbableResult,
		analysis.MainReason,
		analysis.CustomerObjections,
		analysis.ServiceQuality,
		analysis.RecommendedAction,
		analysis.Confidence,
	}
	lines := make([]string, len(analysisLabels))
	for index, label := range analysisLabels {
		lines[index] = label + " " + values[index]
	}
	return strings.Join(lines, "\n")
}

func oneOf(value string, accepted ...string) bool {
	for _, candidate := range accepted {
		if value == candidate {
			return true
		}
	}
	return false
}
