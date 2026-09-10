package domain

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"time"

	statsdomain "github.com/portfolio/auditor-ia/internal/crmstats/domain"
)

type DataAvailability struct {
	Available bool   `json:"available"`
	Reason    string `json:"reason,omitempty"`
}

type MetricValue[T any] struct {
	DataAvailability
	Value T `json:"value,omitempty"`
}

type Money struct {
	MinorUnits int64  `json:"minor_units"`
	Currency   string `json:"currency"`
}

func (m Money) String() string {
	return fmt.Sprintf("%s %d,%02d", fallback(m.Currency, "BRL"), m.MinorUnits/100, abs(m.MinorUnits%100))
}

type Percentage struct {
	BasisPoints int64 `json:"basis_points"`
}

func (p Percentage) String() string { return fmt.Sprintf("%.2f%%", float64(p.BasisPoints)/100) }

type DashboardPeriod string

const (
	PeriodToday         DashboardPeriod = "today"
	PeriodYesterday     DashboardPeriod = "yesterday"
	Period7Days         DashboardPeriod = "last_7_days"
	Period30Days        DashboardPeriod = "last_30_days"
	PeriodCurrentMonth  DashboardPeriod = "current_month"
	PeriodPreviousMonth DashboardPeriod = "previous_month"
	PeriodCustom        DashboardPeriod = "custom"
	PeriodAll           DashboardPeriod = "all"
)

type SortOrder string

const (
	SortAscending  SortOrder = "asc"
	SortDescending SortOrder = "desc"
)

type Pagination struct{ Page, PageSize, Total int }

type GlobalFilters struct {
	Period                                                                       DashboardPeriod `json:"period"`
	DateFrom                                                                     *time.Time      `json:"date_from,omitempty"`
	DateTo                                                                       *time.Time      `json:"date_to,omitempty"`
	Stage, Semantic, Assignee, FinalResult, ProbableResult                       string
	Severity, Rule, ServiceQuality, Confidence, Origin, ConversationStatus       string
	OnlyPossibleReceipts, OnlyDivergences, OnlyOpen, OnlyWithoutValue, OnlyStale bool
}

func (f GlobalFilters) ActiveSummary() string {
	var active []string
	if f.Period != "" && f.Period != PeriodAll {
		active = append(active, "período="+string(f.Period))
	}
	for _, pair := range [][2]string{{"etapa", f.Stage}, {"responsável", f.Assignee}, {"resultado", f.FinalResult}, {"conversa", f.ConversationStatus}} {
		if pair[1] != "" {
			active = append(active, pair[0]+"="+pair[1])
		}
	}
	if f.OnlyOpen {
		active = append(active, "somente abertos")
	}
	if f.OnlyWithoutValue {
		active = append(active, "sem valor")
	}
	if f.OnlyDivergences {
		active = append(active, "divergências")
	}
	if len(active) == 0 {
		return "nenhum"
	}
	return strings.Join(active, " · ")
}

func ResolvePeriod(period DashboardPeriod, now time.Time) (from, to *time.Time) {
	local := now
	startDay := time.Date(local.Year(), local.Month(), local.Day(), 0, 0, 0, 0, local.Location())
	set := func(a, b time.Time) (*time.Time, *time.Time) { return &a, &b }
	switch period {
	case PeriodToday:
		return set(startDay, startDay.AddDate(0, 0, 1))
	case PeriodYesterday:
		return set(startDay.AddDate(0, 0, -1), startDay)
	case Period7Days:
		return set(startDay.AddDate(0, 0, -6), startDay.AddDate(0, 0, 1))
	case Period30Days:
		return set(startDay.AddDate(0, 0, -29), startDay.AddDate(0, 0, 1))
	case PeriodCurrentMonth:
		a := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, local.Location())
		return set(a, a.AddDate(0, 1, 0))
	case PeriodPreviousMonth:
		b := time.Date(local.Year(), local.Month(), 1, 0, 0, 0, 0, local.Location())
		return set(b.AddDate(0, -1, 0), b)
	default:
		return nil, nil
	}
}

type ExecutiveSummary struct {
	Sample, DealsSynced, DealsEvaluated, Completed, Failed             int
	CompletionRate                                                     MetricValue[Percentage]
	AverageScore                                                       MetricValue[string]
	Results                                                            map[string]int
	PossibleReceipts, PossibleUnregisteredSales, LostStillNegotiating  int
	Pipeline, WonRevenue, LostRevenue, AverageTicket, WeightedValue    MetricValue[Money]
	Conversion                                                         MetricValue[Percentage]
	WithoutValue, WithoutConversation, EmptyConversation, AccessDenied int
	CoverageNote                                                       string
}

