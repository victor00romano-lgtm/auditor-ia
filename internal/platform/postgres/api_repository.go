package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"os"
	"strconv"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	apidomain "github.com/portfolio/auditor-ia/internal/api/domain"
)

type APIRepository struct{ pool *pgxpool.Pool }

func NewAPIRepository(pool *pgxpool.Pool) *APIRepository { return &APIRepository{pool: pool} }
func (r *APIRepository) ready() error {
	if r == nil || r.pool == nil {
		return fmt.Errorf("PostgreSQL indisponível")
	}
	return nil
}

const auditSelect = `SELECT a.id,d.bitrix_deal_id,d.title,a.status,a.score,a.final_result,a.result_source,a.summary,
 (SELECT COUNT(*) FROM audit_findings f WHERE f.assessment_id=a.id),a.started_at,a.finished_at,a.created_at,a.trace_id,
 COALESCE((SELECT ca.conversation_status FROM conversation_analyses ca WHERE ca.assessment_id=a.id),''),
 COALESCE((SELECT ca.error_message FROM conversation_analyses ca WHERE ca.assessment_id=a.id),'')
 FROM deal_assessments a JOIN deals d ON d.id=a.deal_id`

func scanAudit(row interface{ Scan(...any) error }) (apidomain.Audit, error) {
	var out apidomain.Audit
	var score sql.NullFloat64
	var final, source, summary sql.NullString
	var traceID sql.NullString
	var finished sql.NullTime
	err := row.Scan(&out.AssessmentID, &out.BitrixDealID, &out.DealTitle, &out.Status, &score, &final, &source, &summary, &out.FindingsCount, &out.StartedAt, &finished, &out.CreatedAt, &traceID, &out.ConversationStatus, &out.ConversationMessage)
	if err != nil {
		return apidomain.Audit{}, err
	}
	if score.Valid {
		out.Score = &score.Float64
	}
	out.FinalResult = final.String
	out.ResultSource = source.String
	out.Summary = summary.String
	out.TraceID = traceID.String
	if finished.Valid {
		out.FinishedAt = &finished.Time
	}
	return out, nil
}

func (r *APIRepository) GetAudit(ctx context.Context, id int64) (apidomain.Audit, error) {
	if err := r.ready(); err != nil {
		return apidomain.Audit{}, err
	}
	out, err := scanAudit(r.pool.QueryRow(ctx, auditSelect+` WHERE a.id=$1`, id))
	if errors.Is(err, pgx.ErrNoRows) {
		return apidomain.Audit{}, apidomain.ErrNotFound
	}
	if err != nil {
		return apidomain.Audit{}, safeConnectionError("consultar auditoria", err, os.Getenv("DATABASE_URL"))
	}
	return out, nil
}

func (r *APIRepository) ListAudits(ctx context.Context, f apidomain.AuditFilter) (apidomain.Page[apidomain.Audit], error) {
	if err := r.ready(); err != nil {
		return apidomain.Page[apidomain.Audit]{}, err
	}
	where := ` WHERE ($1=0 OR d.bitrix_deal_id=$1) AND ($2='' OR a.status=$2) AND ($3='' OR a.final_result=$3) AND ($4::timestamptz IS NULL OR a.created_at >= $4) AND ($5::timestamptz IS NULL OR a.created_at <= $5)`
	args := []any{f.BitrixDealID, f.Status, f.FinalResult, f.DateFrom, f.DateTo}
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM deal_assessments a JOIN deals d ON d.id=a.deal_id`+where, args...).Scan(&total); err != nil {
		return apidomain.Page[apidomain.Audit]{}, safeConnectionError("contar auditorias", err, os.Getenv("DATABASE_URL"))
	}
	offset := (f.Page - 1) * f.PageSize
	rows, err := r.pool.Query(ctx, auditSelect+where+` ORDER BY a.created_at DESC,a.id DESC LIMIT $6 OFFSET $7`, append(args, f.PageSize, offset)...)
	if err != nil {
		return apidomain.Page[apidomain.Audit]{}, safeConnectionError("listar auditorias", err, os.Getenv("DATABASE_URL"))
	}
	defer rows.Close()
	page := apidomain.Page[apidomain.Audit]{Items: []apidomain.Audit{}, Total: total, Page: f.Page, PageSize: f.PageSize}
	for rows.Next() {
		item, err := scanAudit(rows)
		if err != nil {
			return apidomain.Page[apidomain.Audit]{}, err
		}
		page.Items = append(page.Items, item)
	}
	return page, rows.Err()
}

func (r *APIRepository) ListFindings(ctx context.Context, f apidomain.FindingFilter) (apidomain.Page[apidomain.Finding], error) {
	if err := r.ready(); err != nil {
		return apidomain.Page[apidomain.Finding]{}, err
	}
	where := ` WHERE ($1=0 OR f.assessment_id=$1) AND ($2=0 OR d.bitrix_deal_id=$2) AND ($3='' OR f.severity=$3) AND ($4='' OR f.rule_name=$4)`
	args := []any{f.AssessmentID, f.BitrixDealID, f.Severity, f.Rule}
	var total int
	if err := r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_findings f JOIN deals d ON d.id=f.deal_id`+where, args...).Scan(&total); err != nil {
		return apidomain.Page[apidomain.Finding]{}, safeConnectionError("contar achados", err, os.Getenv("DATABASE_URL"))
	}
	rows, err := r.pool.Query(ctx, `SELECT f.id,f.assessment_id,d.bitrix_deal_id,f.rule_name,f.description,f.severity,f.created_at FROM audit_findings f JOIN deals d ON d.id=f.deal_id`+where+` ORDER BY f.created_at DESC,f.id DESC LIMIT $5 OFFSET $6`, append(args, f.PageSize, (f.Page-1)*f.PageSize)...)
	if err != nil {
		return apidomain.Page[apidomain.Finding]{}, safeConnectionError("listar achados", err, os.Getenv("DATABASE_URL"))
	}
	defer rows.Close()
	page := apidomain.Page[apidomain.Finding]{Items: []apidomain.Finding{}, Total: total, Page: f.Page, PageSize: f.PageSize}
	for rows.Next() {
		var item apidomain.Finding
		var assessmentID sql.NullInt64
		if err := rows.Scan(&item.ID, &assessmentID, &item.BitrixDealID, &item.Rule, &item.Description, &item.Severity, &item.CreatedAt); err != nil {
			return apidomain.Page[apidomain.Finding]{}, err
		}
		item.AssessmentID = assessmentID.Int64
		item.EntityType = "deal"
		item.EntityID = strconv.FormatInt(item.BitrixDealID, 10)
		page.Items = append(page.Items, item)
	}
	return page, rows.Err()
}

