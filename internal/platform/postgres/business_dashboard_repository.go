package postgres

import (
	"context"
	"database/sql"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	business "github.com/portfolio/auditor-ia/internal/business/domain"
)

type BusinessDashboardRepository struct{ pool *pgxpool.Pool }

func NewBusinessDashboardRepository(pool *pgxpool.Pool) *BusinessDashboardRepository {
	return &BusinessDashboardRepository{pool: pool}
}

const businessCurrent = `WITH current_assessments AS (
 SELECT DISTINCT ON (deal_id) * FROM deal_assessments ORDER BY deal_id, created_at DESC, id DESC
), current_analysis AS (
 SELECT DISTINCT ON (deal_id) * FROM conversation_analyses ORDER BY deal_id, created_at DESC, id DESC
) `

func (r *BusinessDashboardRepository) Load(ctx context.Context, filters business.GlobalFilters) (business.Dashboard, error) {
	if r == nil || r.pool == nil {
		return business.Dashboard{}, fmt.Errorf("dashboard empresarial: PostgreSQL indisponível")
	}
	from, to := filters.DateFrom, filters.DateTo
	if filters.Period != "" && filters.Period != business.PeriodCustom {
		from, to = business.ResolvePeriod(filters.Period, time.Now())
	}
	out := business.Dashboard{LoadedAt: time.Now(), Executive: business.ExecutiveSummary{Results: map[string]int{}}, Conversations: business.ConversationQualitySummary{Statuses: map[string]int{}, Quality: map[string]int{}, Confidence: map[string]int{}}, AIQuality: business.AIQualitySummary{Models: map[string]int{}, PromptVersions: map[string]int{}, Confidence: map[string]int{}, ConversationStatuses: map[string]int{}, FailureCategories: map[string]int{}}}
	if err := r.loadExecutive(ctx, &out, from, to, filters.OnlyWithoutValue); err != nil {
		return out, err
	}
	if err := r.loadLists(ctx, &out, from, to, filters.OnlyWithoutValue); err != nil {
		return out, err
	}
	if err := r.loadDimensions(ctx, &out, from, to, filters.OnlyWithoutValue); err != nil {
		return out, err
	}
	if err := r.loadFunnelAgentsTrends(ctx, &out, from, to, filters.OnlyWithoutValue); err != nil {
		return out, err
	}
	location, locationErr := time.LoadLocation("America/Sao_Paulo")
	if locationErr != nil {
		return out, fmt.Errorf("carregar timezone empresarial: %w", locationErr)
	}
	statistics, statisticsErr := NewCRMStatisticsRepository(r.pool).Statistics(ctx, time.Now(), location)
	if statisticsErr != nil {
		return out, safeConnectionError("consultar estatísticas globais do CRM", statisticsErr, os.Getenv("DATABASE_URL"))
	}
	out.CRMStatistics = statistics
	out.CRMQuality.HealthScore = business.HealthScore(out.CRMQuality.Sample, out.CRMQuality.WithoutValue, out.CRMQuality.Stale7, out.CRMQuality.WithoutConversation, out.CRMQuality.Indeterminate, len(out.Divergences))
	return out, nil
}

