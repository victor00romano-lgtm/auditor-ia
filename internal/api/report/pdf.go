package report

import (
	"bytes"
	"context"
	"fmt"
	"time"

	"github.com/phpdave11/gofpdf"
	apidomain "github.com/portfolio/auditor-ia/internal/api/domain"
)

type PDFRenderer struct{ Now func() time.Time }

func (renderer PDFRenderer) Render(ctx context.Context, data apidomain.ReportData, executiveSummary string) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	now := renderer.Now
	if now == nil {
		now = time.Now
	}
	pdf := gofpdf.New("P", "mm", "A4", "")
	translate := pdf.UnicodeTranslatorFromDescriptor("")
	pdf.SetMargins(18, 18, 18)
	pdf.SetAutoPageBreak(true, 18)
	pdf.SetFooterFunc(func() {
		pdf.SetY(-12)
		pdf.SetFont("Arial", "", 8)
		pdf.CellFormat(0, 5, translate(footerText(now(), pdf.PageNo())), "", 0, "C", false, 0, "")
	})
	pdf.AddPage()
	heading := func(text string) {
		pdf.SetFont("Arial", "B", 13)
		pdf.Ln(3)
		pdf.MultiCell(0, 7, translate(text), "", "L", false)
	}
	paragraph := func(text string) { pdf.SetFont("Arial", "", 10); pdf.MultiCell(0, 5, translate(text), "", "L", false) }
	pdf.SetFont("Arial", "B", 18)
	pdf.MultiCell(0, 9, translate("Auditor IA — Relatório de Auditoria"), "", "C", false)
	pdf.Ln(4)
	paragraph(fmt.Sprintf("Avaliação: %d\nNegócio Bitrix: %d — %s\nData da auditoria: %s\nStatus: %s\nScore: %s\nResultado final: %s\nFonte: %s", data.Audit.AssessmentID, data.Audit.BitrixDealID, data.Audit.DealTitle, data.Audit.CreatedAt.Format("02/01/2006 15:04"), data.Audit.Status, scoreText(data.Audit.Score), fallback(data.Audit.FinalResult, "indeterminado"), fallback(data.Audit.ResultSource, "não informada")))
	heading("Resumo executivo")
	paragraph(executiveSummary)
	if data.Audit.Summary != "" {
		paragraph(data.Audit.Summary)
	}
	heading("Achados por severidade")
	groups := map[string][]apidomain.Finding{}
	for _, finding := range data.Findings {
		groups[finding.Severity] = append(groups[finding.Severity], finding)
	}
	for _, severity := range []string{"CRITICAL", "HIGH", "MEDIUM", "LOW"} {
		items := groups[severity]
		if len(items) == 0 {
			continue
		}
		pdf.SetFont("Arial", "B", 11)
		pdf.MultiCell(0, 6, fmt.Sprintf("%s (%d)", severity, len(items)), "", "L", false)
		for _, finding := range items {
			paragraph(fmt.Sprintf("• %s — %s", finding.Rule, finding.Description))
		}
	}
	if message, unavailable := unavailableAnalysisText(data.Analysis); unavailable {
		heading("Análise da conversa")
		found, history := pdfConversationStatus(data.Analysis)
		paragraph(fmt.Sprintf("Conversa encontrada: %s\nMensagens analisadas: 0\nHistórico: %s\n%s", found, history, message))
	} else {
		a := data.Analysis
		heading("Análise da conversa")
		paragraph(fmt.Sprintf("Resultado provável: %s\nPossível comprovante: %t\nMotivo principal: %s\nObjeções: %s\nQualidade: %s\nAção recomendada: %s\nConfiança: %s\nModelo: %s\nVersão do prompt: %s", fallback(a.ProbableResult, "indeterminado"), a.PossibleReceipt, fallback(a.MainReason, "não informado"), fallback(a.CustomerObjections, "não informadas"), fallback(a.ServiceQuality, "indeterminada"), fallback(a.RecommendedAction, "não informada"), fallback(a.Confidence, "indeterminada"), fallback(a.Model, "não informado"), fallback(a.PromptVersion, "não informada")))
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var buffer bytes.Buffer
	if err := pdf.Output(&buffer); err != nil {
		return nil, fmt.Errorf("renderizar PDF: %w", err)
	}
	return buffer.Bytes(), nil
}

func pdfConversationStatus(analysis *apidomain.Analysis) (string, string) {
	if analysis == nil {
		return "não", "não localizado"
	}
	switch analysis.ConversationStatus {
	case "ACCESS_DENIED":
		return "sim", "acesso negado pelo Bitrix"
	case "EMPTY_CONVERSATION":
		return "sim", "acessível, sem mensagens humanas analisáveis"
	case "AVAILABLE":
		return "sim", "disponível"
	default:
		return "não", "não localizado"
	}
}

func unavailableAnalysisText(analysis *apidomain.Analysis) (string, bool) {
	if analysis == nil {
		return "Análise de conversa indisponível.", true
	}
	if analysis.UnavailableReason != "" {
		return analysis.UnavailableReason, true
	}
	return "", false
}

func footerText(generatedAt time.Time, page int) string {
	return fmt.Sprintf("Gerado em %s — página %d", generatedAt.Format("02/01/2006 15:04"), page)
}

func scoreText(score *float64) string {
	if score == nil {
		return "não calculado"
	}
	return fmt.Sprintf("%.0f/100", *score)
}
func fallback(value, fallback string) string {
	if value == "" {
		return fallback
	}
	return value
}

var _ apidomain.ReportRenderer = PDFRenderer{}