type FunnelStage struct {
	Stage        string
	Deals, Stale int
	Value        MetricValue[Money]
	Conversion   MetricValue[Percentage]
	AverageDays  MetricValue[string]
}
type FunnelSummary struct {
	Sample        int
	Stages        []FunnelStage
	Won, Lost     int
	LossRate      MetricValue[Percentage]
	WeightedValue MetricValue[Money]
	Note          string
}
type PriorityOpportunity struct {
	PriorityScore                         int
	BitrixDealID                          int64
	Title, Assignee, Stage                string
	Value                                 MetricValue[Money]
	ProbableResult, FinalResult, Evidence string
	PossibleReceipt                       bool
	UpdatedAt                             time.Time
	StaleDays                             int
	NextAction, Confidence                string
	AuditScore                            int
	AnalysisStatus                        string
	ExplicitPaymentEvidence               bool
}

func (item PriorityOpportunity) CommercialResult() string {
	if strings.TrimSpace(item.FinalResult) != "" {
		return item.FinalResult
	}
	return item.ProbableResult
}

type DivergenceType string

const (
	AISaleCRMOpen                DivergenceType = "AI_SALE_CRM_OPEN"
	AISaleCRMLost                DivergenceType = "AI_SALE_CRM_LOST"
	CRMWonWithoutEvidence        DivergenceType = "CRM_WON_WITHOUT_CONVERSATION_EVIDENCE"
	CRMLostAINegotiating         DivergenceType = "CRM_LOST_AI_NEGOTIATING"
	PossibleReceiptNotWon        DivergenceType = "POSSIBLE_RECEIPT_NOT_WON"
	ConversationWithoutCRMUpdate DivergenceType = "CONVERSATION_WITHOUT_CRM_UPDATE"
	FinalResultConflict          DivergenceType = "FINAL_RESULT_CONFLICT"
	PaymentEvidenceWithoutValue  DivergenceType = "PAYMENT_EVIDENCE_WITHOUT_VALUE"
	OutsideChannelSuspected      DivergenceType = "OUTSIDE_CHANNEL_SUSPECTED"
	PaymentTextDetectorConflict  DivergenceType = "PAYMENT_TEXT_DETECTOR_CONFLICT"
)

type CRMAIDivergence struct {
	Type                                                                        DivergenceType
	BitrixDealID                                                                int64
	CRMState, ConversationState, Evidence, Confidence, Recommendation, Assignee string
	PossibleReceipt                                                             bool
	Date                                                                        time.Time
}
type CRMQualitySummary struct {
	Sample, WithoutValue, WithoutAssignee, Stale7, Stale15, Stale30, WithoutConversation, EmptyConversation, AccessDenied, Indeterminate, MessageCountMismatch int
	HealthScore                                                                                                                                                MetricValue[int]
	CoverageNote                                                                                                                                               string
}
type ConversationQualitySummary struct {
	Sample                                                 int
	Statuses, Quality, Confidence                          map[string]int
	MessagesCollected, MessagesAnalyzed, MessagesPersisted int
	AverageMessages                                        MetricValue[string]
	MedianMessages                                         MetricValue[string]
	PossibleReceipts                                       int
	CoverageAlert                                          string
}
type ObjectionSummary struct {
	Category, Label string
	Count           int
	Percentage      MetricValue[Percentage]
	PotentialValue  MetricValue[Money]
	ExampleDealIDs  []int64
}
type AgentPerformance struct {
	Assignee, Area                                              string
	Deals, Open, Won, Lost, Stale, Divergences, PendingReceipts int
	Conversion                                                  MetricValue[Percentage]
	Revenue, Pipeline, AverageTicket                            MetricValue[Money]
	Note                                                        string
}

type ChannelAssignment struct {
	Assignee string
	Area     string
	Deals    int
	Lost     int
	LossRate MetricValue[Percentage]
}

func AgentArea(assignee string) string {
	name := strings.Join(strings.Fields(strings.TrimSpace(assignee)), " ")
	if strings.EqualFold(name, "Camila Rodrigues") {
		return "Suporte"
	}
	return "Vendas"
}

type MonthlyTrend struct {
	Month                                              string
	NewDeals, Won, Lost, PossibleReceipts, Divergences int
	Conversion                                         MetricValue[Percentage]
	Revenue                                            MetricValue[Money]
	AverageScore                                       MetricValue[string]
}
type AIQualitySummary struct {
	Sample, Completed, Failed, Invalid, Incomplete, PossibleReceipts, Divergences int
	SuccessRate                                                                   MetricValue[Percentage]
	Models, PromptVersions, Confidence, ConversationStatuses, FailureCategories   map[string]int
	DurationAverage, DurationP50, DurationP95, DurationMax                        MetricValue[string]
}

type Dashboard struct {
	Executive     ExecutiveSummary
	Funnel        FunnelSummary
	Priorities    []PriorityOpportunity
	Divergences   []CRMAIDivergence
	CRMQuality    CRMQualitySummary
	Conversations ConversationQualitySummary
	Objections    []ObjectionSummary
	Agents        []AgentPerformance
	Channel3264   []ChannelAssignment
	Trends        []MonthlyTrend
	AIQuality     AIQualitySummary
	CRMStatistics statsdomain.Statistics
	LoadedAt      time.Time
}

// ExecutiveReport is the presentation-neutral snapshot used by executive
// report renderers. Raw conversations and AI responses never cross this boundary.
type ExecutiveReport struct {
	Dashboard       Dashboard
	Filters         GlobalFilters
	GeneratedAt     time.Time
	Recommendations []string
}

