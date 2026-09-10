package postgres

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgconn"
	"github.com/jackc/pgx/v5/pgxpool"
	statsdomain "github.com/portfolio/auditor-ia/internal/crmstats/domain"
	"github.com/portfolio/auditor-ia/internal/observability"
)

type CRMStatisticsRepository struct{ pool *pgxpool.Pool }

func NewCRMStatisticsRepository(pool *pgxpool.Pool) *CRMStatisticsRepository {
	return &CRMStatisticsRepository{pool: pool}
}

func (r *CRMStatisticsRepository) Start(ctx context.Context) (int64, error) {
	_, _ = r.pool.Exec(ctx, `UPDATE crm_sync_runs SET status='CANCELED',finished_at=NOW(),safe_error='Execução interrompida antes da conclusão.' WHERE status='RUNNING' AND started_at < NOW()-INTERVAL '2 hours'`)
	var id int64
	err := r.pool.QueryRow(ctx, `INSERT INTO crm_sync_runs(status) SELECT 'RUNNING' WHERE NOT EXISTS(SELECT 1 FROM crm_sync_runs WHERE status='RUNNING') RETURNING id`).Scan(&id)
	if errors.Is(err, pgx.ErrNoRows) {
		return 0, statsdomain.ErrSyncRunning
	}
	var pgErr *pgconn.PgError
	if errors.As(err, &pgErr) && pgErr.Code == "23505" {
		return 0, statsdomain.ErrSyncRunning
	}
	return id, err
}

func (r *CRMStatisticsRepository) KnownAssigneeIDs(ctx context.Context, ids []int64) (map[int64]bool, error) {
	known := make(map[int64]bool)
	if len(ids) == 0 {
		return known, nil
	}
	rows, err := r.pool.Query(ctx, `SELECT bitrix_user_id FROM bitrix_users WHERE bitrix_user_id=ANY($1)`, ids)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		known[id] = true
	}
	return known, rows.Err()
}

func (r *CRMStatisticsRepository) Publish(ctx context.Context, runID int64, snapshot statsdomain.Snapshot) error {
	tx, err := r.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(context.Background())
	for _, user := range snapshot.Users {
		if _, err = tx.Exec(ctx, `INSERT INTO bitrix_users(bitrix_user_id,display_name,active,synced_at) VALUES($1,$2,$3,$4) ON CONFLICT(bitrix_user_id) DO UPDATE SET display_name=EXCLUDED.display_name,active=EXCLUDED.active,synced_at=EXCLUDED.synced_at`, user.BitrixUserID, user.DisplayName, user.Active, user.SyncedAt); err != nil {
			return fmt.Errorf("persistir responsável: %w", err)
		}
	}
	dealIDs := make(map[int64]int64, len(snapshot.Deals))
	for _, deal := range snapshot.Deals {
		var internalID int64
		err = tx.QueryRow(ctx, `INSERT INTO deals(bitrix_deal_id,title,stage_id,stage_semantic_id,closed,amount,currency,assigned_by_id,created_at_bitrix,updated_at_bitrix,synced_at,crm_sync_run_id)
VALUES($1,COALESCE($2,''),COALESCE($3,''),COALESCE($4,''),COALESCE($5,false),$6,$7,$8,$9,$10,NOW(),$11)
ON CONFLICT(bitrix_deal_id) DO UPDATE SET title=EXCLUDED.title,stage_id=EXCLUDED.stage_id,stage_semantic_id=EXCLUDED.stage_semantic_id,closed=EXCLUDED.closed,amount=EXCLUDED.amount,currency=EXCLUDED.currency,assigned_by_id=EXCLUDED.assigned_by_id,created_at_bitrix=EXCLUDED.created_at_bitrix,updated_at_bitrix=EXCLUDED.updated_at_bitrix,synced_at=NOW(),crm_sync_run_id=EXCLUDED.crm_sync_run_id RETURNING id`, deal.BitrixDealID, deal.Title, deal.StageID, deal.StageSemanticID, deal.Closed, deal.Amount, deal.Currency, deal.AssignedByID, deal.CreatedAtBitrix, deal.UpdatedAtBitrix, runID).Scan(&internalID)
		if err != nil {
			return fmt.Errorf("persistir negócio %d: %w", deal.BitrixDealID, err)
		}
		dealIDs[deal.BitrixDealID] = internalID
	}
	if snapshot.MarkerAvailable {
		for _, bitrixID := range snapshot.MarkerDealIDs {
			internalID, exists := dealIDs[bitrixID]
			if !exists {
				continue
			}
			if _, err = tx.Exec(ctx, `INSERT INTO deal_channel_markers(deal_id,sync_run_id,marker,source) VALUES($1,$2,'3264','IMOPENLINES_SESSION') ON CONFLICT DO NOTHING`, internalID, runID); err != nil {
				return fmt.Errorf("persistir marcador 3264: %w", err)
			}
		}
	}
	status := statsdomain.Completed
	safeError := any(nil)
	if !snapshot.MarkerAvailable {
		status = statsdomain.Partial
		safeError = "Atividades IMOPENLINES indisponíveis por permissão do Bitrix."
	}
	tag, err := tx.Exec(ctx, `UPDATE crm_sync_runs SET status=$2,finished_at=NOW(),deals_collected=$3,activities_collected=$4,markers_collected=$5,marker_3264_available=$6,safe_error=$7 WHERE id=$1 AND status='RUNNING'`, runID, status, len(snapshot.Deals), snapshot.ActivitiesCollected, len(snapshot.MarkerDealIDs), snapshot.MarkerAvailable, safeError)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("execução de sincronização não está ativa")
	}
	return tx.Commit(ctx)
}