func (r *BusinessDashboardRepository) loadFunnelAgentsTrends(ctx context.Context, out *business.Dashboard, from, to *time.Time, onlyWithoutValue bool) error {
	rows, err := r.pool.Query(ctx, `SELECT stage_id,COUNT(*),COUNT(*) FILTER(WHERE updated_at_bitrix < NOW()-INTERVAL '7 days'),COUNT(*) FILTER(WHERE updated_at_bitrix < NOW()-INTERVAL '15 days'),COUNT(*) FILTER(WHERE updated_at_bitrix < NOW()-INTERVAL '30 days'),SUM(amount) FILTER(WHERE amount>0) FROM deals WHERE ($1::timestamptz IS NULL OR synced_at >= $1) AND ($2::timestamptz IS NULL OR synced_at < $2) AND (NOT $3 OR amount IS NULL OR amount<=0) GROUP BY stage_id ORDER BY COUNT(*) DESC`, from, to, onlyWithoutValue)
	if err != nil {
		return safeConnectionError("agregar funil", err, os.Getenv("DATABASE_URL"))
	}
	for rows.Next() {
		var stage business.FunnelStage
		var value sql.NullString
		var stale15, stale30 int
		if err := rows.Scan(&stage.Stage, &stage.Deals, &stage.Stale, &stale15, &stale30, &value); err != nil {
			rows.Close()
			return err
		}
		stage.Value = business.ParseMoney(value.String, "BRL")
		stage.Conversion.DataAvailability.Reason = "Histórico entre etapas não está persistido"
		stage.AverageDays.DataAvailability.Reason = "Histórico de etapas não está persistido"
		out.Funnel.Stages = append(out.Funnel.Stages, stage)
		out.Funnel.Sample += stage.Deals
		_ = stale15
		_ = stale30
	}
	rows.Close()
	out.Funnel.Won = out.Executive.Results["venda"]
	out.Funnel.Lost = out.Executive.Results["não venda"]
	out.Funnel.LossRate = business.Percent(out.Funnel.Lost, out.Funnel.Won+out.Funnel.Lost)
	out.Funnel.WeightedValue.DataAvailability.Reason = "Probabilidade por etapa não está persistida"
	agents, err := r.pool.Query(ctx, `SELECT COALESCE(NULLIF(u.display_name,''),d.assigned_by_id::text,'indisponível'),COUNT(*),COUNT(*) FILTER(WHERE NOT d.closed),COUNT(*) FILTER(WHERE d.stage_semantic_id='S'),COUNT(*) FILTER(WHERE d.stage_semantic_id='F'),COUNT(*) FILTER(WHERE d.updated_at_bitrix < NOW()-INTERVAL '7 days'),SUM(d.amount) FILTER(WHERE d.stage_semantic_id='S' AND d.amount>0),SUM(d.amount) FILTER(WHERE NOT d.closed AND d.amount>0) FROM deals d LEFT JOIN bitrix_users u ON u.bitrix_user_id=d.assigned_by_id WHERE ($1::timestamptz IS NULL OR d.synced_at >= $1) AND ($2::timestamptz IS NULL OR d.synced_at < $2) AND (NOT $3 OR d.amount IS NULL OR d.amount<=0) GROUP BY d.assigned_by_id,u.display_name ORDER BY COUNT(*) DESC`, from, to, onlyWithoutValue)
	if err != nil {
		return safeConnectionError("agregar equipe", err, os.Getenv("DATABASE_URL"))
	}
	for agents.Next() {
		var a business.AgentPerformance
		var revenue, pipeline sql.NullString
		if err := agents.Scan(&a.Assignee, &a.Deals, &a.Open, &a.Won, &a.Lost, &a.Stale, &revenue, &pipeline); err != nil {
			agents.Close()
			return err
		}
		a.Conversion = business.Percent(a.Won, a.Won+a.Lost)
		a.Area = business.AgentArea(a.Assignee)
		a.Revenue = business.ParseMoney(revenue.String, "BRL")
		a.Pipeline = business.ParseMoney(pipeline.String, "BRL")
		a.AverageTicket.DataAvailability.Reason = "Valores incompletos por responsável"
		if _, err := strconv.ParseInt(a.Assignee, 10, 64); err == nil {
			a.Note = "Nome ainda não sincronizado; exibindo assigned_by_id"
		}
		out.Agents = append(out.Agents, a)
	}
	agents.Close()
	channel3264, err := r.pool.Query(ctx, `SELECT COALESCE(NULLIF(u.display_name,''),d.assigned_by_id::text,'Sem responsável'),COUNT(*),COUNT(*) FILTER(WHERE d.stage_semantic_id='F') FROM deals d LEFT JOIN bitrix_users u ON u.bitrix_user_id=d.assigned_by_id WHERE d.title ILIKE '%3264%' AND ($1::timestamptz IS NULL OR d.synced_at >= $1) AND ($2::timestamptz IS NULL OR d.synced_at < $2) AND (NOT $3 OR d.amount IS NULL OR d.amount<=0) GROUP BY d.assigned_by_id,u.display_name ORDER BY COUNT(*) DESC`, from, to, onlyWithoutValue)
	if err != nil {
		return safeConnectionError("agregar distribuição do canal 3264", err, os.Getenv("DATABASE_URL"))
	}
	for channel3264.Next() {
		var item business.ChannelAssignment
		if err := channel3264.Scan(&item.Assignee, &item.Deals, &item.Lost); err != nil {
			channel3264.Close()
			return err
		}
		item.Area = business.AgentArea(item.Assignee)
		item.LossRate = business.Percent(item.Lost, item.Deals)
		out.Channel3264 = append(out.Channel3264, item)
	}
	channel3264.Close()
	trends, err := r.pool.Query(ctx, `SELECT to_char(date_trunc('month',synced_at),'YYYY-MM'),COUNT(*),COUNT(*) FILTER(WHERE stage_semantic_id='S'),COUNT(*) FILTER(WHERE stage_semantic_id='F'),SUM(amount) FILTER(WHERE stage_semantic_id='S' AND amount>0) FROM deals WHERE ($1::timestamptz IS NULL OR synced_at >= $1) AND ($2::timestamptz IS NULL OR synced_at < $2) AND (NOT $3 OR amount IS NULL OR amount<=0) GROUP BY date_trunc('month',synced_at) ORDER BY date_trunc('month',synced_at) DESC LIMIT 12`, from, to, onlyWithoutValue)
	if err != nil {
		return safeConnectionError("agregar tendências", err, os.Getenv("DATABASE_URL"))
	}
	for trends.Next() {
		var t business.MonthlyTrend
		var revenue sql.NullString
		if err := trends.Scan(&t.Month, &t.NewDeals, &t.Won, &t.Lost, &revenue); err != nil {
			trends.Close()
			return err
		}
		t.Conversion = business.Percent(t.Won, t.Won+t.Lost)
		t.Revenue = business.ParseMoney(revenue.String, "BRL")
		t.AverageScore.DataAvailability.Reason = "Score histórico mensal não consolidado"
		out.Trends = append(out.Trends, t)
	}
	trends.Close()
	return nil
}

