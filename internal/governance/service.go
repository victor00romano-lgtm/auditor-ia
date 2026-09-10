package governance

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Counts struct{ Messages, Analyses, Assessments, Findings, Jobs, Deals, Reports int64 }
type DataService interface {
	PreviewDeal(context.Context, int64) (Counts, error)
	DeleteDeal(context.Context, int64, string) (Counts, error)
}
type Service struct {
	Pool       *pgxpool.Pool
	ReportsDir string
}

func (s Service) PreviewDeal(ctx context.Context, bitrixID int64) (Counts, error) {
	var c Counts
	err := s.Pool.QueryRow(ctx, `SELECT COUNT(DISTINCT d.id),COUNT(DISTINCT m.id),COUNT(DISTINCT ca.id),COUNT(DISTINCT a.id),COUNT(DISTINCT f.id),COUNT(DISTINCT j.id) FROM deals d LEFT JOIN conversation_messages m ON m.deal_id=d.id LEFT JOIN conversation_analyses ca ON ca.deal_id=d.id LEFT JOIN deal_assessments a ON a.deal_id=d.id LEFT JOIN audit_findings f ON f.deal_id=d.id LEFT JOIN analysis_jobs j ON j.deal_id=d.id WHERE d.bitrix_deal_id=$1`, bitrixID).Scan(&c.Deals, &c.Messages, &c.Analyses, &c.Assessments, &c.Findings, &c.Jobs)
	if err != nil {
		return c, fmt.Errorf("contar dados do negócio: %w", err)
	}
	ids, err := s.assessmentIDs(ctx, bitrixID)
	if err != nil {
		return c, err
	}
	files, err := safeReportFiles(s.ReportsDir, ids)
	if err != nil {
		return c, err
	}
	c.Reports = int64(len(files))
	return c, nil
}
func (s Service) DeleteDeal(ctx context.Context, bitrixID int64, confirm string) (Counts, error) {
	if fmt.Sprint(bitrixID) != confirm {
		return Counts{}, fmt.Errorf("confirmação do negócio divergente")
	}
	c, err := s.PreviewDeal(ctx, bitrixID)
	if err != nil {
		return c, err
	}
	ids, err := s.assessmentIDs(ctx, bitrixID)
	if err != nil {
		return c, err
	}
	files, err := safeReportFiles(s.ReportsDir, ids)
	if err != nil {
		return c, err
	}
	tx, err := s.Pool.Begin(ctx)
	if err != nil {
		return c, fmt.Errorf("iniciar exclusão: %w", err)
	}
	defer tx.Rollback(ctx)
	if _, err = tx.Exec(ctx, `DELETE FROM deals WHERE bitrix_deal_id=$1`, bitrixID); err != nil {
		return c, fmt.Errorf("excluir negócio: %w", err)
	}
	if err = tx.Commit(ctx); err != nil {
		return c, fmt.Errorf("confirmar exclusão: %w", err)
	}
	for _, file := range files {
		if err = os.Remove(file); err != nil {
			return c, fmt.Errorf("remover relatório após exclusão confirmada: %w", err)
		}
	}
	return c, nil
}
func (s Service) assessmentIDs(ctx context.Context, id int64) (map[int64]bool, error) {
	rows, err := s.Pool.Query(ctx, `SELECT a.id FROM deal_assessments a JOIN deals d ON d.id=a.deal_id WHERE d.bitrix_deal_id=$1`, id)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	out := map[int64]bool{}
	for rows.Next() {
		var v int64
		if err = rows.Scan(&v); err != nil {
			return nil, err
		}
		out[v] = true
	}
	return out, rows.Err()
}

var reportName = regexp.MustCompile(`^auditoria-(\d+)(?:-\d{8}-\d{6})?\.pdf$`)

