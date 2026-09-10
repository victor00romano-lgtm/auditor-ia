package application

import (
	"context"
	"encoding/csv"
	"fmt"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/portfolio/auditor-ia/internal/business/domain"
)

type CSVExporter struct {
	BaseDir string
	Now     func() time.Time
}

func (e CSVExporter) Priorities(ctx context.Context, items []domain.PriorityOpportunity) (string, error) {
	rows := [][]string{{"prioridade", "negocio", "titulo", "responsavel", "etapa", "resultado_final", "resultado_ia", "possivel_comprovante", "dias_parado", "confianca"}}
	for _, v := range items {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		rows = append(rows, []string{strconv.Itoa(v.PriorityScore), strconv.FormatInt(v.BitrixDealID, 10), v.Title, v.Assignee, v.Stage, v.CommercialResult(), v.ProbableResult, strconv.FormatBool(v.PossibleReceipt), strconv.Itoa(v.StaleDays), v.Confidence})
	}
	return e.write("oportunidades-prioritarias", rows)
}
func (e CSVExporter) Divergences(ctx context.Context, items []domain.CRMAIDivergence) (string, error) {
	rows := [][]string{{"tipo", "negocio", "crm", "ia", "confianca", "recomendacao"}}
	for _, v := range items {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		rows = append(rows, []string{string(v.Type), strconv.FormatInt(v.BitrixDealID, 10), v.CRMState, v.ConversationState, v.Confidence, v.Recommendation})
	}
	return e.write("divergencias-crm-ia", rows)
}
func (e CSVExporter) CRMQuality(ctx context.Context, value domain.CRMQualitySummary) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	return e.write("qualidade-crm", [][]string{{"metrica", "valor"}, {"sem_valor", strconv.Itoa(value.WithoutValue)}, {"sem_conversa", strconv.Itoa(value.WithoutConversation)}, {"conversa_vazia", strconv.Itoa(value.EmptyConversation)}, {"acesso_negado", strconv.Itoa(value.AccessDenied)}})
}
func (e CSVExporter) Objections(ctx context.Context, items []domain.ObjectionSummary) (string, error) {
	rows := [][]string{{"categoria", "quantidade", "percentual_basis_points"}}
	for _, v := range items {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		rows = append(rows, []string{v.Label, strconv.Itoa(v.Count), strconv.FormatInt(v.Percentage.Value.BasisPoints, 10)})
	}
	return e.write("objecoes", rows)
}
func (e CSVExporter) Agents(ctx context.Context, items []domain.AgentPerformance) (string, error) {
	rows := [][]string{{"responsavel", "area", "negocios", "abertos", "ganhos", "perdidos", "conversao_basis_points"}}
	for _, v := range items {
		if err := ctx.Err(); err != nil {
			return "", err
		}
		area := v.Area
		if area == "" {
			area = domain.AgentArea(v.Assignee)
		}
		rows = append(rows, []string{v.Assignee, area, strconv.Itoa(v.Deals), strconv.Itoa(v.Open), strconv.Itoa(v.Won), strconv.Itoa(v.Lost), strconv.FormatInt(v.Conversion.Value.BasisPoints, 10)})
	}
	return e.write("equipe", rows)
}
func (e CSVExporter) write(prefix string, rows [][]string) (string, error) {
	now := e.Now
	if now == nil {
		now = time.Now
	}
	base := e.BaseDir
	if base == "" {
		base = "./reports"
	}
	if err := os.MkdirAll(base, 0750); err != nil {
		return "", err
	}
	name := fmt.Sprintf("%s-%s.csv", prefix, now().Format("2006-01-02"))
	path := filepath.Join(base, name)
	if _, err := os.Stat(path); err == nil {
		path = filepath.Join(base, fmt.Sprintf("%s-%s.csv", prefix, now().Format("2006-01-02-150405")))
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0640)
	if err != nil {
		return "", err
	}
	defer file.Close()
	if _, err := file.Write([]byte{0xEF, 0xBB, 0xBF}); err != nil {
		return "", err
	}
	writer := csv.NewWriter(file)
	writer.Comma = ';'
	for _, row := range rows {
		safe := make([]string, len(row))
		for i, v := range row {
			safe[i] = SafeCSV(v)
		}
		if err := writer.Write(safe); err != nil {
			return "", err
		}
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		return "", err
	}
	return path, nil
}
func SafeCSV(value string) string {
	trimmed := strings.TrimLeft(value, " \t\r\n")
	if trimmed != "" && strings.ContainsRune("=+-@", rune(trimmed[0])) {
		return "'" + value
	}
	return value
}
