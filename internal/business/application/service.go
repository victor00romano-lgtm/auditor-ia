package application

import (
	"context"
	"time"

	"github.com/portfolio/auditor-ia/internal/business/domain"
	"github.com/portfolio/auditor-ia/internal/observability"
	"go.opentelemetry.io/otel/attribute"
)

type Service struct {
	Repository domain.Repository
	Metrics    *observability.Metrics
}

func (s Service) Load(ctx context.Context, filters domain.GlobalFilters) (result domain.Dashboard, err error) {
	started := time.Now()
	ctx, span := observability.Tracer().Start(ctx, "dashboard.executive.load")
	span.SetAttributes(attribute.String("dashboard.screen", "executive"))
	defer span.End()
	defer func() {
		if s.Metrics != nil {
			status := "success"
			if err != nil {
				status = "error"
				s.Metrics.DashboardQueryErrors.WithLabelValues("executive").Inc()
			}
			s.Metrics.DashboardLoads.WithLabelValues("executive", status).Inc()
			s.Metrics.DashboardLoadDuration.WithLabelValues("executive").Observe(time.Since(started).Seconds())
		}
	}()
	return s.Repository.Load(ctx, filters)
}