func (r *BusinessDashboardRepository) loadExecutive(ctx context.Context, out *business.Dashboard, from, to *time.Time, onlyWithoutValue bool) error {
	var avg sql.NullFloat64
	var amountCount int
	var amountTotal, pipeline, wone, lost sql.NullString
	err := r.pool.QueryRow(ctx, businessCurrent+`SELECT
	 (SELECT COUNT(*) FROM deals d WHERE ($1::timestamptz IS NULL OR d.synced_at >= $1) AND ($2::timestamptz IS NULL OR d.synced_at < $2) AND (NOT $3 OR d.amount IS NULL OR d.amount<=0)),COUNT(ca.id),
 COUNT(ca.id) FILTER(WHERE ca.status='COMPLETED'),COUNT(ca.id) FILTER(WHERE ca.status='FAILED'),AVG(ca.score),
 COUNT(ca.id) FILTER(WHERE ca.final_result='venda'),COUNT(ca.id) FILTER(WHERE ca.final_result='não venda'),COUNT(ca.id) FILTER(WHERE ca.final_result='em negociação'),COUNT(ca.id) FILTER(WHERE ca.final_result='pós-venda/suporte'),COUNT(ca.id) FILTER(WHERE ca.final_result='indeterminado'),
 COUNT(*) FILTER(WHERE an.possible_receipt),COUNT(*) FILTER(WHERE an.probable_result='venda' AND d.stage_semantic_id='P'),COUNT(*) FILTER(WHERE an.probable_result='em negociação' AND d.stage_semantic_id='F'),
	 COUNT(ca.id) FILTER(WHERE d.amount IS NULL OR d.amount<=0),COUNT(ca.id) FILTER(WHERE an.conversation_status='NO_CONVERSATION'),COUNT(ca.id) FILTER(WHERE an.conversation_status='EMPTY_CONVERSATION'),COUNT(ca.id) FILTER(WHERE an.conversation_status='ACCESS_DENIED'),
	 COUNT(ca.id) FILTER(WHERE d.updated_at_bitrix < NOW()-INTERVAL '7 days'),COUNT(ca.id) FILTER(WHERE d.updated_at_bitrix < NOW()-INTERVAL '15 days'),COUNT(ca.id) FILTER(WHERE d.updated_at_bitrix < NOW()-INTERVAL '30 days'),
	 COUNT(ca.id) FILTER(WHERE d.amount>0),SUM(d.amount) FILTER(WHERE ca.id IS NOT NULL AND d.amount>0),SUM(d.amount) FILTER(WHERE ca.id IS NOT NULL AND NOT d.closed AND d.amount>0),SUM(d.amount) FILTER(WHERE ca.id IS NOT NULL AND d.stage_semantic_id='S' AND d.amount>0),SUM(d.amount) FILTER(WHERE ca.id IS NOT NULL AND d.stage_semantic_id='F' AND d.amount>0)
 FROM deals d LEFT JOIN current_assessments ca ON ca.deal_id=d.id LEFT JOIN current_analysis an ON an.deal_id=d.id
	 WHERE ($1::timestamptz IS NULL OR d.synced_at >= $1) AND ($2::timestamptz IS NULL OR d.synced_at < $2) AND (NOT $3 OR d.amount IS NULL OR d.amount<=0)`, from, to, onlyWithoutValue).Scan(&out.Executive.DealsSynced, &out.Executive.DealsEvaluated, &out.Executive.Completed, &out.Executive.Failed, &avg,
		mapBusinessResult(&out.Executive, "venda"), mapBusinessResult(&out.Executive, "não venda"), mapBusinessResult(&out.Executive, "em negociação"), mapBusinessResult(&out.Executive, "pós-venda/suporte"), mapBusinessResult(&out.Executive, "indeterminado"), &out.Executive.PossibleReceipts, &out.Executive.PossibleUnregisteredSales, &out.Executive.LostStillNegotiating, &out.Executive.WithoutValue, &out.Executive.WithoutConversation, &out.Executive.EmptyConversation, &out.Executive.AccessDenied, &out.CRMQuality.Stale7, &out.CRMQuality.Stale15, &out.CRMQuality.Stale30, &amountCount, &amountTotal, &pipeline, &wone, &lost)
	if err != nil {
		return safeConnectionError("consultar visão executiva", err, os.Getenv("DATABASE_URL"))
	}
	out.Executive.Sample = out.Executive.DealsEvaluated
	out.Executive.CompletionRate = business.Percent(out.Executive.Completed, out.Executive.DealsEvaluated)
	if avg.Valid {
		out.Executive.AverageScore = business.MetricValue[string]{DataAvailability: business.DataAvailability{Available: true}, Value: fmt.Sprintf("%.2f", avg.Float64)}
	} else {
		out.Executive.AverageScore.DataAvailability.Reason = "Nenhuma avaliação com score"
	}
	coverage := business.Percent(out.Executive.WithoutValue, out.Executive.DealsEvaluated)
	if coverage.Available && coverage.Value.BasisPoints > 0 {
		out.Executive.CoverageNote = fmt.Sprintf("Indisponível para parte da amostra: %s dos negócios não possuem valor informado.", coverage.Value.String())
	}
	out.Executive.Pipeline = business.ParseMoney(pipeline.String, "BRL")
	out.Executive.WonRevenue = business.ParseMoney(wone.String, "BRL")
	out.Executive.LostRevenue = business.ParseMoney(lost.String, "BRL")
	out.Executive.Conversion = business.Percent(out.Executive.Results["venda"], out.Executive.Results["venda"]+out.Executive.Results["não venda"])
	if amountCount > 0 && amountTotal.Valid {
		value, _ := strconv.ParseFloat(amountTotal.String, 64)
		out.Executive.AverageTicket = business.ParseMoney(fmt.Sprintf("%.2f", value/float64(amountCount)), "BRL")
	} else {
		out.Executive.AverageTicket.DataAvailability.Reason = "Negócios sem valor cadastrado"
	}
	out.Executive.WeightedValue.DataAvailability.Reason = "Probabilidade por etapa não está persistida"
	out.CRMQuality.Sample = out.Executive.DealsEvaluated
	out.CRMQuality.WithoutValue = out.Executive.WithoutValue
	out.CRMQuality.WithoutConversation = out.Executive.WithoutConversation
	out.CRMQuality.EmptyConversation = out.Executive.EmptyConversation
	out.CRMQuality.AccessDenied = out.Executive.AccessDenied
	out.CRMQuality.Indeterminate = out.Executive.Results["indeterminado"]
	out.CRMQuality.CoverageNote = "Indicadores usam somente a avaliação mais recente por negócio."
	return nil
}

