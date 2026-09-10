package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	dashboard "github.com/portfolio/auditor-ia/internal/dashboard/domain"
)

func TestDashboardRepositoryUsesOnlyLatestAssessmentWithPostgreSQL(t *testing.T) {
	if strings.TrimSpace(os.Getenv("DATABASE_URL")) == "" {
		t.Skip("DATABASE_URL não configurada; teste de integração ignorado")
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	pool, err := Open(ctx)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	bitrixID := time.Now().UnixNano()
	var dealID, oldAssessment, latestAssessment int64
	if err := pool.QueryRow(ctx, `INSERT INTO deals(bitrix_deal_id,title) VALUES($1,'Dashboard test') RETURNING id`, bitrixID).Scan(&dealID); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(context.Background(), `DELETE FROM deals WHERE id=$1`, dealID) }()
	if err := pool.QueryRow(ctx, `INSERT INTO deal_assessments(deal_id,status,score,final_result,started_at,finished_at) VALUES($1,'COMPLETED',10,'não venda',NOW()-INTERVAL '2 hour',NOW()-INTERVAL '1 hour') RETURNING id`, dealID).Scan(&oldAssessment); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO deal_assessments(deal_id,status,score,final_result,started_at,finished_at) VALUES($1,'COMPLETED',90,'venda',NOW()-INTERVAL '30 minute',NOW()) RETURNING id`, dealID).Scan(&latestAssessment); err != nil {
		t.Fatal(err)
	}
	_, err = pool.Exec(ctx, `INSERT INTO audit_findings(assessment_id,deal_id,rule_name,description,severity) VALUES($1,$3,'old_rule','antigo','LOW'),($2,$3,'new_rule','atual','HIGH')`, oldAssessment, latestAssessment, dealID)
	if err != nil {
		t.Fatal(err)
	}
	repo := NewDashboardRepository(pool)
	page, err := repo.ListFindings(ctx, dashboard.FindingFilter{BitrixDealID: bitrixID, Limit: 25})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 1 || len(page.Items) != 1 || page.Items[0].Rule != "new_rule" {
		t.Fatalf("histórico contado como atual: %#v", page)
	}
	detail, err := repo.GetDealDetail(ctx, bitrixID)
	if err != nil {
		t.Fatal(err)
	}
	if detail.AssessmentID != latestAssessment || detail.Score == nil || *detail.Score != 90 {
		t.Fatalf("avaliação mais recente incorreta: %#v", detail)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO conversation_analyses(assessment_id,deal_id,status,probable_result,message_count,created_at) VALUES($1,$3,'COMPLETED','não venda',1,NOW()),($2,$3,'COMPLETED','venda',2,NOW()-INTERVAL '1 day')`, oldAssessment, latestAssessment, dealID); err != nil {
		t.Fatal(err)
	}
	analysis, err := repo.GetLatestConversationAnalysis(ctx, bitrixID)
	if err != nil {
		t.Fatal(err)
	}
	if analysis.AssessmentID != latestAssessment || analysis.ProbableResult != "venda" {
		t.Fatalf("análise histórica foi misturada ao estado atual: %#v", analysis)
	}
	filtered, err := repo.ListFindings(ctx, dashboard.FindingFilter{BitrixDealID: bitrixID, Severity: "HIGH", Rule: "new_rule", Limit: 1, Offset: 99})
	if err != nil {
		t.Fatal(err)
	}
	if filtered.Total != 1 || len(filtered.Items) != 1 || filtered.Offset != 0 || filtered.Items[0].AssessmentID != latestAssessment {
		t.Fatalf("total, filtros ou paginação divergentes: %#v", filtered)
	}

	secondBitrixID := bitrixID + 1
	var secondDeal, secondOld, secondLatest int64
	if err := pool.QueryRow(ctx, `INSERT INTO deals(bitrix_deal_id,title) VALUES($1,'Dashboard sem achados atuais') RETURNING id`, secondBitrixID).Scan(&secondDeal); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(context.Background(), `DELETE FROM deals WHERE id=$1`, secondDeal) }()
	if err := pool.QueryRow(ctx, `INSERT INTO deal_assessments(deal_id,status,created_at) VALUES($1,'COMPLETED',NOW()-INTERVAL '1 hour') RETURNING id`, secondDeal).Scan(&secondOld); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO deal_assessments(deal_id,status,created_at) VALUES($1,'COMPLETED',NOW()) RETURNING id`, secondDeal).Scan(&secondLatest); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO audit_findings(assessment_id,deal_id,rule_name,description,severity) VALUES($1,$2,'historical_only','antigo','MEDIUM')`, secondOld, secondDeal); err != nil {
		t.Fatal(err)
	}
	empty, err := repo.ListFindings(ctx, dashboard.FindingFilter{BitrixDealID: secondBitrixID, Limit: 25})
	if err != nil {
		t.Fatal(err)
	}
	if empty.Total != 0 || len(empty.Items) != 0 {
		t.Fatalf("achado antigo apareceu quando avaliação atual %d não possui achados: %#v", secondLatest, empty)
	}

	tieBitrixID := bitrixID + 2
	var tieDeal, lowerID, higherID int64
	if err := pool.QueryRow(ctx, `INSERT INTO deals(bitrix_deal_id,title) VALUES($1,'Dashboard empate') RETURNING id`, tieBitrixID).Scan(&tieDeal); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(context.Background(), `DELETE FROM deals WHERE id=$1`, tieDeal) }()
	if err := pool.QueryRow(ctx, `INSERT INTO deal_assessments(deal_id,status,created_at) VALUES($1,'COMPLETED','2026-01-01T00:00:00Z') RETURNING id`, tieDeal).Scan(&lowerID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO deal_assessments(deal_id,status,created_at) VALUES($1,'COMPLETED','2026-01-01T00:00:00Z') RETURNING id`, tieDeal).Scan(&higherID); err != nil {
		t.Fatal(err)
	}
	if _, err := pool.Exec(ctx, `INSERT INTO audit_findings(assessment_id,deal_id,rule_name,description,severity) VALUES($1,$3,'lower_id','antigo','LOW'),($2,$3,'higher_id','atual','HIGH')`, lowerID, higherID, tieDeal); err != nil {
		t.Fatal(err)
	}
	tiePage, err := repo.ListFindings(ctx, dashboard.FindingFilter{BitrixDealID: tieBitrixID, Limit: 25})
	if err != nil {
		t.Fatal(err)
	}
	if tiePage.Total != 1 || len(tiePage.Items) != 1 || tiePage.Items[0].AssessmentID != higherID || tiePage.Items[0].Rule != "higher_id" {
		t.Fatalf("empate de created_at não foi resolvido pelo maior id: %#v", tiePage)
	}
}