type Repository interface {
	Load(context.Context, GlobalFilters) (Dashboard, error)
}

func PriorityScore(item PriorityOpportunity) int {
	score := 0
	analysisValid := item.AnalysisStatus == "COMPLETED"
	if analysisValid && item.PossibleReceipt {
		score += 40
	}
	if analysisValid && item.ExplicitPaymentEvidence {
		score += 30
	}
	if analysisValid && item.ProbableResult == "venda" {
		score += 35
	}
	if item.StaleDays > 7 {
		score += 20
	}
	if analysisValid && item.Confidence == "alta" {
		score += 10
	}
	if analysisValid && strings.TrimSpace(item.NextAction) == "" {
		score += 10
	}
	if score > 100 {
		return 100
	}
	return score
}

func HealthScore(sample, withoutValue, stale, withoutConversation, indeterminate, divergences int) MetricValue[int] {
	if sample <= 0 {
		return MetricValue[int]{DataAvailability: DataAvailability{Reason: "Nenhum negócio avaliado no período"}}
	}
	penalty := (withoutValue*25 + stale*25 + withoutConversation*20 + indeterminate*15 + divergences*15) / sample
	if penalty > 100 {
		penalty = 100
	}
	return MetricValue[int]{DataAvailability: DataAvailability{Available: true}, Value: 100 - penalty}
}

func Percent(part, total int) MetricValue[Percentage] {
	if total <= 0 {
		return MetricValue[Percentage]{DataAvailability: DataAvailability{Reason: "Amostra insuficiente"}}
	}
	return MetricValue[Percentage]{DataAvailability: DataAvailability{Available: true}, Value: Percentage{BasisPoints: int64(part * 10000 / total)}}
}

func ParseMoney(value, currency string) MetricValue[Money] {
	if strings.TrimSpace(value) == "" {
		return MetricValue[Money]{DataAvailability: DataAvailability{Reason: "Valor não informado"}}
	}
	f, err := strconv.ParseFloat(value, 64)
	if err != nil {
		return MetricValue[Money]{DataAvailability: DataAvailability{Reason: "Valor inválido"}}
	}
	if f <= 0 {
		return MetricValue[Money]{DataAvailability: DataAvailability{Reason: "Valor não informado ou igual a zero"}}
	}
	return MetricValue[Money]{DataAvailability: DataAvailability{Available: true}, Value: Money{MinorUnits: int64(f*100 + 0.5), Currency: currency}}
}

func NormalizeObjection(text string) string {
	lower := strings.ToLower(strings.TrimSpace(text))
	switch {
	case lower == "" || strings.Contains(lower, "indetermin"):
		return "UNDETERMINED"
	case strings.Contains(lower, "sem objeção") || strings.Contains(lower, "nenhuma"):
		return "NO_OBJECTION"
	case strings.Contains(lower, "preço") || strings.Contains(lower, "caro"):
		return "PRICE"
	case strings.Contains(lower, "orçamento"):
		return "NO_BUDGET"
	case strings.Contains(lower, "pagamento") || strings.Contains(lower, "pix") || strings.Contains(lower, "cartão"):
		return "PAYMENT_METHOD"
	case strings.Contains(lower, "prazo"):
		return "DEADLINE"
	case strings.Contains(lower, "conteúdo"):
		return "CONTENT"
	case strings.Contains(lower, "acesso"):
		return "ACCESS"
	case strings.Contains(lower, "confiança"):
		return "TRUST"
	case strings.Contains(lower, "momento") || strings.Contains(lower, "depois"):
		return "TIMING"
	case strings.Contains(lower, "concorr"):
		return "COMPETITOR"
	default:
		return "OTHER"
	}
}

func ObjectionLabel(category string) string {
	labels := map[string]string{"PRICE": "Preço", "NO_BUDGET": "Sem orçamento", "PAYMENT_METHOD": "Forma de pagamento", "DEADLINE": "Prazo", "CONTENT": "Conteúdo", "ACCESS": "Acesso", "TRUST": "Confiança", "TIMING": "Momento", "COMPETITOR": "Concorrente", "NO_OBJECTION": "Sem objeção", "OTHER": "Outras", "UNDETERMINED": "Indeterminada"}
	return labels[category]
}

func Sparkline(values []int) string {
	if len(values) == 0 {
		return "indisponível"
	}
	chars := []rune("▁▂▃▄▅▆▇█")
	min, max := values[0], values[0]
	for _, v := range values {
		if v < min {
			min = v
		}
		if v > max {
			max = v
		}
	}
	var b strings.Builder
	for _, v := range values {
		idx := 0
		if max > min {
			idx = (v - min) * (len(chars) - 1) / (max - min)
		}
		b.WriteRune(chars[idx])
	}
	return b.String()
}

func abs(v int64) int64 {
	if v < 0 {
		return -v
	}
	return v
}
func fallback(v, f string) string {
	if v == "" {
		return f
	}
	return v
}
