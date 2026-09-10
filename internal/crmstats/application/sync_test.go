package application

import (
	"context"
	"errors"
	"testing"
	"time"

	statsdomain "github.com/portfolio/auditor-ia/internal/crmstats/domain"
	dealdomain "github.com/portfolio/auditor-ia/internal/deal/domain"
)

type sourceStub struct {
	deals      []dealdomain.Deal
	markers    []int64
	activities int
	markerErr  error
	dealErr    error
	users      map[int64]dealdomain.Assignee
	userCalls  []int64
}

func (s *sourceStub) CollectDeals(context.Context, func(statsdomain.Progress)) ([]dealdomain.Deal, error) {
	return s.deals, s.dealErr
}
func (s *sourceStub) CollectMarker3264(context.Context, func(statsdomain.Progress)) ([]int64, int, error) {
	return s.markers, s.activities, s.markerErr
}
func (s *sourceStub) GetAssignee(_ context.Context, id int64) (dealdomain.Assignee, error) {
	s.userCalls = append(s.userCalls, id)
	return s.users[id], nil
}

type repositoryStub struct {
	known      map[int64]bool
	published  []statsdomain.Snapshot
	failed     []statsdomain.Status
	safeErrors []string
}

func (r *repositoryStub) Start(context.Context) (int64, error) { return 9, nil }
func (r *repositoryStub) KnownAssigneeIDs(context.Context, []int64) (map[int64]bool, error) {
	return r.known, nil
}
func (r *repositoryStub) Publish(_ context.Context, _ int64, snapshot statsdomain.Snapshot) error {
	r.published = append(r.published, snapshot)
	return nil
}
func (r *repositoryStub) FinishFailed(_ context.Context, _ int64, status statsdomain.Status, safe string) error {
	r.failed = append(r.failed, status)
	r.safeErrors = append(r.safeErrors, safe)
	return nil
}
func (r *repositoryStub) Statistics(context.Context, time.Time, *time.Location) (statsdomain.Statistics, error) {
	return statsdomain.Statistics{}, nil
}

func TestSyncDeduplicatesDealsSyncsUnknownNamesAndDoesNotCreateAssessments(t *testing.T) {
	seven, eight := int64(7), int64(8)
	source := &sourceStub{
		deals:   []dealdomain.Deal{{BitrixDealID: 1, AssignedByID: &seven}, {BitrixDealID: 1, AssignedByID: &seven}, {BitrixDealID: 2, AssignedByID: &eight}, {BitrixDealID: 3}},
		markers: []int64{1, 1}, activities: 4,
		users: map[int64]dealdomain.Assignee{8: {BitrixUserID: 8, DisplayName: "Responsável Oito"}},
	}
	repository := &repositoryStub{known: map[int64]bool{7: true}}
	if err := (Sync{Source: source, Repository: repository}).Sync(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if len(repository.published) != 1 || len(repository.published[0].Deals) != 3 || len(repository.published[0].MarkerDealIDs) != 1 || len(repository.published[0].Users) != 1 || len(source.userCalls) != 1 || source.userCalls[0] != 8 {
		t.Fatalf("snapshot=%#v calls=%v", repository.published, source.userCalls)
	}
	// O contrato do sincronizador não possui repositório de avaliações nem analisador Ollama.
}

func TestAccessDeniedPublishesMetadataWithMarkerUnavailableNotZero(t *testing.T) {
	source := &sourceStub{deals: []dealdomain.Deal{{BitrixDealID: 1}}, markerErr: statsdomain.ErrMarkerUnavailable}
	repository := &repositoryStub{known: map[int64]bool{}}
	if err := (Sync{Source: source, Repository: repository}).Sync(context.Background(), nil); err != nil {
		t.Fatal(err)
	}
	if len(repository.published) != 1 || repository.published[0].MarkerAvailable {
		t.Fatalf("snapshot=%#v", repository.published)
	}
}

func TestPartialOperationalFailureDoesNotPublishSnapshot(t *testing.T) {
	source := &sourceStub{deals: []dealdomain.Deal{{BitrixDealID: 1}}, markerErr: errors.New("HTTP 503")}
	repository := &repositoryStub{known: map[int64]bool{}}
	if err := (Sync{Source: source, Repository: repository}).Sync(context.Background(), nil); err == nil {
		t.Fatal("falha operacional ignorada")
	}
	if len(repository.published) != 0 || len(repository.failed) != 1 || repository.failed[0] != statsdomain.Failed {
		t.Fatalf("published=%d failed=%v", len(repository.published), repository.failed)
	}
}

func TestCancellationDoesNotPublishSnapshot(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	source := &sourceStub{dealErr: context.Canceled}
	repository := &repositoryStub{}
	if err := (Sync{Source: source, Repository: repository}).Sync(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	if len(repository.published) != 0 || len(repository.failed) != 1 || repository.failed[0] != statsdomain.Canceled {
		t.Fatalf("published=%d failed=%v", len(repository.published), repository.failed)
	}
}
