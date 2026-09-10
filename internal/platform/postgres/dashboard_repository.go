package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	dashboard "github.com/portfolio/auditor-ia/internal/dashboard/domain"
)

type DashboardRepository struct{ pool *pgxpool.Pool }

func NewDashboardRepository(pool *pgxpool.Pool) *DashboardRepository {
	return &DashboardRepository{pool: pool}
}

const latestAssessmentsCTE = `WITH latest AS (
 SELECT DISTINCT ON (deal_id) * FROM deal_assessments
 ORDER BY deal_id, created_at DESC, id DESC
)`

func (r *DashboardRepository) ready(operation string) error {
	if r == nil || r.pool == nil {
		return fmt.Errorf("%s: PostgreSQL indisponível", operation)
	}
	return nil
}

func (r *DashboardRepository) GetSummary(ctx context.Context) (dashboard.Summary, error) {
	if err := r.ready("consultar visão geral"); err != nil {
		return dashboard.Summary{}, err
	}
	s := dashboard.Summary{Results: map[string]int{}, FindingsBySeverity: map[string]int{}, FindingsByRule: map[string]int{}}
	err := r.pool.QueryRow(ctx, latestAssessmentsCTE+`
SELECT (SELECT COUNT(*) FROM deals), COUNT(*),
 COUNT(*) FILTER (WHERE status='COMPLETED'), COUNT(*) FILTER (WHERE status='FAILED'),
 COALESCE(AVG(score),0),
 COUNT(*) FILTER (WHERE final_result='venda'), COUNT(*) FILTER (WHERE final_result='não venda'),
 COUNT(*) FILTER (WHERE final_result='em negociação'), COUNT(*) FILTER (WHERE final_result='pós-venda/suporte'),
 COUNT(*) FILTER (WHERE final_result='indeterminado'),
 (SELECT COUNT(*) FROM conversation_analyses ca JOIN latest l2 ON l2.id=ca.assessment_id WHERE ca.possible_receipt)
FROM latest`).Scan(&s.DealsSynced, &s.DealsAnalyzed, &s.Completed, &s.Failed, &s.AverageScore,
		mapResult(&s, "venda"), mapResult(&s, "não venda"), mapResult(&s, "em negociação"), mapResult(&s, "pós-venda/suporte"), mapResult(&s, "indeterminado"), &s.PossibleReceipts)
	if err != nil {
		return dashboard.Summary{}, safeConnectionError("consultar visão geral", err, os.Getenv("DATABASE_URL"))
	}
	rows, err := r.pool.Query(ctx, latestAssessmentsCTE+` SELECT f.severity, COUNT(*) FROM audit_findings f JOIN latest l ON l.id=f.assessment_id GROUP BY f.severity`)
	if err != nil {
		return dashboard.Summary{}, safeConnectionError("contar achados por severidade", err, os.Getenv("DATABASE_URL"))
	}
	defer rows.Close()
	for rows.Next() {
		var key string
		var count int
		if err := rows.Scan(&key, &count); err != nil {
			return dashboard.Summary{}, err
		}
		s.FindingsBySeverity[key] = count
	}
	ruleRows, err := r.pool.Query(ctx, latestAssessmentsCTE+` SELECT f.rule_name, COUNT(*) FROM audit_findings f JOIN latest l ON l.id=f.assessment_id GROUP BY f.rule_name ORDER BY COUNT(*) DESC, f.rule_name`)
	if err != nil {
		return dashboard.Summary{}, safeConnectionError("contar achados por regra", err, os.Getenv("DATABASE_URL"))
	}
	defer ruleRows.Close()
	for ruleRows.Next() {
		var key string
		var count int
		if err := ruleRows.Scan(&key, &count); err != nil {
			return dashboard.Summary{}, err
		}
		s.FindingsByRule[key] = count
	}
	return s, nil
}

func mapResult(summary *dashboard.Summary, key string) any {
	return resultScanner{summary: summary, key: key}
}

type resultScanner struct {
	summary *dashboard.Summary
	key     string
}

func (s resultScanner) Scan(src any) error {
	var value int64
	switch v := src.(type) {
	case int64:
		value = v
	case int32:
		value = int64(v)
	default:
		return fmt.Errorf("contagem inválida")
	}
	s.summary.Results[s.key] = int(value)
	return nil
}

