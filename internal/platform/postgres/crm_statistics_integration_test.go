package postgres

import (
	"context"
	"os"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	statsdomain "github.com/portfolio/auditor-ia/internal/crmstats/domain"
	dealdomain "github.com/portfolio/auditor-ia/internal/deal/domain"
)

func TestCRMStatisticsRepositoryPublishesAndGroupsSnapshot(t *testing.T) {
	url := os.Getenv("CRM_STATS_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("CRM_STATS_TEST_DATABASE_URL não configurada")
	}
	ctx := context.Background()
	pool, err := pgxpool.New(ctx, url)
	if err != nil {
		t.Fatal(err)
	}
	defer pool.Close()
	repository := NewCRMStatisticsRepository(pool)
	runID, err := repository.Start(ctx)
	if err != nil {
		t.Fatal(err)
	}
	baseID := time.Now().UnixNano() / 1000
	assigned := int64(700001)
	createdCurrent := time.Date(2026, 8, 10, 12, 0, 0, 0, time.FixedZone("BRT", -3*60*60))
	createdOld := createdCurrent.AddDate(0, -2, 0)
	won := "S"
	lost := "F"
	snapshot := statsdomain.Snapshot{
		Deals: []dealdomain.Deal{
			{BitrixDealID: baseID, AssignedByID: &assigned, CreatedAtBitrix: &createdCurrent, StageSemanticID: &won},
			{BitrixDealID: baseID + 1, AssignedByID: &assigned, CreatedAtBitrix: &createdOld, StageSemanticID: &lost},
			{BitrixDealID: baseID + 2, CreatedAtBitrix: &createdCurrent},
		},
		Users:         []dealdomain.Assignee{{BitrixUserID: assigned, DisplayName: "Débora Teste", SyncedAt: time.Now()}},
		MarkerDealIDs: []int64{baseID, baseID, baseID + 2}, ActivitiesCollected: 4, MarkerAvailable: true,
	}
	if err = repository.Publish(ctx, runID, snapshot); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM deals WHERE crm_sync_run_id=$1`, runID)
		_, _ = pool.Exec(ctx, `DELETE FROM crm_sync_runs WHERE id=$1`, runID)
		_, _ = pool.Exec(ctx, `DELETE FROM bitrix_users WHERE bitrix_user_id=$1`, assigned)
	})
	location, _ := time.LoadLocation("America/Sao_Paulo")
	stats, err := repository.Statistics(ctx, createdCurrent, location)
	if err != nil {
		t.Fatal(err)
	}
	if stats.TotalDeals != 3 || stats.WonDeals != 1 || stats.LostDeals != 1 || len(stats.ByAssignee) != 2 || stats.Marker3264Total != 2 || len(stats.Marker3264ByAssignee) != 2 || len(stats.ByMonth) != 12 || len(stats.OutcomesByMonth) != 12 {
		t.Fatalf("estatísticas=%#v", stats)
	}
	if stats.OutcomesByMonth[9].Lost != 1 || stats.OutcomesByMonth[11].Won != 1 {
		t.Fatalf("resultados mensais=%#v", stats.OutcomesByMonth)
	}
	if stats.ByAssignee[0].Name != "Débora Teste" || stats.ByAssignee[0].Count != 2 {
		t.Fatalf("responsáveis=%#v", stats.ByAssignee)
	}
	foundWithoutAssignee := false
	for _, item := range stats.ByAssignee {
		if item.Name == "Sem responsável" && item.Count == 1 {
			foundWithoutAssignee = true
		}
	}
	if !foundWithoutAssignee {
		t.Fatalf("sem responsável ausente: %#v", stats.ByAssignee)
	}
}
