package application

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sort"
	"time"

	statsdomain "github.com/portfolio/auditor-ia/internal/crmstats/domain"
	dealdomain "github.com/portfolio/auditor-ia/internal/deal/domain"
	"github.com/portfolio/auditor-ia/internal/observability"
	"go.opentelemetry.io/otel/attribute"
)

type Sync struct {
	Source     statsdomain.Source
	Repository statsdomain.Repository
	Metrics    *observability.Metrics
	Logger     *slog.Logger
	Now        func() time.Time
	Timeout    time.Duration
}

func (uc Sync) Sync(ctx context.Context, progress func(statsdomain.Progress)) (returnErr error) {
	if uc.Source == nil || uc.Repository == nil {
		return fmt.Errorf("sincronização de estatísticas CRM não configurada")
	}
	if uc.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, uc.Timeout)
		defer cancel()
	}
	started := time.Now()
	ctx, span := observability.Tracer().Start(ctx, "auditor.crm_statistics.sync")
	defer func() {
		span.End()
		if uc.Metrics != nil {
			status := "completed"
			if returnErr != nil {
				status = "failed"
			}
			uc.Metrics.CRMSyncRuns.WithLabelValues(status).Inc()
			uc.Metrics.CRMSyncDuration.Observe(time.Since(started).Seconds())
		}
	}()
	runID, err := uc.Repository.Start(ctx)
	if err != nil {
		return fmt.Errorf("iniciar snapshot CRM: %w", err)
	}
	fail := func(status statsdomain.Status, cause error) error {
		failureCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		_ = uc.Repository.FinishFailed(failureCtx, runID, status, observability.SafeError(cause))
		return cause
	}
	currentProgress := statsdomain.Progress{}
	emit := func(update statsdomain.Progress) {
		if update.Phase != "" {
			currentProgress.Phase = update.Phase
		}
		if update.DealsCollected > currentProgress.DealsCollected {
			currentProgress.DealsCollected = update.DealsCollected
		}
		if update.ActivitiesCollected > currentProgress.ActivitiesCollected {
			currentProgress.ActivitiesCollected = update.ActivitiesCollected
		}
		if update.Markers3264Discovered > currentProgress.Markers3264Discovered {
			currentProgress.Markers3264Discovered = update.Markers3264Discovered
		}
		if progress != nil {
			progress(currentProgress)
		}
	}

	dealsCtx, dealsSpan := observability.Tracer().Start(ctx, "bitrix.deals.sync")
	deals, err := uc.Source.CollectDeals(dealsCtx, emit)
	dealsSpan.SetAttributes(attribute.Int("deals.collected", len(deals)))
	dealsSpan.End()
	if err != nil {
		if errors.Is(err, context.Canceled) || errors.Is(err, context.DeadlineExceeded) {
			return fail(statsdomain.Canceled, err)
		}
		return fail(statsdomain.Failed, err)
	}
	deals = deduplicateDeals(deals)
	if uc.Metrics != nil {
		uc.Metrics.CRMSyncDeals.Add(float64(len(deals)))
	}

	assigned := uniqueAssigneeIDs(deals)
	known, err := uc.Repository.KnownAssigneeIDs(ctx, assigned)
	if err != nil {
		return fail(statsdomain.Failed, fmt.Errorf("consultar responsáveis sincronizados: %w", err))
	}
	users := make([]dealdomain.Assignee, 0)
	for _, id := range assigned {
		if known[id] {
			continue
		}
		user, userErr := uc.Source.GetAssignee(ctx, id)
		if userErr != nil {
			return fail(statsdomain.Failed, fmt.Errorf("sincronizar responsável %d: %w", id, userErr))
		}
		users = append(users, user)
	}

	activitiesCtx, activitiesSpan := observability.Tracer().Start(ctx, "bitrix.activities.sync")
	markers, activities, markerErr := uc.Source.CollectMarker3264(activitiesCtx, emit)
	activitiesSpan.SetAttributes(attribute.Int("activities.collected", activities), attribute.Int("markers.3264", len(markers)))
	activitiesSpan.End()
	markerAvailable := markerErr == nil
	if markerErr != nil && !errors.Is(markerErr, statsdomain.ErrMarkerUnavailable) {
		return fail(statsdomain.Failed, markerErr)
	}
	markers = deduplicateIDs(markers)
	if uc.Metrics != nil {
		uc.Metrics.CRMSyncActivities.Add(float64(activities))
		uc.Metrics.CRMSyncMarkers.WithLabelValues("3264").Add(float64(len(markers)))
	}
	snapshot := statsdomain.Snapshot{Deals: deals, Users: users, MarkerDealIDs: markers, ActivitiesCollected: activities, MarkerAvailable: markerAvailable}
	if err = uc.Repository.Publish(ctx, runID, snapshot); err != nil {
		return fail(statsdomain.Failed, fmt.Errorf("publicar snapshot CRM: %w", err))
	}
	if uc.Metrics != nil && markerAvailable {
		now := time.Now
		if uc.Now != nil {
			now = uc.Now
		}
		uc.Metrics.CRMSyncLastSuccess.Set(float64(now().Unix()))
	}
	if uc.Logger != nil {
		uc.Logger.InfoContext(ctx, "crm_statistics_sync_completed", "deals", len(deals), "activities", activities, "markers_3264", len(markers), "marker_available", markerAvailable)
	}
	return nil
}

func deduplicateIDs(values []int64) []int64 {
	set := make(map[int64]struct{}, len(values))
	for _, value := range values {
		if value > 0 {
			set[value] = struct{}{}
		}
	}
	result := make([]int64, 0, len(set))
	for value := range set {
		result = append(result, value)
	}
	sort.Slice(result, func(i, j int) bool { return result[i] < result[j] })
	return result
}

func deduplicateDeals(values []dealdomain.Deal) []dealdomain.Deal {
	unique := make(map[int64]dealdomain.Deal, len(values))
	for _, deal := range values {
		if deal.BitrixDealID > 0 {
			unique[deal.BitrixDealID] = deal
		}
	}
	ids := make([]int64, 0, len(unique))
	for id := range unique {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	result := make([]dealdomain.Deal, 0, len(ids))
	for _, id := range ids {
		result = append(result, unique[id])
	}
	return result
}

func uniqueAssigneeIDs(deals []dealdomain.Deal) []int64 {
	set := make(map[int64]struct{})
	for _, deal := range deals {
		if deal.AssignedByID != nil && *deal.AssignedByID > 0 {
			set[*deal.AssignedByID] = struct{}{}
		}
	}
	ids := make([]int64, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}
