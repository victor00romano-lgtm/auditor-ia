package postgres

import (
	"context"
	"os"
	"strings"
	"testing"
	"time"

	apidomain "github.com/portfolio/auditor-ia/internal/api/domain"
)

func TestAPIRepositoryReturnsHistoricalAssessmentsWithPostgreSQL(t *testing.T) {
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
	var dealID int64
	if err := pool.QueryRow(ctx, `INSERT INTO deals(bitrix_deal_id,title) VALUES($1,'API integration') RETURNING id`, bitrixID).Scan(&dealID); err != nil {
		t.Fatal(err)
	}
	defer func() { _, _ = pool.Exec(context.Background(), `DELETE FROM deals WHERE id=$1`, dealID) }()
	for index := 0; index < 2; index++ {
		if _, err := pool.Exec(ctx, `INSERT INTO deal_assessments(deal_id,status,score,final_result,started_at,finished_at) VALUES($1,'COMPLETED',$2,'em negociação',NOW(),NOW())`, dealID, 80+index); err != nil {
			t.Fatal(err)
		}
	}
	repo := NewAPIRepository(pool)
	page, err := repo.ListAudits(ctx, apidomain.AuditFilter{Page: 1, PageSize: 20, BitrixDealID: bitrixID})
	if err != nil {
		t.Fatal(err)
	}
	if page.Total != 2 || len(page.Items) != 2 || page.Items[0].AssessmentID == page.Items[1].AssessmentID {
		t.Fatalf("histórico inesperado: %#v", page)
	}
	if page.Items[0].Score == nil {
		t.Fatal("score não retornado")
	}
}
