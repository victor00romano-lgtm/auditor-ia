package domain

import (
	"strings"
	"testing"
	"time"
)

func TestPercentAndUnavailable(t *testing.T) {
	if got := Percent(1, 0); got.Available || got.Reason == "" {
		t.Fatalf("divisão por zero: %#v", got)
	}
	got := Percent(1, 4)
	if !got.Available || got.Value.BasisPoints != 2500 {
		t.Fatalf("percentual: %#v", got)
	}
}
func TestMoneyPreservesMinorUnits(t *testing.T) {
	got := ParseMoney("123.45", "BRL")
	if !got.Available || got.Value.MinorUnits != 12345 {
		t.Fatalf("dinheiro: %#v", got)
	}
}
func TestMoneyZeroIsUnavailable(t *testing.T) {
	got := ParseMoney("0.00", "BRL")
	if got.Available || got.Reason == "" {
		t.Fatalf("valor zero deveria ser indisponível: %#v", got)
	}
}
func TestPipelineWithOnlyZeroValuesIsUnavailable(t *testing.T) {
	pipeline := ParseMoney("0", "BRL")
	if pipeline.Available || pipeline.Reason != "Valor não informado ou igual a zero" {
		t.Fatalf("pipeline totalmente sem valores: %#v", pipeline)
	}
}
func TestPriorityScoreIsCapped(t *testing.T) {
	got := PriorityScore(PriorityOpportunity{AnalysisStatus: "COMPLETED", PossibleReceipt: true, ExplicitPaymentEvidence: true, ProbableResult: "venda", StaleDays: 30, Confidence: "alta"})
	if got != 100 {
		t.Fatalf("priority_score=%d", got)
	}
}
func TestCompletionRateUsesAllEvaluatedDeals(t *testing.T) {
	got := Percent(59, 61)
	if !got.Available || got.Value.BasisPoints != 9672 || got.Value.String() != "96.72%" {
		t.Fatalf("taxa 59/61 incorreta: %#v (%s)", got, got.Value.String())
	}
}
func TestPriorityDeal35318BeatsFailedAnalysis(t *testing.T) {
	deal35318 := PriorityOpportunity{BitrixDealID: 35318, AnalysisStatus: "COMPLETED", PossibleReceipt: true, ExplicitPaymentEvidence: true, Confidence: "alta", Evidence: "Pagamento já realizado.", NextAction: "Confirmar atualização no CRM"}
	failed := PriorityOpportunity{BitrixDealID: 34746, AnalysisStatus: "FAILED", PossibleReceipt: true, Confidence: "alta", StaleDays: 30}
	if got, failedScore := PriorityScore(deal35318), PriorityScore(failed); got != 80 || got <= failedScore {
		t.Fatalf("prioridades incorretas: 35318=%d failed=%d", got, failedScore)
	}
}
func TestFailedAnalysisGetsNoReceiptOrConfidenceBonus(t *testing.T) {
	got := PriorityScore(PriorityOpportunity{AnalysisStatus: "FAILED", PossibleReceipt: true, ExplicitPaymentEvidence: true, Confidence: "alta"})
	if got != 0 {
		t.Fatalf("análise falhada recebeu bônus: %d", got)
	}
}
func TestHealthScore(t *testing.T) {
	if got := HealthScore(0, 0, 0, 0, 0, 0); got.Available {
		t.Fatal("amostra vazia deveria ser indisponível")
	}
	got := HealthScore(100, 20, 10, 10, 10, 10)
	if !got.Available || got.Value < 0 || got.Value > 100 {
		t.Fatalf("health=%#v", got)
	}
}
func TestResolvePeriods(t *testing.T) {
	now := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	from, to := ResolvePeriod(PeriodPreviousMonth, now)
	if from.Month() != 7 || to.Month() != 8 {
		t.Fatalf("período: %v %v", from, to)
	}
}
func TestSparklineAndObjections(t *testing.T) {
	if got := Sparkline([]int{1, 2, 3}); !strings.Contains(got, "█") {
		t.Fatalf("sparkline=%q", got)
	}
	if got := NormalizeObjection("Preço muito caro"); got != "PRICE" {
		t.Fatalf("objeção=%q", got)
	}
}
func TestGlobalFiltersSummary(t *testing.T) {
	got := (GlobalFilters{Period: Period30Days, OnlyOpen: true}).ActiveSummary()
	if !strings.Contains(got, "30") || !strings.Contains(got, "abertos") {
		t.Fatalf("filtros=%q", got)
	}
}

func TestAgentAreaBusinessRule(t *testing.T) {
	for _, name := range []string{"Camila Rodrigues", "  camila   rodrigues ", "CAMILA RODRIGUES"} {
		if got := AgentArea(name); got != "Suporte" {
			t.Fatalf("%q deveria ser Suporte, recebeu %q", name, got)
		}
	}
	if got := AgentArea("Flávia Rita"); got != "Vendas" {
		t.Fatalf("demais responsáveis deveriam ser Vendas: %q", got)
	}
}

func TestPriorityCommercialResultUsesFinalResult(t *testing.T) {
	for _, dealID := range []int64{34720, 34734} {
		item := PriorityOpportunity{BitrixDealID: dealID, ProbableResult: "em negociação", FinalResult: "não venda"}
		if got := item.CommercialResult(); got != "não venda" {
			t.Fatalf("negócio %d resultado=%q", dealID, got)
		}
	}
	item := PriorityOpportunity{BitrixDealID: 34710, ProbableResult: "indeterminado", FinalResult: "pós-venda/suporte"}
	if got := item.CommercialResult(); got != "pós-venda/suporte" {
		t.Fatalf("negócio 34710 resultado=%q", got)
	}
}

func TestChannelLossRate(t *testing.T) {
	debora, solange := Percent(6, 35), Percent(3, 24)
	if debora.Value.BasisPoints != 1714 || solange.Value.BasisPoints != 1250 {
		t.Fatalf("taxas do canal 3264 incorretas: %s e %s", debora.Value.String(), solange.Value.String())
	}
}
