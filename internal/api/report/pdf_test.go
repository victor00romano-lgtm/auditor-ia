package report

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	apidomain "github.com/portfolio/auditor-ia/internal/api/domain"
)

func TestPDFRendererProducesPDFWithPortugueseContent(t *testing.T) {
	score := 80.0
	data := apidomain.ReportData{Audit: apidomain.Audit{AssessmentID: 10, BitrixDealID: 8620, DealTitle: "Negócio ação", Status: "COMPLETED", Score: &score, CreatedAt: time.Now()}, Findings: []apidomain.Finding{{Severity: "HIGH", Rule: "regra", Description: strings.Repeat("Descrição longa com acentuação. ", 40)}}}
	content, err := (PDFRenderer{Now: func() time.Time { return time.Date(2026, 8, 10, 12, 0, 0, 0, time.UTC) }}).Render(context.Background(), data, "Resumo determinístico")
	if err != nil {
		t.Fatal(err)
	}
	if len(content) < 100 || string(content[:4]) != "%PDF" {
		t.Fatalf("assinatura PDF inválida: %q", content[:min(4, len(content))])
	}
	for _, mojibake := range []string{
		string([]rune{0x00e2, 0x20ac, 0x201d}),
		string([]rune{'p', 0x00c3, 0x00a1, 'g', 'i', 'n', 'a'}),
	} {
		if strings.Contains(footerText(time.Date(2026, 8, 10, 15, 7, 0, 0, time.UTC), 1), mojibake) || bytes.Contains(content, []byte(mojibake)) {
			t.Fatalf("PDF contém mojibake %q", mojibake)
		}
	}
	if got := footerText(time.Date(2026, 8, 10, 15, 7, 0, 0, time.UTC), 1); got != "Gerado em 10/08/2026 15:07 — página 1" {
		t.Fatalf("rodapé incorreto: %q", got)
	}
}

func TestUnavailableConversationIsShownInPDF(t *testing.T) {
	want := "Análise de conversa indisponível por ausência de mensagens IMOPENLINES e formulário CRM."
	message, unavailable := unavailableAnalysisText(&apidomain.Analysis{UnavailableReason: want})
	if !unavailable || message != want {
		t.Fatalf("estado indisponível não propagado ao PDF: unavailable=%t message=%q", unavailable, message)
	}
}

func TestAccessDeniedConversationIsNotReportedAsMissingInPDF(t *testing.T) {
	analysis := &apidomain.Analysis{ConversationStatus: "ACCESS_DENIED", UnavailableReason: "Conversa localizada, mas o Bitrix negou acesso ao histórico."}
	found, history := pdfConversationStatus(analysis)
	if found != "sim" || history != "acesso negado pelo Bitrix" {
		t.Fatalf("estado incorreto no PDF: found=%q history=%q", found, history)
	}
	content, err := (PDFRenderer{}).Render(context.Background(), apidomain.ReportData{Audit: apidomain.Audit{AssessmentID: 1, BitrixDealID: 52, Status: "COMPLETED", CreatedAt: time.Now()}, Analysis: analysis}, "Resumo")
	if err != nil || !bytes.HasPrefix(content, []byte("%PDF")) {
		t.Fatalf("PDF ACCESS_DENIED inválido: err=%v", err)
	}
}

func TestPDFUsesDeterministicCommercialResult(t *testing.T) {
	for _, audit := range []apidomain.Audit{
		{AssessmentID: 1, BitrixDealID: 34710, Status: "COMPLETED", FinalResult: "pós-venda/suporte", ResultSource: "responsável de suporte no Bitrix", CreatedAt: time.Now()},
		{AssessmentID: 2, BitrixDealID: 34720, Status: "COMPLETED", FinalResult: "não venda", ResultSource: "estágio perdido no Bitrix", CreatedAt: time.Now()},
		{AssessmentID: 3, BitrixDealID: 34734, Status: "COMPLETED", FinalResult: "não venda", ResultSource: "estágio perdido no Bitrix", CreatedAt: time.Now()},
	} {
		content, err := (PDFRenderer{}).Render(context.Background(), apidomain.ReportData{Audit: audit}, "Resumo")
		if err != nil || !bytes.HasPrefix(content, []byte("%PDF")) {
			t.Fatalf("negócio %d PDF inválido: %v", audit.BitrixDealID, err)
		}
	}
}