func (r *APIRepository) GetAnalysis(ctx context.Context, dealID, assessmentID int64) (apidomain.Analysis, error) {
	if err := r.ready(); err != nil {
		return apidomain.Analysis{}, err
	}
	query := `SELECT ca.id,ca.assessment_id,d.bitrix_deal_id,ca.status,ca.probable_result,ca.final_result,ca.result_source,ca.main_reason,ca.customer_objections,ca.service_quality,ca.recommended_action,ca.confidence,ca.possible_receipt,ca.message_count,ca.collected_message_count,ca.persisted_message_count,ca.analyzed_message_count,ca.model,ca.prompt_version,ca.raw_response,ca.error_message,ca.conversation_status,ca.created_at FROM conversation_analyses ca JOIN deals d ON d.id=ca.deal_id WHERE d.bitrix_deal_id=$1 AND ($2=0 OR ca.assessment_id=$2) ORDER BY ca.created_at DESC,ca.id DESC LIMIT 1`
	var a apidomain.Analysis
	var storedAssessmentID sql.NullInt64
	var probable, final, source, reason, objections, quality, action, confidence, model, prompt, raw, unavailable sql.NullString
	err := r.pool.QueryRow(ctx, query, dealID, assessmentID).Scan(&a.ID, &storedAssessmentID, &a.BitrixDealID, &a.Status, &probable, &final, &source, &reason, &objections, &quality, &action, &confidence, &a.PossibleReceipt, &a.MessageCount, &a.CollectedMessageCount, &a.PersistedMessageCount, &a.AnalyzedMessageCount, &model, &prompt, &raw, &unavailable, &a.ConversationStatus, &a.CreatedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return apidomain.Analysis{}, apidomain.ErrNotFound
	}
	if err != nil {
		return apidomain.Analysis{}, safeConnectionError("consultar análise", err, os.Getenv("DATABASE_URL"))
	}
	a.ProbableResult = probable.String
	a.AssessmentID = storedAssessmentID.Int64
	a.FinalResult = final.String
	a.ResultSource = source.String
	a.MainReason = reason.String
	a.CustomerObjections = objections.String
	a.ServiceQuality = quality.String
	a.RecommendedAction = action.String
	a.Confidence = confidence.String
	a.Model = model.String
	a.PromptVersion = prompt.String
	a.RawResponse = raw.String
	a.UnavailableReason = unavailable.String
	return a, nil
}

func (r *APIRepository) GetReportData(ctx context.Context, id int64) (apidomain.ReportData, error) {
	audit, err := r.GetAudit(ctx, id)
	if err != nil {
		return apidomain.ReportData{}, err
	}
	out := apidomain.ReportData{Audit: audit, Findings: []apidomain.Finding{}}
	for pageNumber := 1; ; pageNumber++ {
		findings, err := r.ListFindings(ctx, apidomain.FindingFilter{Page: pageNumber, PageSize: 100, AssessmentID: id})
		if err != nil {
			return apidomain.ReportData{}, err
		}
		out.Findings = append(out.Findings, findings.Items...)
		if len(out.Findings) >= findings.Total {
			break
		}
	}
	analysis, err := r.GetAnalysis(ctx, audit.BitrixDealID, id)
	if err == nil {
		out.Analysis = &analysis
	} else if !errors.Is(err, apidomain.ErrNotFound) {
		return apidomain.ReportData{}, err
	}
	return out, nil
}

var _ apidomain.Repository = (*APIRepository)(nil)
