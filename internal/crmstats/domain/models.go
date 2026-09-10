package domain

import (
	"context"
	"errors"
	"time"

	dealdomain "github.com/portfolio/auditor-ia/internal/deal/domain"
)

var (
	ErrMarkerUnavailable = errors.New("marcador indisponível")
	ErrSyncRunning       = errors.New("sincronização CRM já está em execução")
)

type Status string

const (
	Running   Status = "RUNNING"
	Completed Status = "COMPLETED"
	Partial   Status = "PARTIAL"
	Failed    Status = "FAILED"
	Canceled  Status = "CANCELED"
)

type Progress struct {
	Phase                 string
	DealsCollected        int
	ActivitiesCollected   int
	Markers3264Discovered int
}

type Snapshot struct {
	Deals               []dealdomain.Deal
	Users               []dealdomain.Assignee
	MarkerDealIDs       []int64
	ActivitiesCollected int
	MarkerAvailable     bool
}

type AssigneeCount struct {
	ID    int64  `json:"id,omitempty"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

type MonthlyCount struct {
	Month string `json:"month"`
	Count int    `json:"count"`
}

type MonthlyOutcome struct {
	Month string `json:"month"`
	Won   int    `json:"won"`
	Lost  int    `json:"lost"`
}

type MarkerAssignment struct {
	ID              int64  `json:"id,omitempty"`
	Name            string `json:"name"`
	Count           int    `json:"count"`
	PercentageBasis int64  `json:"percentage_basis_points"`
}

type Statistics struct {
	Available            bool               `json:"available"`
	UnavailableReason    string             `json:"unavailable_reason,omitempty"`
	TotalDeals           int                `json:"total_deals"`
	WonDeals             int                `json:"won_deals"`
	LostDeals            int                `json:"lost_deals"`
	ByAssignee           []AssigneeCount    `json:"by_assignee"`
	ByMonth              []MonthlyCount     `json:"by_month"`
	OutcomesByMonth      []MonthlyOutcome   `json:"outcomes_by_month"`
	Marker3264Available  bool               `json:"marker_3264_available"`
	MarkerUnavailable    string             `json:"marker_unavailable_reason,omitempty"`
	Marker3264Total      int                `json:"marker_3264_total"`
	Marker3264ByAssignee []MarkerAssignment `json:"marker_3264_by_assignee"`
	LastSyncAt           time.Time          `json:"last_sync_at,omitempty"`
	LastAttemptAt        time.Time          `json:"last_attempt_at,omitempty"`
	LastSuccessAt        time.Time          `json:"last_success_at,omitempty"`
	LastStatus           Status             `json:"last_status,omitempty"`
	Stale                bool               `json:"stale"`
	CoverageNote         string             `json:"coverage_note,omitempty"`
}

type Source interface {
	CollectDeals(context.Context, func(Progress)) ([]dealdomain.Deal, error)
	CollectMarker3264(context.Context, func(Progress)) ([]int64, int, error)
	GetAssignee(context.Context, int64) (dealdomain.Assignee, error)
}

type Repository interface {
	Start(context.Context) (int64, error)
	KnownAssigneeIDs(context.Context, []int64) (map[int64]bool, error)
	Publish(context.Context, int64, Snapshot) error
	FinishFailed(context.Context, int64, Status, string) error
	Statistics(context.Context, time.Time, *time.Location) (Statistics, error)
}

type Synchronizer interface {
	Sync(context.Context, func(Progress)) error
}