func (r *DashboardRepository) ListFindings(ctx context.Context, filter dashboard.FindingFilter) (dashboard.FindingPage, error) {
	if err := r.ready("consultar achados"); err != nil {
		return dashboard.FindingPage{}, err
	}
	if filter.Limit <= 0 || filter.Limit > 200 {
		filter.Limit = 25
	}
	if filter.Offset < 0 {
		filter.Offset = 0
	}
	args := []any{filter.Severity, filter.Rule, filter.BitrixDealID, filter.Limit, filter.Offset}
	where := ` WHERE ($1='' OR f.severity=$1) AND ($2='' OR f.rule_name=$2) AND ($3=0 OR d.bitrix_deal_id=$3)`
	var total int
	if err := r.pool.QueryRow(ctx, latestAssessmentsCTE+` SELECT COUNT(*) FROM audit_findings f JOIN latest l ON l.id=f.assessment_id JOIN deals d ON d.id=f.deal_id`+where, args[:3]...).Scan(&total); err != nil {
		return dashboard.FindingPage{}, safeConnectionError("contar achados", err, os.Getenv("DATABASE_URL"))
	}
	if total > 0 && filter.Offset >= total {
		filter.Offset = ((total - 1) / filter.Limit) * filter.Limit
		args[4] = filter.Offset
	}
	rows, err := r.pool.Query(ctx, latestAssessmentsCTE+` SELECT f.id,f.assessment_id,f.deal_id,d.bitrix_deal_id,f.severity,f.rule_name,f.description,d.title FROM audit_findings f JOIN latest l ON l.id=f.assessment_id JOIN deals d ON d.id=f.deal_id`+where+` ORDER BY f.id DESC LIMIT $4 OFFSET $5`, args...)
	if err != nil {
		return dashboard.FindingPage{}, safeConnectionError("listar achados", err, os.Getenv("DATABASE_URL"))
	}
	defer rows.Close()
	page := dashboard.FindingPage{Total: total, Limit: filter.Limit, Offset: filter.Offset}
	for rows.Next() {
		var item dashboard.Finding
		if err := rows.Scan(&item.ID, &item.AssessmentID, &item.DealID, &item.BitrixDealID, &item.Severity, &item.Rule, &item.Description, &item.Title); err != nil {
			return dashboard.FindingPage{}, err
		}
		page.Items = append(page.Items, item)
	}
	return page, rows.Err()
}

func (r *DashboardRepository) ListRules(ctx context.Context) ([]string, error) {
	if err := r.ready("listar regras"); err != nil {
		return nil, err
	}
	rows, err := r.pool.Query(ctx, latestAssessmentsCTE+` SELECT DISTINCT f.rule_name FROM audit_findings f JOIN latest l ON l.id=f.assessment_id ORDER BY f.rule_name`)
	if err != nil {
		return nil, safeConnectionError("listar regras", err, os.Getenv("DATABASE_URL"))
	}
	defer rows.Close()
	var out []string
	for rows.Next() {
		var value string
		if err := rows.Scan(&value); err != nil {
			return nil, err
		}
		out = append(out, value)
	}
	return out, rows.Err()
}

