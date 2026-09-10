package postgres

import (
	"context"
	"os"
	"strconv"
	"sync"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	batch "github.com/portfolio/auditor-ia/internal/batch/domain"
)

func TestBatchQueueClaimsDistinctItemsAndPersistsProgress(t *testing.T) {
	pool := openBatchTestPool(t)
	queue := NewBatchQueue(pool)
	created, err := queue.Create(context.Background(), []int64{900000001, 900000002})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM audit_batches WHERE id=$1`, created.ID) })
	var items [2]*batch.Item
	var errs [2]error
	var group sync.WaitGroup
	for i := range 2 {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			items[index], errs[index] = queue.Claim(context.Background(), "test-worker", time.Minute)
		}(i)
	}
	group.Wait()
	for _, claimErr := range errs {
		if claimErr != nil {
			t.Fatal(claimErr)
		}
	}
	if items[0] == nil || items[1] == nil || items[0].ID == items[1].ID {
		t.Fatalf("claims duplicados: %#v %#v", items[0], items[1])
	}
	if err = queue.Reschedule(context.Background(), *items[0], time.Now(), "TEST", "Falha simulada."); err != nil {
		t.Fatal(err)
	}
	if err = queue.Fail(context.Background(), *items[1], "TEST", "Falha simulada."); err != nil {
		t.Fatal(err)
	}
	claimedAgain, err := queue.Claim(context.Background(), "test-worker", time.Minute)
	if err != nil || claimedAgain == nil {
		t.Fatalf("retomada=%#v err=%v", claimedAgain, err)
	}
	if err = queue.Fail(context.Background(), *claimedAgain, "TEST", "Falha simulada."); err != nil {
		t.Fatal(err)
	}
	progress, err := queue.Get(context.Background(), created.ID)
	if err != nil {
		t.Fatal(err)
	}
	if progress.Status != batch.Failed || progress.Completed != 0 || progress.Failed != 2 || progress.Pending != 0 || progress.Processing != 0 {
		t.Fatalf("progresso=%#v", progress)
	}
}

func TestBatchQueueCompleteFailRetryAndCancel(t *testing.T) {
	ctx := context.Background()
	pool := openBatchTestPool(t)
	queue := NewBatchQueue(pool)

	t.Run("complete", func(t *testing.T) {
		created := createTestBatch(t, queue, pool, []int64{910000001})
		item, err := queue.Claim(ctx, "complete-worker", time.Minute)
		if err != nil || item == nil || item.BatchID != created.ID {
			t.Fatalf("claim=%#v err=%v", item, err)
		}
		assessmentID, dealID := createCompletedAssessment(t, pool, item.BitrixDealID, "batch-item-"+int64Text(item.ID))
		t.Cleanup(func() {
			_, _ = pool.Exec(ctx, `DELETE FROM audit_batches WHERE id=$1`, created.ID)
			_, _ = pool.Exec(ctx, `DELETE FROM deals WHERE id=$1`, dealID)
		})
		if err = queue.Complete(ctx, *item, assessmentID); err != nil {
			t.Fatal(err)
		}
		progress, err := queue.Get(ctx, created.ID)
		if err != nil || progress.Status != batch.Completed || progress.Completed != 1 {
			t.Fatalf("progress=%#v err=%v", progress, err)
		}
	})

	t.Run("fail", func(t *testing.T) {
		created := createTestBatch(t, queue, pool, []int64{910000002})
		item, err := queue.Claim(ctx, "fail-worker", time.Minute)
		if err != nil || item == nil || item.BatchID != created.ID {
			t.Fatalf("claim=%#v err=%v", item, err)
		}
		if err = queue.Fail(ctx, *item, "TEST_FAILURE", "Falha segura."); err != nil {
			t.Fatal(err)
		}
		progress, err := queue.Get(ctx, created.ID)
		if err != nil || progress.Status != batch.Failed || progress.Failed != 1 {
			t.Fatalf("progress=%#v err=%v", progress, err)
		}
	})

	t.Run("retry", func(t *testing.T) {
		created := createTestBatch(t, queue, pool, []int64{910000003})
		item, err := queue.Claim(ctx, "retry-worker", time.Minute)
		if err != nil || item == nil || item.BatchID != created.ID {
			t.Fatalf("claim=%#v err=%v", item, err)
		}
		if err = queue.Reschedule(ctx, *item, time.Now().Add(-time.Second), "TIMEOUT", "Falha temporária."); err != nil {
			t.Fatal(err)
		}
		retried, err := queue.Claim(ctx, "retry-worker-2", time.Minute)
		if err != nil || retried == nil || retried.ID != item.ID || retried.Attempts != 2 {
			t.Fatalf("retry=%#v err=%v", retried, err)
		}
		if err = queue.Fail(ctx, *retried, "TEST_END", "Fim do teste."); err != nil {
			t.Fatal(err)
		}
	})

	t.Run("cancel", func(t *testing.T) {
		created := createTestBatch(t, queue, pool, []int64{910000004, 910000005, 910000006})
		processingRetry, err := queue.Claim(ctx, "cancel-worker-retry", time.Minute)
		if err != nil || processingRetry == nil || processingRetry.BatchID != created.ID {
			t.Fatalf("claim retry=%#v err=%v", processingRetry, err)
		}
		processingFailure, err := queue.Claim(ctx, "cancel-worker-failure", time.Minute)
		if err != nil || processingFailure == nil || processingFailure.BatchID != created.ID {
			t.Fatalf("claim failure=%#v err=%v", processingFailure, err)
		}
		if err = queue.Cancel(ctx, created.ID); err != nil {
			t.Fatal(err)
		}
		if err = queue.Reschedule(ctx, *processingRetry, time.Now(), "CANCELED", "Processo encerrado."); err != nil {
			t.Fatal(err)
		}
		if err = queue.Fail(ctx, *processingFailure, "CANCELED", "Processo encerrado."); err != nil {
			t.Fatal(err)
		}
		var canceled int
		if err = pool.QueryRow(ctx, `SELECT COUNT(*) FROM audit_batch_items WHERE batch_id=$1 AND status='CANCELED'`, created.ID).Scan(&canceled); err != nil || canceled != 3 {
			t.Fatalf("cancelados=%d err=%v", canceled, err)
		}
	})
}

func TestBatchQueueRecoveryReusesCompletedAssessmentByExecutionKey(t *testing.T) {
	ctx := context.Background()
	pool := openBatchTestPool(t)
	queue := NewBatchQueue(pool)
	created := createTestBatch(t, queue, pool, []int64{920000001})
	item, err := queue.Claim(ctx, "worker-before-recovery", time.Minute)
	if err != nil || item == nil {
		t.Fatalf("claim=%#v err=%v", item, err)
	}
	key := "batch-item-" + int64Text(item.ID)
	assessmentID, dealID := createCompletedAssessment(t, pool, item.BitrixDealID, key)
	t.Cleanup(func() {
		_, _ = pool.Exec(ctx, `DELETE FROM audit_batches WHERE id=$1`, created.ID)
		_, _ = pool.Exec(ctx, `DELETE FROM deals WHERE id=$1`, dealID)
	})
	if _, err = pool.Exec(ctx, `UPDATE audit_batch_items SET lease_expires_at=NOW()-INTERVAL '1 second' WHERE id=$1`, item.ID); err != nil {
		t.Fatal(err)
	}
	if recovered, recoverErr := queue.RecoverExpired(ctx); recoverErr != nil || recovered != 1 {
		t.Fatalf("recovered=%d err=%v", recovered, recoverErr)
	}
	retried, err := queue.Claim(ctx, "worker-after-recovery", time.Minute)
	if err != nil || retried == nil || retried.ID != item.ID {
		t.Fatalf("retry=%#v err=%v", retried, err)
	}
	repository := NewAssessmentRepository(pool)
	reusedID, status, err := repository.FindExecution(ctx, key)
	if err != nil || reusedID != assessmentID || status != "COMPLETED" {
		t.Fatalf("reused=%d status=%s err=%v", reusedID, status, err)
	}
	if err = queue.Complete(ctx, *retried, reusedID); err != nil {
		t.Fatal(err)
	}
	progress, err := queue.Get(ctx, created.ID)
	if err != nil || progress.Status != batch.Completed {
		t.Fatalf("progress=%#v err=%v", progress, err)
	}
}

func openBatchTestPool(t *testing.T) *pgxpool.Pool {
	t.Helper()
	url := os.Getenv("BATCH_TEST_DATABASE_URL")
	if url == "" {
		t.Skip("BATCH_TEST_DATABASE_URL não configurada")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(pool.Close)
	return pool
}

func createTestBatch(t *testing.T, queue *BatchQueue, pool *pgxpool.Pool, dealIDs []int64) batch.Batch {
	t.Helper()
	created, err := queue.Create(context.Background(), dealIDs)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _, _ = pool.Exec(context.Background(), `DELETE FROM audit_batches WHERE id=$1`, created.ID) })
	return created
}

func createCompletedAssessment(t *testing.T, pool *pgxpool.Pool, bitrixDealID int64, executionKey string) (int64, int64) {
	t.Helper()
	ctx := context.Background()
	var dealID, assessmentID int64
	if err := pool.QueryRow(ctx, `INSERT INTO deals(bitrix_deal_id,title) VALUES($1,'Teste de lote') RETURNING id`, bitrixDealID).Scan(&dealID); err != nil {
		t.Fatal(err)
	}
	if err := pool.QueryRow(ctx, `INSERT INTO deal_assessments(deal_id,status,execution_key,finished_at) VALUES($1,'COMPLETED',$2,NOW()) RETURNING id`, dealID, executionKey).Scan(&assessmentID); err != nil {
		_, _ = pool.Exec(ctx, `DELETE FROM deals WHERE id=$1`, dealID)
		t.Fatal(err)
	}
	return assessmentID, dealID
}

func int64Text(value int64) string {
	return strconv.FormatInt(value, 10)
}
