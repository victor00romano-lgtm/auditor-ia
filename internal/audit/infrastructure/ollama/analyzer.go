package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"sort"
	"strings"
	"time"

	"github.com/portfolio/auditor-ia/internal/audit/domain"
)

type Analyzer struct {
	URL, Model string
	Client     *http.Client
}

func (a Analyzer) Analyze(ctx context.Context, audit *domain.Audit) (string, error) {

	counts := make(map[string]int)
	for _, finding := range audit.Findings {
		counts[finding.Description]++
	}
	descriptions := make([]string, 0, len(counts))
	for description := range counts {
		descriptions = append(descriptions, description)
	}

	sort.Strings(descriptions)

	var summary strings.Builder
	for _, description := range descriptions {
		fmt.Fprintf(
			&summary,
			"- %s: %d ocorrências\n",
			description,
			counts[description],
		)
	}

	prompt := fmt.Sprintf(`Você é um auditor de negócios do Bitrix24.

		Foram encontrados %s problemas.
		Analise os riscos gerais sem comentar cada negócio individualmente.
		Responda em português, sem Markdown, usando:
		Regras obrigatórias de classificação:

		- pós-venda/suporte: atendimento após uma compra, matrícula ou contratação já realizada; inclui certificado, acesso, entrega, reclamação, suporte e reembolso.
		- em negociação: conversa comercial anterior à compra ou contratação.
		- venda: existe confirmação explícita de compra, pagamento ou contratação.
		- não venda: existe recusa, desistência ou perda explícita.
		- indeterminado: não há evidência suficiente.

		Prioridade: se o cliente já comprou ou concluiu o serviço e busca atendimento, classifique como pós-venda/suporte, mesmo que o negócio esteja aberto no CRM.
		
		Responda uma única vez com exatamente 3 linhas:
		Risco:
		Impacto:
		Ação recomendada:`,
		summary.String(),
	)
	body, err := json.Marshal(map[string]any{
		"model":      a.Model,
		"prompt":     prompt,
		"stream":     false,
		"keep_alive": "10m",
		"options": map[string]any{
			"temperature": 0.1,
			"num_predict": 300,
			"stop":        []string{"\n\n"},
		},
	})
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(
		ctx,
		http.MethodPost,
		a.URL+"/api/generate",
		bytes.NewReader(body),
	)
	req.Header.Set("Content-Type", "application/json")
	client := a.Client
	if client == nil {
		client = &http.Client{Timeout: 2 * time.Minute}
	}
	resp, err := client.Do(req)
	if err != nil {
		return "", err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		return "", fmt.Errorf("Ollama HTTP %d", resp.StatusCode)
	}
	var result struct {
		Response string `json:"response"`
	}
	if err = json.NewDecoder(resp.Body).Decode(&result); err != nil {
		return "", err
	}
	return result.Response, nil
}