func safeReportFiles(dir string, ids map[int64]bool) ([]string, error) {
	if dir == "" {
		dir = "./reports"
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, fmt.Errorf("resolver REPORTS_DIR: %w", err)
	}
	if filepath.Clean(root) == filepath.VolumeName(root)+string(os.PathSeparator) {
		return nil, fmt.Errorf("REPORTS_DIR não pode ser raiz")
	}
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, entry := range entries {
		if entry.IsDir() {
			continue
		}
		match := reportName.FindStringSubmatch(entry.Name())
		if len(match) != 2 {
			continue
		}
		var id int64
		_, _ = fmt.Sscan(match[1], &id)
		if ids[id] {
			candidate := filepath.Join(root, entry.Name())
			if filepath.Dir(candidate) != root {
				return nil, fmt.Errorf("caminho de relatório inseguro")
			}
			out = append(out, candidate)
		}
	}
	return out, nil
}

type RetentionPolicy struct{ MessagesDays, RawDays, AssessmentsDays, ReportsDays, Batch int }

func (s Service) Retention(ctx context.Context, p RetentionPolicy, apply bool) (map[string]int64, error) {
	now := time.Now()
	out := map[string]int64{}
	queries := []struct {
		name       string
		days       int
		count, del string
	}{{"messages", p.MessagesDays, `SELECT COUNT(*) FROM conversation_messages WHERE sent_at<$1`, `DELETE FROM conversation_messages WHERE id IN (SELECT id FROM conversation_messages WHERE sent_at<$1 LIMIT $2)`}, {"raw_analysis", p.RawDays, `SELECT COUNT(*) FROM conversation_analyses WHERE raw_response IS NOT NULL AND raw_response<>'' AND created_at<$1`, `UPDATE conversation_analyses SET raw_response=NULL WHERE id IN (SELECT id FROM conversation_analyses WHERE raw_response IS NOT NULL AND raw_response<>'' AND created_at<$1 LIMIT $2)`}, {"assessments", p.AssessmentsDays, `SELECT COUNT(*) FROM deal_assessments WHERE created_at<$1`, `DELETE FROM deal_assessments WHERE id IN (SELECT id FROM deal_assessments WHERE created_at<$1 LIMIT $2)`}}
	if p.Batch <= 0 {
		p.Batch = 500
	}
	for _, q := range queries {
		if q.days == 0 {
			continue
		}
		cutoff := now.AddDate(0, 0, -q.days)
		var count int64
		if err := s.Pool.QueryRow(ctx, q.count, cutoff).Scan(&count); err != nil {
			return out, err
		}
		out[q.name] = count
		if apply {
			for {
				tag, err := s.Pool.Exec(ctx, q.del, cutoff, p.Batch)
				if err != nil {
					return out, fmt.Errorf("aplicar retenção %s: %w", q.name, err)
				}
				if tag.RowsAffected() < int64(p.Batch) {
					break
				}
			}
		}
	}
	if p.ReportsDays > 0 {
		files, err := retentionReports(s.ReportsDir, now.AddDate(0, 0, -p.ReportsDays))
		if err != nil {
			return out, err
		}
		out["reports"] = int64(len(files))
		if apply {
			for _, file := range files {
				if err = os.Remove(file); err != nil {
					return out, fmt.Errorf("remover relatório: %w", err)
				}
			}
		}
	}
	return out, nil
}
func retentionReports(dir string, cutoff time.Time) ([]string, error) {
	if dir == "" {
		dir = "./reports"
	}
	root, err := filepath.Abs(dir)
	if err != nil {
		return nil, err
	}
	if filepath.Clean(root) == filepath.VolumeName(root)+string(os.PathSeparator) {
		return nil, fmt.Errorf("REPORTS_DIR não pode ser raiz")
	}
	entries, err := os.ReadDir(root)
	if os.IsNotExist(err) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	var out []string
	for _, entry := range entries {
		if entry.IsDir() || !reportName.MatchString(entry.Name()) {
			continue
		}
		info, err := entry.Info()
		if err != nil {
			return nil, err
		}
		if info.ModTime().Before(cutoff) {
			out = append(out, filepath.Join(root, entry.Name()))
		}
	}
	return out, nil
}
