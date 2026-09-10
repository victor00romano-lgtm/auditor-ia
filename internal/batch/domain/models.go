package domain

import (
	"context"
	"errors"
	"time"
)

type Status string

const (
	Pending    Status = "PENDING"
	Running    Status = "RUNNING"
	Completed  Status = "COMPLETED"
	Partial    Status = "PARTIAL"
	Failed     Status = "FAILED"
	Canceled   Status = "CANCELED"
	Processing Status = "PROCESSING"
)

var ErrNotFound = errors.New("lote não encontrado")

type executionKeyContext struct{}

func WithExecutionKey(ctx context.Context, key string) context.Context {
	return context.WithValue(ctx, executionKeyContext{}, key)
}
func ExecutionKey(ctx context.Context) string {
	value, _ := ctx.Value(executionKeyContext{}).(string)
	return value
}

type Batch struct {
	ID         int64      `json:"id"`
	Status     Status     `json:"status"`
	Total      int        `json:"total"`
	Pending    int        `json:"pending"`
	Processing int        `json:"processing"`
	Completed  int        `json:"completed"`
	Failed     int        `json:"failed"`
	CreatedAt  time.Time  `json:"created_at"`
	StartedAt  *time.Time `json:"started_at,omitempty"`
	FinishedAt *time.Time `json:"finished_at,omitempty"`
}

type Item struct {
	ID, BatchID, BitrixDealID, AssessmentID int64
	Status                                  Status
	Attempts                                int
	LeaseExpiresAt                          time.Time
}

type Queue interface {
	Create(context.Context, []int64) (Batch, error)
	Get(context.Context, int64) (Batch, error)
	Cancel(context.Context, int64) error
	Claim(context.Context, string, time.Duration) (*Item, error)
	Complete(context.Context, Item, int64) error
	Reschedule(context.Context, Item, time.Time, string, string) error
	Fail(context.Context, Item, string, string) error
	RecoverExpired(context.Context) (int64, error)
}

type Processor interface {
	Run(context.Context, int64) (int64, error)
}
