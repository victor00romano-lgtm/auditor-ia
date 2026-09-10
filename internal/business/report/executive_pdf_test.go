package report

import (
	"bytes"
	"context"
	"strings"
	"testing"
	"time"

	"github.com/portfolio/auditor-ia/internal/business/domain"
	statsdomain "github.com/portfolio/auditor-ia/internal/crmstats/domain"
)

func TestExecutivePDFRendererProducesPolishedMultipageDocument(t *testing.T) {
	report := executiveFixture()
	content, err := (ExecutivePDFRenderer{}).Render(context.Background(), report)
	if err != nil {
		t.Fatal(err)
	}
	if len(content) < 5000 || !bytes.HasPrefix(content, []byte("%PDF")) {
		t.Fatalf("PDF inválido: %d bytes", len(content))
	}
	if pages := bytes.Count(content, []byte("/Type /Page")); pages < 5 {
		t.Fatalf("relatório deveria ser multipágina: %d marcadores", pages)
	}
}

func TestExecutivePDFRendererHonorsCancellation(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := (ExecutivePDFRenderer{}).Render(ctx, executiveFixture()); err == nil {
		t.Fatal("contexto cancelado foi ignorado")
	}
}

func TestExecutiveReportPresentationHelpers(t *testing.T) {
	if got := divergenceLabel(domain.PossibleReceiptNotWon); !strings.Contains(got, "comprovante") {
		t.Fatalf("rótulo inadequado: %q", got)
	}
	if got := periodLabel(domain.GlobalFilters{Period: domain.Period30Days}); got != "Últimos 30 dias | filtros: período=last_30_days" {
		t.Fatalf("período incorreto: %q", got)
	}
}

func TestExecutivePDFIncludesCRMStatisticsWithUTF8Names(t *testing.T) {
	report := executiveFixture()
	report.Dashboard.CRMStatistics = statsdomain.Statistics{Available: true, TotalDeals: 30000, WonDeals: 4100, LostDeals: 9200, LastSyncAt: time.Now(), LastStatus: statsdomain.Completed, ByAssignee: []statsdomain.AssigneeCount{{Name: "Débora Ávila", Count: 7200}, {Name: "Camila Rodrigues", Count: 8500}}, ByMonth: []statsdomain.MonthlyCount{{Month: "2026-08", Count: 932}}, OutcomesByMonth: []statsdomain.MonthlyOutcome{{Month: "2026-08", Won: 120, Lost: 80}}, Marker3264Available: true, Marker3264Total: 4280, Marker3264ByAssignee: []statsdomain.MarkerAssignment{{Name: "Débora Ávila", Count: 2310, PercentageBasis: 5397}}}
	content, err := (ExecutivePDFRenderer{}).Render(context.Background(), report)
	if err != nil || !bytes.HasPrefix(content, []byte("%PDF")) {
		t.Fatalf("PDF CRM inválido: %v", err)
	}
	for _, broken := range []string{"DÃ©bora", "NegÃ³cios", "sincronizaÃ§Ã£o"} {
		if bytes.Contains(content, []byte(broken)) {
			t.Fatalf("PDF contém mojibake %q", broken)
		}
	}
}

func executiveFixture() domain.ExecutiveReport {
	availablePercent := domain.MetricValue[domain.Percentage]{DataAvailability: domain.DataAvailability{Available: true}, Value: domain.Percentage{BasisPoints: 9672}}
	availableScore := domain.MetricValue[string]{DataAvailability: domain.DataAvailability{Available: true}, Value: "81.97"}
	availableHealth := domain.MetricValue[int]{DataAvailability: domain.DataAvailability{Available: true}, Value: 48}
	dashboard := domain.Dashboard{
		Executive:     domain.ExecutiveSummary{Sample: 61, DealsEvaluated: 61, Completed: 59, Failed: 2, CompletionRate: availablePercent, AverageScore: availableScore, Results: map[string]int{"em negociação": 30, "não venda": 9, "indeterminado": 20}, PossibleReceipts: 4, WithoutValue: 61, WithoutConversation: 14, EmptyConversation: 3, AccessDenied: 1},
		CRMQuality:    domain.CRMQualitySummary{Sample: 61, WithoutValue: 61, Stale7: 39, Stale15: 20, Stale30: 10, WithoutConversation: 14, EmptyConversation: 3, AccessDenied: 1, Indeterminate: 20, HealthScore: availableHealth, CoverageNote: "A cobertura financeira está indisponível por ausência de valores."},
		Conversations: domain.ConversationQualitySummary{Sample: 61, Statuses: map[string]int{"AVAILABLE": 43, "NO_CONVERSATION": 14, "EMPTY_CONVERSATION": 3, "ACCESS_DENIED": 1}, Quality: map[string]int{"boa": 12, "regular": 20, "ruim": 3}, Confidence: map[string]int{"alta": 8, "média": 20, "baixa": 15}, MessagesAnalyzed: 1200, MessagesPersisted: 1200, PossibleReceipts: 4},
		Divergences:   []domain.CRMAIDivergence{{Type: domain.PossibleReceiptNotWon, BitrixDealID: 35318, CRMState: "P", ConversationState: "em negociação", Confidence: "alta"}},
		Objections:    []domain.ObjectionSummary{{Label: "Forma de pagamento", Count: 12, Percentage: domain.Percent(12, 61)}},
		Agents:        []domain.AgentPerformance{{Assignee: "Equipe A", Deals: 31, Open: 20, Won: 5, Lost: 6, Conversion: domain.Percent(5, 11)}},
		Channel3264:   []domain.ChannelAssignment{{Assignee: "Débora Fanny", Area: "Vendas", Deals: 35, Lost: 6, LossRate: domain.Percent(6, 35)}, {Assignee: "Solange Silva", Area: "Vendas", Deals: 24, Lost: 3, LossRate: domain.Percent(3, 24)}},
	}
	for i := range 12 {
		dashboard.Priorities = append(dashboard.Priorities, domain.PriorityOpportunity{PriorityScore: 100 - i, BitrixDealID: int64(35000 + i), Stage: "Em negociação", ProbableResult: "em negociação", PossibleReceipt: i%3 == 0, NextAction: "Validar o próximo passo com o cliente e atualizar o CRM."})
	}
	return domain.ExecutiveReport{Dashboard: dashboard, Filters: domain.GlobalFilters{Period: domain.Period30Days}, GeneratedAt: time.Date(2026, 8, 13, 16, 30, 0, 0, time.UTC), Recommendations: []string{"Cadastrar valores no CRM.", "Revisar negócios desatualizados."}}
}
