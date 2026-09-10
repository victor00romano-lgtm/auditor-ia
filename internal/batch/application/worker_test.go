package application

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/portfolio/auditor-ia/internal/batch/domain"
)

var errStop = errors.New("stop")

type queueStub struct {
	mu                             sync.Mutex
	items                          []*domain.Item
	completed, failed, rescheduled int
	completeCalls                  int
	completeErrors                 []error
	available                      time.Time
	category                       string
}

func (q *queueStub) Create(context.Context, []int64) (domain.Batch, error) {
	return domain.Batch{}, nil
}
func (q *queueStub) Get(context.Context, int64) (domain.Batch, error) { return domain.Batch{}, nil }
func (q *queueStub) Cancel(context.Context, int64) error              { return nil }
func (q *queueStub) Claim(context.Context, string, time.Duration) (*domain.Item, error) {
	q.mu.Lock()
	defer q.mu.Unlock()
	if len(q.items) == 0 {
		return nil, errStop
	}
	v := q.items[0]
	q.items = q.items[1:]
	return v, nil
}
func (q *queueStub) Complete(_ context.Context, _ domain.Item, _ int64) error {
	q.mu.Lock()
	defer q.mu.Unlock()
	q.completeCalls++
	if len(q.completeErrors) > 0 {
		err := q.completeErrors[0]
		q.completeErrors = q.completeErrors[1:]
		if err != nil {
			return err
		}
	}
	q.completed++
	return nil
}
func (q *queueStub) Reschedule(_ context.Context, _ domain.Item, at time.Time, category, _ string) error {
	q.rescheduled++
	q.available = at
	q.category = category
	return nil
}
func (q *queueStub) Fail(_ context.Context, _ domain.Item, category, _ string) error {
	q.failed++
	q.category = category
	return nil
}
func (q *queueStub) RecoverExpired(context.Context) (int64, error) { return 0, nil }

type processorStub struct {
	assessment int64
	err        error
}

func (p processorStub) Run(context.Context, int64) (int64, error) { return p.assessment, p.err }

type processorFunc func(context.Context, int64) (int64, error)

func (f processorFunc) Run(ctx context.Context, dealID int64) (int64, error) {
	return f(ctx, dealID)
}

func TestWorkerCompletesClaimedItem(t *testing.T) {
	q := &queueStub{items: []*domain.Item{{ID: 1, BatchID: 2, BitrixDealID: 3, Attempts: 1}}}
	w := Worker{Queue: q, Processor: processorStub{assessment: 44}, Now: time.Now}
	if err := w.Run(context.Background()); !errors.Is(err, errStop) {
		t.Fatalf("err=%v", err)
	}
	if q.completed != 1 || q.failed != 0 {
		t.Fatalf("completed=%d failed=%d", q.completed, q.failed)
	}
}
func TestWorkerRetriesTransientFailureWithPersistentSchedule(t *testing.T) {
	now := time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC)
	q := &queueStub{items: []*domain.Item{{ID: 1, BatchID: 2, BitrixDealID: 3, Attempts: 2}}}
	w := Worker{Queue: q, Processor: processorStub{err: context.DeadlineExceeded}, Now: func() time.Time { return now }, MaxAttempts: 5}
	_ = w.Run(context.Background())
	if q.rescheduled != 1 || q.failed != 0 || q.category != "TIMEOUT" {
		t.Fatalf("retry=%d failed=%d category=%s", q.rescheduled, q.failed, q.category)
	}
	if q.available.Before(now.Add(2 * time.Minute)) {
		t.Fatalf("backoff=%v", q.available.Sub(now))
	}
}
func TestWorkerDoesNotRetryPermanentOrExhaustedFailure(t *testing.T) {
	for _, tc := range []struct {
		name     string
		item     *domain.Item
		err      error
		category string
	}{{"access denied", &domain.Item{Attempts: 1}, errors.New("ACCESS_DENIED"), "ACCESS_DENIED"}, {"exhausted", &domain.Item{Attempts: 5}, context.DeadlineExceeded, "TIMEOUT"}} {
		t.Run(tc.name, func(t *testing.T) {
			q := &queueStub{items: []*domain.Item{tc.item}}
			w := Worker{Queue: q, Processor: processorStub{err: tc.err}, MaxAttempts: 5}
			_ = w.Run(context.Background())
			if q.failed != 1 || q.rescheduled != 0 || q.category != tc.category {
				t.Fatalf("failed=%d retry=%d category=%s", q.failed, q.rescheduled, q.category)
			}
		})
	}
}
func TestRetryClassification(t *testing.T) {
	tests := []struct {
		err      error
		category string
		retry    bool
	}{{errors.New("HTTP 429"), "RATE_LIMIT", true}, {errors.New("HTTP 503"), "DEPENDENCY", true}, {errors.New("HTTP 401"), "AUTH", false}, {context.Canceled, "CANCELED", false}}
	for _, tc := range tests {
		category, retry := Classify(tc.err)
		if category != tc.category || retry != tc.retry {
			t.Fatalf("%v => %s,%t", tc.err, category, retry)
		}
	}
}

func TestCompleteErrorDoesNotPreventWorkerFromProcessingNextItem(t *testing.T) {
	q := &queueStub{
		items: []*domain.Item{
			{ID: 1, BatchID: 10, BitrixDealID: 101, Attempts: 1},
			{ID: 2, BatchID: 10, BitrixDealID: 102, Attempts: 1},
		},
		completeErrors: []error{errors.New("falha temporária no PostgreSQL")},
	}
	w := Worker{Queue: q, Processor: processorStub{assessment: 44}}
	if err := w.Run(context.Background()); !errors.Is(err, errStop) {
		t.Fatalf("err=%v", err)
	}
	if q.completeCalls != 2 || q.completed != 1 {
		t.Fatalf("complete calls=%d completed=%d", q.completeCalls, q.completed)
	}
}

func TestExecutionKeyIsStableWhenLeaseRecoveryRetriesCompletedAssessment(t *testing.T) {
	q := &queueStub{
		items: []*domain.Item{
			{ID: 7, BatchID: 20, BitrixDealID: 107, Attempts: 1},
			{ID: 7, BatchID: 20, BitrixDealID: 107, Attempts: 2},
		},
		completeErrors: []error{errors.New("conexão perdida após avaliação concluída")},
	}
	var keys []string
	processor := processorFunc(func(ctx context.Context, _ int64) (int64, error) {
		keys = append(keys, domain.ExecutionKey(ctx))
		return 55, nil
	})
	w := Worker{Queue: q, Processor: processor}
	_ = w.Run(context.Background())
	if len(keys) != 2 || keys[0] != "batch-item-7" || keys[1] != keys[0] {
		t.Fatalf("chaves de execução instáveis: %v", keys)
	}
	if q.completed != 1 {
		t.Fatalf("item retomado não foi concluído: %d", q.completed)
	}
}

func TestProcessCancellationDoesNotPermanentlyFailClaimedItem(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	q := &queueStub{items: []*domain.Item{{ID: 8, BatchID: 21, BitrixDealID: 108, Attempts: 1}}}
	processor := processorFunc(func(context.Context, int64) (int64, error) {
		cancel()
		return 0, context.Canceled
	})
	w := Worker{Queue: q, Processor: processor}
	if err := w.Run(ctx); err != nil {
		t.Fatal(err)
	}
	if q.failed != 0 || q.rescheduled != 0 {
		t.Fatalf("shutdown alterou item: failed=%d rescheduled=%d", q.failed, q.rescheduled)
	}
}
