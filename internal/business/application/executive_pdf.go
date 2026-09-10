package application

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/portfolio/auditor-ia/internal/business/domain"
)

type ExecutiveReportLoader interface {
	Load(context.Context, domain.GlobalFilters) (domain.Dashboard, error)
}

type ExecutiveReportRenderer interface {
	Render(context.Context, domain.ExecutiveReport) ([]byte, error)
}

type ExecutivePDFExporter struct {
	Loader   ExecutiveReportLoader
	Renderer ExecutiveReportRenderer
	BaseDir  string
	Now      func() time.Time
}

func (e ExecutivePDFExporter) Export(ctx context.Context, filters domain.GlobalFilters) (string, error) {
	if e.Loader == nil || e.Renderer == nil {
		return "", fmt.Errorf("exportação executiva não configurada")
	}
	if err := ctx.Err(); err != nil {
		return "", err
	}
	dashboard, err := e.Loader.Load(ctx, filters)
	if err != nil {
		return "", fmt.Errorf("carregar dashboard executivo: %w", err)
	}
	now := e.Now
	if now == nil {
		now = time.Now
	}
	generatedAt := now()
	report := domain.ExecutiveReport{
		Dashboard:       dashboard,
		Filters:         filters,
		GeneratedAt:     generatedAt,
		Recommendations: ExecutiveRecommendations(dashboard),
	}
	content, err := e.Renderer.Render(ctx, report)
	if err != nil {
		return "", fmt.Errorf("gerar relatorio executivo: %w", err)
	}
	if len(content) < 4 || string(content[:4]) != "%PDF" {
		return "", fmt.Errorf("renderer retornou um PDF inválido")
	}

	base := strings.TrimSpace(e.BaseDir)
	if base == "" {
		base = "./reports"
	}
	directory, err := filepath.Abs(filepath.Clean(base))
	if err != nil {
		return "", fmt.Errorf("resolver pasta de relatorios: %w", err)
	}
	if err := os.MkdirAll(directory, 0o750); err != nil {
		return "", fmt.Errorf("criar pasta de relatorios: %w", err)
	}

	path := filepath.Join(directory, "relatorio-executivo-"+generatedAt.Format("2006-01-02")+".pdf")
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	if os.IsExist(err) {
		path = filepath.Join(directory, "relatorio-executivo-"+generatedAt.Format("2006-01-02-150405")+".pdf")
		file, err = os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o640)
	}
	if err != nil {
		return "", fmt.Errorf("criar arquivo executivo: %w", err)
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("salvar relatorio executivo: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("finalizar relatorio executivo: %w", err)
	}
	return path, nil
}

func ExecutiveRecommendations(d domain.Dashboard) []string {
	var recommendations []string
	add := func(condition bool, text string) {
		if condition && len(recommendations) < 7 {
			recommendations = append(recommendations, text)
		}
	}
	q, e := d.CRMQuality, d.Executive
	add(e.WithoutValue > 0, fmt.Sprintf("Cadastrar valor em %d negócio(s) para recuperar indicadores de pipeline, receita e ticket médio.", e.WithoutValue))
	add(q.Stale7 > 0, fmt.Sprintf("Revisar %d negócio(s) sem atualização há mais de 7 dias e definir próxima ação com prazo.", q.Stale7))
	add(e.PossibleReceipts > 0, fmt.Sprintf("Validar %d possível(is) comprovante(s) e atualizar imediatamente o resultado no CRM.", e.PossibleReceipts))
	add(e.WithoutConversation+e.EmptyConversation+e.AccessDenied > 0, fmt.Sprintf("Corrigir a cobertura de conversas: %d sem conversa, %d vazia(s) e %d com acesso negado.", e.WithoutConversation, e.EmptyConversation, e.AccessDenied))
	add(q.Indeterminate > 0, fmt.Sprintf("Revisar %d análise(s) indeterminada(s) e melhorar o registro do contexto comercial.", q.Indeterminate))
	add(e.Failed > 0, fmt.Sprintf("Reprocessar %d avaliação(ões) com falha e acompanhar a causa operacional.", e.Failed))
	add(len(d.Divergences) > 0, fmt.Sprintf("Tratar %d divergência(s) entre CRM e IA antes de usar os dados em decisões de receita.", len(d.Divergences)))
	if len(recommendations) == 0 {
		recommendations = append(recommendations, "Manter a rotina de auditoria e acompanhar variações de conversão, qualidade do CRM e cobertura das conversas.")
	}
	return recommendations
}