func (r *CRMStatisticsRepository) FinishFailed(ctx context.Context, runID int64, status statsdomain.Status, safeError string) error {
	if status != statsdomain.Failed && status != statsdomain.Canceled {
		status = statsdomain.Failed
	}
	if len([]rune(safeError)) > 500 {
		safeError = string([]rune(safeError)[:500])
	}
	_, err := r.pool.Exec(ctx, `UPDATE crm_sync_runs SET status=$2,finished_at=NOW(),safe_error=$3 WHERE id=$1 AND status='RUNNING'`, runID, status, safeError)
	return err
}

func (r *CRMStatisticsRepository) Statistics(ctx context.Context, now time.Time, location *time.Location) (statsdomain.Statistics, error) {
	if location == nil {
		location = time.UTC
	}
	ctx, span := observability.Tracer().Start(ctx, "postgres.crm_statistics.query")
	defer span.End()
	var out statsdomain.Statistics
	var runID int64
	var markerAvailable bool
	var safeError sql.NullString
	var publishedStatus statsdomain.Status
	err := r.pool.QueryRow(ctx, `SELECT id,status,finished_at,marker_3264_available,safe_error FROM crm_sync_runs WHERE status IN('COMPLETED','PARTIAL') ORDER BY finished_at DESC,id DESC LIMIT 1`).Scan(&runID, &publishedStatus, &out.LastSyncAt, &markerAvailable, &safeError)
	if errors.Is(err, pgx.ErrNoRows) {
		out.UnavailableReason = "Nenhuma sincronização global concluída"
		return out, nil
	}
	if err != nil {
		return out, err
	}
	out.Available = true
	if err = r.pool.QueryRow(ctx, `SELECT status,COALESCE(finished_at,started_at) FROM crm_sync_runs ORDER BY created_at DESC,id DESC LIMIT 1`).Scan(&out.LastStatus, &out.LastAttemptAt); err != nil {
		return out, err
	}
	if out.LastStatus == statsdomain.Failed || out.LastStatus == statsdomain.Canceled {
		out.CoverageNote = "A última tentativa de sincronização não foi publicada; exibindo o snapshot válido anterior."
	}
	out.Marker3264Available = markerAvailable
	if !markerAvailable {
		out.MarkerUnavailable = "Indisponível: acesso às atividades negado pelo Bitrix"
	}
	if safeError.Valid && safeError.String != "" {
		out.CoverageNote = strings.TrimSpace(out.CoverageNote + " " + safeError.String)
	}
	var lastSuccess sql.NullTime
	if err = r.pool.QueryRow(ctx, `SELECT MAX(finished_at) FROM crm_sync_runs WHERE status='COMPLETED'`).Scan(&lastSuccess); err != nil {
		return out, err
	}
	if lastSuccess.Valid {
		out.LastSuccessAt = lastSuccess.Time
	}
	if !out.LastSyncAt.IsZero() && now.Sub(out.LastSyncAt) > 24*time.Hour {
		out.Stale = true
		out.CoverageNote = strings.TrimSpace(out.CoverageNote + " Dados desatualizados há mais de 24 horas.")
	}
	if err = r.pool.QueryRow(ctx, `SELECT COUNT(DISTINCT bitrix_deal_id) FROM deals WHERE crm_sync_run_id=$1`, runID).Scan(&out.TotalDeals); err != nil {
		return out, err
	}
	if err = r.pool.QueryRow(ctx, `SELECT COUNT(*) FILTER (WHERE UPPER(TRIM(COALESCE(stage_semantic_id,'')))='S'),COUNT(*) FILTER (WHERE UPPER(TRIM(COALESCE(stage_semantic_id,'')))='F') FROM deals WHERE crm_sync_run_id=$1`, runID).Scan(&out.WonDeals, &out.LostDeals); err != nil {
		return out, err
	}
	rows, err := r.pool.Query(ctx, `SELECT COALESCE(d.assigned_by_id,0),CASE WHEN COALESCE(d.assigned_by_id,0)=0 THEN 'Sem responsável' ELSE COALESCE(NULLIF(u.display_name,''),'Responsável não sincronizado') END,COUNT(*) FROM deals d LEFT JOIN bitrix_users u ON u.bitrix_user_id=d.assigned_by_id WHERE d.crm_sync_run_id=$1 GROUP BY COALESCE(d.assigned_by_id,0),u.display_name ORDER BY COUNT(*) DESC`, runID)
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var item statsdomain.AssigneeCount
		if err = rows.Scan(&item.ID, &item.Name, &item.Count); err != nil {
			rows.Close()
			return out, err
		}
		out.ByAssignee = append(out.ByAssignee, item)
	}
	rows.Close()
	start := time.Date(now.In(location).Year(), now.In(location).Month(), 1, 0, 0, 0, 0, location).AddDate(0, -11, 0)
	monthCounts := make(map[string]int)
	rows, err = r.pool.Query(ctx, `SELECT to_char(date_trunc('month',created_at_bitrix AT TIME ZONE $2),'YYYY-MM'),COUNT(*) FROM deals WHERE crm_sync_run_id=$1 AND created_at_bitrix >= $3 AND created_at_bitrix < $4 GROUP BY 1`, runID, location.String(), start.UTC(), start.AddDate(1, 0, 0).UTC())
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var month string
		var count int
		if err = rows.Scan(&month, &count); err != nil {
			rows.Close()
			return out, err
		}
		monthCounts[month] = count
	}
	rows.Close()
	out.ByMonth = statsdomain.LastTwelveCalendarMonths(now, location, monthCounts)
	wonByMonth := make(map[string]int)
	lostByMonth := make(map[string]int)
	rows, err = r.pool.Query(ctx, `SELECT to_char(date_trunc('month',created_at_bitrix AT TIME ZONE $2),'YYYY-MM'),COUNT(*) FILTER (WHERE UPPER(TRIM(COALESCE(stage_semantic_id,'')))='S'),COUNT(*) FILTER (WHERE UPPER(TRIM(COALESCE(stage_semantic_id,'')))='F') FROM deals WHERE crm_sync_run_id=$1 AND created_at_bitrix >= $3 AND created_at_bitrix < $4 GROUP BY 1`, runID, location.String(), start.UTC(), start.AddDate(1, 0, 0).UTC())
	if err != nil {
		return out, err
	}
	for rows.Next() {
		var month string
		var won, lost int
		if err = rows.Scan(&month, &won, &lost); err != nil {
			rows.Close()
			return out, err
		}
		wonByMonth[month] = won
		lostByMonth[month] = lost
	}
	if err = rows.Err(); err != nil {
		rows.Close()
		return out, err
	}
	rows.Close()
	out.OutcomesByMonth = statsdomain.LastTwelveMonthlyOutcomes(now, location, wonByMonth, lostByMonth)
	if markerAvailable {
		if err = r.pool.QueryRow(ctx, `SELECT COUNT(*) FROM deal_channel_markers WHERE sync_run_id=$1 AND marker='3264'`, runID).Scan(&out.Marker3264Total); err != nil {
			return out, err
		}
		rows, err = r.pool.Query(ctx, `SELECT COALESCE(d.assigned_by_id,0),CASE WHEN COALESCE(d.assigned_by_id,0)=0 THEN 'Sem responsável' ELSE COALESCE(NULLIF(u.display_name,''),'Responsável não sincronizado') END,COUNT(*) FROM deal_channel_markers m JOIN deals d ON d.id=m.deal_id LEFT JOIN bitrix_users u ON u.bitrix_user_id=d.assigned_by_id WHERE m.sync_run_id=$1 AND m.marker='3264' GROUP BY COALESCE(d.assigned_by_id,0),u.display_name ORDER BY COUNT(*) DESC`, runID)
		if err != nil {
			return out, err
		}
		for rows.Next() {
			var item statsdomain.MarkerAssignment
			if err = rows.Scan(&item.ID, &item.Name, &item.Count); err != nil {
				rows.Close()
				return out, err
			}
			if out.Marker3264Total > 0 {
				item.PercentageBasis = statsdomain.PercentageBasisPoints(item.Count, out.Marker3264Total)
			}
			out.Marker3264ByAssignee = append(out.Marker3264ByAssignee, item)
		}
		rows.Close()
	}
	sort.SliceStable(out.Marker3264ByAssignee, func(i, j int) bool { return out.Marker3264ByAssignee[i].Count > out.Marker3264ByAssignee[j].Count })
	return out, nil
}

var _ statsdomain.Repository = (*CRMStatisticsRepository)(nil)