func (r *DashboardRepository) GetDealDetail(ctx context.Context, bitrixID int64) (dashboard.DealDetail, error) {
	if err := r.ready("consultar negócio"); err != nil {
		return dashboard.DealDetail{}, err
	}
	var d dashboard.DealDetail
	var amount, currency, assessmentStatus sql.NullString
	var assessmentID sql.NullInt64
	var analysisID sql.NullInt64
	var score sql.NullFloat64
	var messageCount sql.NullInt64
	var possible sql.NullBool
	var probable, final, source, action, analysisStatus, conversationStatus sql.NullString
	var assigned sql.NullInt64
	var created, updated sql.NullTime
	err := r.pool.QueryRow(ctx, latestAssessmentsCTE+` SELECT d.bitrix_deal_id,d.title,d.stage_id,d.stage_semantic_id,d.closed,d.amount::text,d.currency,d.synced_at,l.id,l.status,l.score,ca.id,ca.message_count,ca.possible_receipt,ca.probable_result,ca.final_result,ca.result_source,ca.recommended_action,ca.error_message,ca.conversation_status,d.assigned_by_id,d.created_at_bitrix,d.updated_at_bitrix,COALESCE(ca.main_reason,''),COALESCE(ca.customer_objections,''),COALESCE(ca.service_quality,''),COALESCE(ca.confidence,''),COALESCE(ca.observation,''),COALESCE(l.trace_id,'') FROM deals d LEFT JOIN latest l ON l.deal_id=d.id LEFT JOIN conversation_analyses ca ON ca.assessment_id=l.id WHERE d.bitrix_deal_id=$1`, bitrixID).Scan(&d.BitrixDealID, &d.Title, &d.StageID, &d.StageSemanticID, &d.Closed, &amount, &currency, &d.SyncedAt, &assessmentID, &assessmentStatus, &score, &analysisID, &messageCount, &possible, &probable, &final, &source, &action, &analysisStatus, &conversationStatus, &assigned, &created, &updated, &d.MainReason, &d.CustomerObjections, &d.ServiceQuality, &d.Confidence, &d.Observation, &d.TraceID)
	if errors.Is(err, pgx.ErrNoRows) {
		return dashboard.DealDetail{}, dashboard.ErrNotFound
	}
	if err != nil {
		return dashboard.DealDetail{}, safeConnectionError("consultar negócio", err, os.Getenv("DATABASE_URL"))
	}
	if amount.Valid {
		d.Amount = &amount.String
	}
	d.Currency = currency.String
	if assessmentID.Valid {
		d.AssessmentID = assessmentID.Int64
	}
	d.HasAnalysis = analysisID.Valid
	d.AssessmentStatus = assessmentStatus.String
	if score.Valid {
		d.Score = &score.Float64
	}
	d.MessageCount = int(messageCount.Int64)
	d.PossibleReceipt = possible.Bool
	d.ProbableResult = probable.String
	d.FinalResult = final.String
	d.ResultSource = source.String
	d.RecommendedAction = action.String
	d.AnalysisStatusMessage = analysisStatus.String
	d.ConversationStatus = conversationStatus.String
	if assigned.Valid {
		v := assigned.Int64
		d.AssignedByID = &v
	}
	if created.Valid {
		v := created.Time
		d.CreatedAtBitrix = &v
	}
	if updated.Valid {
		v := updated.Time
		d.UpdatedAtBitrix = &v
	}
	rows, err := r.pool.Query(ctx, latestAssessmentsCTE+` SELECT f.id,f.assessment_id,f.deal_id,d.bitrix_deal_id,f.severity,f.rule_name,f.description,d.title FROM audit_findings f JOIN latest l ON l.id=f.assessment_id JOIN deals d ON d.id=f.deal_id WHERE d.bitrix_deal_id=$1 ORDER BY f.id`, bitrixID)
	if err != nil {
		return dashboard.DealDetail{}, safeConnectionError("consultar achados do negócio", err, os.Getenv("DATABASE_URL"))
	}
	defer rows.Close()
	for rows.Next() {
		var f dashboard.Finding
		if err := rows.Scan(&f.ID, &f.AssessmentID, &f.DealID, &f.BitrixDealID, &f.Severity, &f.Rule, &f.Description, &f.Title); err != nil {
			return dashboard.DealDetail{}, err
		}
		d.Findings = append(d.Findings, f)
	}
	return d, rows.Err()
}

func (r *DashboardRepository) GetLatestConversationAnalysis(ctx context.Context, bitrixID int64) (dashboard.ConversationAnalysisDetail, error) {
	if err := r.ready("consultar análise"); err != nil {
		return dashboard.ConversationAnalysisDetail{}, err
	}
	var a dashboard.ConversationAnalysisDetail
	err := r.pool.QueryRow(ctx, latestAssessmentsCTE+` SELECT ca.id,ca.assessment_id,d.bitrix_deal_id,d.title,ca.status,COALESCE(ca.model,''),COALESCE(ca.prompt_version,''),ca.message_count,ca.possible_receipt,COALESCE(ca.probable_result,''),COALESCE(ca.final_result,''),COALESCE(ca.result_source,''),COALESCE(ca.main_reason,''),COALESCE(ca.customer_objections,''),COALESCE(ca.service_quality,''),COALESCE(ca.recommended_action,''),COALESCE(ca.confidence,''),COALESCE(ca.observation,''),COALESCE(ca.raw_response,''),COALESCE(ca.error_message,''),ca.conversation_status FROM latest l JOIN deals d ON d.id=l.deal_id JOIN conversation_analyses ca ON ca.assessment_id=l.id WHERE d.bitrix_deal_id=$1 ORDER BY ca.created_at DESC, ca.id DESC LIMIT 1`, bitrixID).Scan(&a.ID, &a.AssessmentID, &a.BitrixDealID, &a.Title, &a.Status, &a.Model, &a.PromptVersion, &a.MessageCount, &a.PossibleReceipt, &a.ProbableResult, &a.FinalResult, &a.ResultSource, &a.MainReason, &a.CustomerObjections, &a.ServiceQuality, &a.RecommendedAction, &a.Confidence, &a.Observation, &a.RawResponse, &a.StatusMessage, &a.ConversationStatus)
	if errors.Is(err, pgx.ErrNoRows) {
		return dashboard.ConversationAnalysisDetail{}, dashboard.ErrNotFound
	}
	if err != nil {
		return dashboard.ConversationAnalysisDetail{}, safeConnectionError("consultar análise", err, os.Getenv("DATABASE_URL"))
	}
	return a, nil
}

var _ dashboard.Repository = (*DashboardRepository)(nil)
