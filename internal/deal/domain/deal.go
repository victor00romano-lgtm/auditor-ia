package domain

import (
	"context"
	"time"
)

// Deal contains the Bitrix fields persisted by the synchronization boundary.
// Pointer fields distinguish absent data from explicit zero values.
type Deal struct {
	BitrixDealID    int64
	Title           *string
	StageID         *string
	StageSemanticID *string
	Closed          *bool
	Amount          *string
	Currency        *string
	AssignedByID    *int64
	CreatedAtBitrix *time.Time
	UpdatedAtBitrix *time.Time
	SyncedAt        *time.Time
}

type DealRepository interface {
	Upsert(context.Context, Deal) (int64, error)
}

type DealSource interface {
	Get(context.Context, string) (Deal, error)
}