func (r *BusinessDashboardRepository) loadLists(ctx context.Context, out *business.Dashboard, from, to *time.Time, onlyWithoutValue bool) error {
	rows, err := r.pool.Query(ctx, businessCurrent+`SELECT d.bitrix_deal_id,d.title,COALESCE(NULLIF(u.display_name,''),d.assigned_by_id::text,'indisponível'),d.stage_id,d.amount::text,COALESCE(d.currency,'BRL'),COALESCE(an.probable_result,''),COALESCE(an.final_result,''),COALESCE(an.main_reason,''),COALESCE(an.possible_receipt,false),d.updated_at_bitrix,COALESCE(an.recommended_action,''),COALESCE(an.confidence,''),COALESCE(ca.score,0),d.stage_semantic_id,ca.status
 FROM deals d JOIN current_assessments ca ON ca.deal_id=d.id LEFT JOIN current_analysis an ON an.deal_id=d.id LEFT JOIN bitrix_users u ON u.bitrix_user_id=d.assigned_by_id WHERE ($1::timestamptz IS NULL OR d.synced_at >= $1) AND ($2::timestamptz IS NULL OR d.synced_at < $2) AND (NOT $3 OR d.amount IS NULL OR d.amount<=0) ORDER BY d.updated_at_bitrix DESC NULLS LAST,d.id DESC LIMIT 200`, from, to, onlyWithoutValue)
	if err != nil {
		return safeConnectionError("listar prioridades", err, os.Getenv("DATABASE_URL"))
	}
	defer rows.Close()
	for rows.Next() {
		var p business.PriorityOpportunity
		var amount sql.NullString
		var updated sql.NullTime
		var semantic string
		if err := rows.Scan(&p.BitrixDealID, &p.Title, &p.Assignee, &p.Stage, &amount, &p.Value.Value.Currency, &p.ProbableResult, &p.FinalResult, &p.Evidence, &p.PossibleReceipt, &updated, &p.NextAction, &p.Confidence, &p.AuditScore, &semantic, &p.AnalysisStatus); err != nil {
			return err
		}
		p.Value = business.ParseMoney(amount.String, p.Value.Value.Currency)
		p.ExplicitPaymentEvidence = hasPaymentEvidence(p.Evidence)
		if updated.Valid {
			p.UpdatedAt = updated.Time
			p.StaleDays = int(time.Since(updated.Time).Hours() / 24)
		}
		p.PriorityScore = business.PriorityScore(p)
		if p.PriorityScore >= 20 {
			out.Priorities = append(out.Priorities, p)
		}
		types := divergenceTypes(p, semantic, p.Value.Available)
		for _, kind := range types {
			out.Divergences = append(out.Divergences, business.CRMAIDivergence{Type: kind, BitrixDealID: p.BitrixDealID, CRMState: semantic, ConversationState: p.ProbableResult, Evidence: p.Evidence, Confidence: p.Confidence, Recommendation: p.NextAction, Assignee: p.Assignee, PossibleReceipt: p.PossibleReceipt, Date: p.UpdatedAt})
		}
	}
	sort.Slice(out.Priorities, func(i, j int) bool { return out.Priorities[i].PriorityScore > out.Priorities[j].PriorityScore })
	if len(out.Priorities) > 50 {
		out.Priorities = out.Priorities[:50]
	}
	return rows.Err()
}

