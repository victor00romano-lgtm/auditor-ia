package postgres

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/jackc/pgx/v5"
	"github.com/jackc/pgx/v5/pgxpool"
	batch "github.com/portfolio/auditor-ia/internal/batch/domain"
)

type BatchQueue struct{ pool *pgxpool.Pool }

func NewBatchQueue(pool *pgxpool.Pool) *BatchQueue { return &BatchQueue{pool: pool} }

func (q *BatchQueue) Create(ctx context.Context, dealIDs []int64) (batch.Batch, error) {
	if len(dealIDs) == 0 {
		return batch.Batch{}, fmt.Errorf("lote deve possuir ao menos um negócio")
	}
	tx, err := q.pool.Begin(ctx)
	if err != nil {
		return batch.Batch{}, err
	}
	defer tx.Rollback(ctx)
	var out batch.Batch
	err = tx.QueryRow(ctx, `INSERT INTO audit_batches(total_items) VALUES($1) RETURNING id,status,created_at`, len(dealIDs)).Scan(&out.ID, &out.Status, &out.CreatedAt)
	if err != nil {
		return out, err
	}
	seen := map[int64]struct{}{}
	for _, id := range dealIDs {
		if id <= 0 {
			return out, fmt.Errorf("bitrix_deal_id inválido")
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		if _, err = tx.Exec(ctx, `INSERT INTO audit_batch_items(batch_id,bitrix_deal_id) VALUES($1,$2)`, out.ID, id); err != nil {
			return out, err
		}
	}
	out.Total = len(seen)
	if _, err = tx.Exec(ctx, `UPDATE audit_batches SET total_items=$2 WHERE id=$1`, out.ID, out.Total); err != nil {
		return out, err
	}
	out.Pending = out.Total
	return out, tx.Commit(ctx)
}

func (q *BatchQueue) Get(ctx context.Context, id int64) (batch.Batch, error) {
	var out batch.Batch
	err := q.pool.QueryRow(ctx, `SELECT b.id,b.status,b.total_items,
 COUNT(i.id) FILTER(WHERE i.status='PENDING'),COUNT(i.id) FILTER(WHERE i.status='PROCESSING'),COUNT(i.id) FILTER(WHERE i.status='COMPLETED'),COUNT(i.id) FILTER(WHERE i.status='FAILED'),
 b.created_at,b.started_at,b.finished_at FROM audit_batches b LEFT JOIN audit_batch_items i ON i.batch_id=b.id WHERE b.id=$1 GROUP BY b.id`, id).Scan(&out.ID, &out.Status, &out.Total, &out.Pending, &out.Processing, &out.Completed, &out.Failed, &out.CreatedAt, &out.StartedAt, &out.FinishedAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return out, batch.ErrNotFound
	}
	return out, err
}

func (q *BatchQueue) Cancel(ctx context.Context, id int64) error {
	tag, err := q.pool.Exec(ctx, `WITH changed AS (UPDATE audit_batches SET status='CANCELED',finished_at=NOW() WHERE id=$1 AND status IN ('PENDING','RUNNING') RETURNING id) UPDATE audit_batch_items SET status='CANCELED',finished_at=NOW(),locked_by=NULL,lease_expires_at=NULL WHERE batch_id IN(SELECT id FROM changed) AND status='PENDING'`, id)
	if err != nil {
		return err
	}
	if tag.RowsAffected() == 0 {
		var exists bool
		if err = q.pool.QueryRow(ctx, `SELECT EXISTS(SELECT 1 FROM audit_batches WHERE id=$1)`, id).Scan(&exists); err != nil {
			return err
		}
		if !exists {
			return batch.ErrNotFound
		}
	}
	return nil
}

func (q *BatchQueue) Claim(ctx context.Context, worker string, lease time.Duration) (*batch.Item, error) {
	var out batch.Item
	err := q.pool.QueryRow(ctx, `WITH candidate AS (SELECT i.id FROM audit_batch_items i JOIN audit_batches b ON b.id=i.batch_id WHERE i.status='PENDING' AND i.available_at<=NOW() AND b.status IN('PENDING','RUNNING') ORDER BY i.available_at,i.id FOR UPDATE OF i SKIP LOCKED LIMIT 1), claimed AS (UPDATE audit_batch_items i SET status='PROCESSING',attempts=attempts+1,locked_by=$1,lease_expires_at=NOW()+$2::interval FROM candidate c WHERE i.id=c.id RETURNING i.*) SELECT id,batch_id,bitrix_deal_id,status,attempts,lease_expires_at FROM claimed`, worker, lease.String()).Scan(&out.ID, &out.BatchID, &out.BitrixDealID, &out.Status, &out.Attempts, &out.LeaseExpiresAt)
	if errors.Is(err, pgx.ErrNoRows) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	_, err = q.pool.Exec(ctx, `UPDATE audit_batches SET status='RUNNING',started_at=COALESCE(started_at,NOW()) WHERE id=$1 AND status='PENDING'`, out.BatchID)
	return &out, err
}

func (q *BatchQueue) Complete(ctx context.Context, item batch.Item, assessmentID int64) error {
	return q.finish(ctx, item, `status='COMPLETED',assessment_id=$3,finished_at=NOW()`, assessmentID)
}
func (q *BatchQueue) Fail(ctx context.Context, item batch.Item, category, message string) error {
	return q.finish(ctx, item, `status=CASE WHEN EXISTS(SELECT 1 FROM audit_batches b WHERE b.id=$2 AND b.status='CANCELED') THEN 'CANCELED' ELSE 'FAILED' END,failure_category=CASE WHEN EXISTS(SELECT 1 FROM audit_batches b WHERE b.id=$2 AND b.status='CANCELED') THEN NULL ELSE $3 END,safe_error=CASE WHEN EXISTS(SELECT 1 FROM audit_batches b WHERE b.id=$2 AND b.status='CANCELED') THEN NULL ELSE $4 END,finished_at=NOW()`, category, message)
}

func (q *BatchQueue) finish(ctx context.Context, item batch.Item, set string, values ...any) error {
	tx, err := q.pool.Begin(ctx)
	if err != nil {
		return err
	}
	defer tx.Rollback(ctx)
	args := append([]any{item.ID, item.BatchID}, values...)
	tag, err := tx.Exec(ctx, `UPDATE audit_batch_items SET `+set+`,locked_by=NULL,lease_expires_at=NULL WHERE id=$1 AND batch_id=$2 AND status='PROCESSING'`, args...)
	if err != nil {
		return err
	}
	if tag.RowsAffected() != 1 {
		return fmt.Errorf("item de lote não está reservado")
	}
	if err = finalizeBatch(ctx, tx, item.BatchID); err != nil {
		return err
	}
	return tx.Commit(ctx)
}

func (q *BatchQueue) Reschedule(ctx context.Context, item batch.Item, available time.Time, category, message string) error {
	tag, err := q.pool.Exec(ctx, `UPDATE audit_batch_items i SET status=CASE WHEN b.status='CANCELED' THEN 'CANCELED' ELSE 'PENDING' END,available_at=$3,failure_category=$4,safe_error=$5,finished_at=CASE WHEN b.status='CANCELED' THEN NOW() ELSE NULL END,locked_by=NULL,lease_expires_at=NULL FROM audit_batches b WHERE i.id=$1 AND i.batch_id=$2 AND i.batch_id=b.id AND i.status='PROCESSING'`, item.ID, item.BatchID, available, category, message)
	if err == nil && tag.RowsAffected() != 1 {
		return fmt.Errorf("item de lote não está reservado")
	}
	return err
}

func (q *BatchQueue) RecoverExpired(ctx context.Context) (int64, error) {
	tag, err := q.pool.Exec(ctx, `UPDATE audit_batch_items i SET status=CASE WHEN b.status='CANCELED' THEN 'CANCELED' ELSE 'PENDING' END,available_at=NOW(),finished_at=CASE WHEN b.status='CANCELED' THEN NOW() ELSE NULL END,locked_by=NULL,lease_expires_at=NULL,failure_category='LEASE_EXPIRED',safe_error='Processamento interrompido; item reagendado.' FROM audit_batches b WHERE i.batch_id=b.id AND i.status='PROCESSING' AND i.lease_expires_at<NOW()`)
	return tag.RowsAffected(), err
}

func finalizeBatch(ctx context.Context, tx pgx.Tx, batchID int64) error {
	_, err := tx.Exec(ctx, `UPDATE audit_batches b SET status=CASE WHEN counts.active>0 THEN b.status WHEN counts.failed=0 THEN 'COMPLETED' WHEN counts.completed=0 THEN 'FAILED' ELSE 'PARTIAL' END,finished_at=CASE WHEN counts.active=0 THEN NOW() ELSE NULL END FROM (SELECT COUNT(*) FILTER(WHERE status IN('PENDING','PROCESSING')) active,COUNT(*) FILTER(WHERE status='COMPLETED') completed,COUNT(*) FILTER(WHERE status='FAILED') failed FROM audit_batch_items WHERE batch_id=$1) counts WHERE b.id=$1 AND b.status<>'CANCELED'`, batchID)
	return err
}

var _ batch.Queue = (*BatchQueue)(nil)