func (r *BusinessDashboardRepository) loadDimensions(ctx context.Context, out *business.Dashboard, from, to *time.Time, onlyWithoutValue bool) error {
	rows, err := r.pool.Query(ctx, businessCurrent+`SELECT COALESCE(an.conversation_status,'NO_CONVERSATION'),1,COALESCE(an.collected_message_count,an.message_count,0),COALESCE(an.analyzed_message_count,LEAST(an.message_count,50),0),(SELECT COUNT(*) FROM conversation_messages m WHERE m.deal_id=d.id),COALESCE(an.service_quality,'indeterminada'),COALESCE(an.confidence,'indeterminada'),CASE WHEN an.possible_receipt THEN 1 ELSE 0 END,COALESCE(an.model,''),COALESCE(an.prompt_version,''),ca.status,COALESCE(an.error_message,''),COALESCE(an.customer_objections,'') FROM deals d JOIN current_assessments ca ON ca.deal_id=d.id LEFT JOIN current_analysis an ON an.deal_id=d.id WHERE ($1::timestamptz IS NULL OR d.synced_at >= $1) AND ($2::timestamptz IS NULL OR d.synced_at < $2) AND (NOT $3 OR d.amount IS NULL OR d.amount<=0)`, from, to, onlyWithoutValue)
	if err != nil {
		return safeConnectionError("agregar qualidade empresarial", err, os.Getenv("DATABASE_URL"))
	}
	defer rows.Close()
	objectionCounts := map[string]int{}
	for rows.Next() {
		var status, quality, confidence, model, prompt, assessmentStatus, errorMessage, objection string
		var count, collected, analyzed, persisted, receipts int
		if err := rows.Scan(&status, &count, &collected, &analyzed, &persisted, &quality, &confidence, &receipts, &model, &prompt, &assessmentStatus, &errorMessage, &objection); err != nil {
			return err
		}
		out.Conversations.Sample += count
		out.Conversations.Statuses[status] += count
		out.Conversations.Quality[quality] += count
		out.Conversations.Confidence[confidence] += count
		out.Conversations.MessagesCollected += collected
		out.Conversations.MessagesAnalyzed += analyzed
		out.Conversations.MessagesPersisted += persisted
		out.Conversations.PossibleReceipts += receipts
		out.AIQuality.Sample += count
		out.AIQuality.ConversationStatuses[status] += count
		if assessmentStatus == "COMPLETED" {
			out.AIQuality.Completed += count
		} else if assessmentStatus == "FAILED" {
			out.AIQuality.Failed += count
			out.AIQuality.FailureCategories[classifyFailure(errorMessage)] += count
		}
		if model != "" {
			out.AIQuality.Models[model] += count
		}
		if prompt != "" {
			out.AIQuality.PromptVersions[prompt] += count
		}
		out.AIQuality.Confidence[confidence] += count
		category := business.NormalizeObjection(objection)
		objectionCounts[category] += count
	}
	if out.Conversations.MessagesPersisted < out.Conversations.MessagesCollected {
		out.Conversations.CoverageAlert = fmt.Sprintf("%d mensagens foram coletadas nas análises atuais, mas somente %d estão persistidas.", out.Conversations.MessagesCollected, out.Conversations.MessagesPersisted)
		out.CRMQuality.MessageCountMismatch = out.Conversations.MessagesCollected - out.Conversations.MessagesPersisted
	}
	out.AIQuality.SuccessRate = business.Percent(out.AIQuality.Completed, out.AIQuality.Completed+out.AIQuality.Failed)
	out.AIQuality.PossibleReceipts = out.Conversations.PossibleReceipts
	out.AIQuality.Divergences = len(out.Divergences)
	out.AIQuality.DurationAverage.DataAvailability.Reason = "Duração de inferência não está persistida separadamente"
	out.AIQuality.DurationP50.DataAvailability.Reason = out.AIQuality.DurationAverage.Reason
	out.AIQuality.DurationP95.DataAvailability.Reason = out.AIQuality.DurationAverage.Reason
	out.AIQuality.DurationMax.DataAvailability.Reason = out.AIQuality.DurationAverage.Reason
	for category, count := range objectionCounts {
		out.Objections = append(out.Objections, business.ObjectionSummary{Category: category, Label: business.ObjectionLabel(category), Count: count, Percentage: business.Percent(count, out.Conversations.Sample), PotentialValue: business.MetricValue[business.Money]{DataAvailability: business.DataAvailability{Reason: "Valor por objeção incompleto"}}})
	}
	sort.Slice(out.Objections, func(i, j int) bool { return out.Objections[i].Count > out.Objections[j].Count })
	return rows.Err()
}

func divergenceTypes(p business.PriorityOpportunity, semantic string, hasValue bool) []business.DivergenceType {
	var out []business.DivergenceType
	if p.ProbableResult == "venda" && semantic == "P" {
		out = append(out, business.AISaleCRMOpen)
	}
	if p.ProbableResult == "venda" && semantic == "F" {
		out = append(out, business.AISaleCRMLost)
	}
	if p.ProbableResult == "em negociação" && semantic == "F" {
		out = append(out, business.CRMLostAINegotiating)
	}
	if p.PossibleReceipt && semantic != "S" {
		out = append(out, business.PossibleReceiptNotWon)
	}
	if p.PossibleReceipt && !hasValue {
		out = append(out, business.PaymentEvidenceWithoutValue)
	}
	if p.AnalysisStatus == "COMPLETED" && p.ExplicitPaymentEvidence && !p.PossibleReceipt {
		out = append(out, business.PaymentTextDetectorConflict)
	}
	return out
}

func hasPaymentEvidence(text string) bool {
	text = strings.ToLower(strings.TrimSpace(text))
	return strings.Contains(text, "pagamento já realizado") ||
		strings.Contains(text, "pagamento realizado") ||
		strings.Contains(text, "comprovante de pagamento") ||
		strings.Contains(text, "envio do comprovante") ||
		strings.Contains(text, "comprovante enviado") ||
		strings.Contains(text, "paguei")
}
func classifyFailure(message string) string {
	switch {
	case contains(message, "inválida"):
		return "PARSER_ERROR"
	case contains(message, "incompleta"):
		return "PARSER_ERROR"
	case contains(message, "timeout"):
		return "TIMEOUT"
	case contains(message, "ollama"):
		return "OLLAMA_UNAVAILABLE"
	case contains(message, "bitrix"):
		return "BITRIX_ERROR"
	case contains(message, "postgres"):
		return "POSTGRES_ERROR"
	case contains(message, "acesso"):
		return "ACCESS_DENIED"
	default:
		return "UNKNOWN"
	}
}
func contains(v, needle string) bool {
	return len(v) >= len(needle) && strings.Contains(strings.ToLower(v), strings.ToLower(needle))
}
func mapBusinessResult(summary *business.ExecutiveSummary, key string) any {
	return businessResultScanner{summary, key}
}

type businessResultScanner struct {
	summary *business.ExecutiveSummary
	key     string
}

func (s businessResultScanner) Scan(src any) error {
	switch v := src.(type) {
	case int64:
		s.summary.Results[s.key] = int(v)
	case int32:
		s.summary.Results[s.key] = int(v)
	default:
		return fmt.Errorf("contagem inválida")
	}
	return nil
}

var _ business.Repository = (*BusinessDashboardRepository)(nil)
