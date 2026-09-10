package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	apiapp "github.com/portfolio/auditor-ia/internal/api/application"
	"github.com/portfolio/auditor-ia/internal/api/report"
	"github.com/portfolio/auditor-ia/internal/audit/interfaces/tui"
	"github.com/portfolio/auditor-ia/internal/bootstrap"
	businessapp "github.com/portfolio/auditor-ia/internal/business/application"
	businessreport "github.com/portfolio/auditor-ia/internal/business/report"
	statsapp "github.com/portfolio/auditor-ia/internal/crmstats/application"
	statsbitrix "github.com/portfolio/auditor-ia/internal/crmstats/infrastructure/bitrix"
	"github.com/portfolio/auditor-ia/internal/observability"
	platformconfig "github.com/portfolio/auditor-ia/internal/platform/config"
	postgresdb "github.com/portfolio/auditor-ia/internal/platform/postgres"
	"github.com/portfolio/auditor-ia/internal/platform/security"
)

func main() {
	cfg, err := platformconfig.Load(platformconfig.Auditor)
	if err != nil {
		fmt.Fprintln(os.Stderr, "configuração:", security.Error(err))
		os.Exit(1)
	}
	obsConfig := observability.Config{ServiceName: "auditor-tui", ServiceVersion: cfg.ServiceVersion, Environment: cfg.AppEnv, LogLevel: cfg.LogLevel, LogFormat: cfg.LogFormat, OTLPEndpoint: strings.TrimPrefix(strings.TrimPrefix(cfg.OTLPEndpoint, "http://"), "https://"), TracingEnabled: cfg.OTELTracingEnabled, SampleRatio: cfg.OTELSampleRatio}
	logger, loggerErr := observability.NewLogger(obsConfig, os.Stderr)
	if loggerErr != nil {
		fmt.Fprintln(os.Stderr, "configuração:", loggerErr)
		os.Exit(1)
	}
	slog.SetDefault(logger)
	app, err := bootstrap.New()
	if err != nil {
		fmt.Fprintln(os.Stderr, "configuração:", security.Error(err))
		os.Exit(1)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	shutdownTracing, tracingErr := observability.SetupTracing(ctx, obsConfig)
	if tracingErr != nil {
		logger.Warn("tracing_disabled", "error", observability.SafeError(tracingErr))
		shutdownTracing = func(context.Context) error { return nil }
	}
	defer func() {
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer shutdownCancel()
		_ = shutdownTracing(shutdownCtx)
	}()
	ctx, tuiSpan := observability.Tracer().Start(ctx, "auditor.tui")
	defer tuiSpan.End()
	pool, dbErr := postgresdb.Open(ctx)
	if dbErr != nil {
		fmt.Fprintln(os.Stderr, "PostgreSQL:", security.Error(dbErr))
		os.Exit(1)
	}
	defer pool.Close()
	apiRepository := postgresdb.NewAPIRepository(pool)
	exporter := apiapp.ReportExporter{Source: apiRepository, Renderer: report.PDFRenderer{}, BaseDir: reportsDir()}
	metrics := observability.NewMetrics()
	service := businessapp.Service{Repository: postgresdb.NewBusinessDashboardRepository(pool), Metrics: metrics}
	csvExporter := businessapp.CSVExporter{BaseDir: reportsDir()}
	executivePDFExporter := businessapp.ExecutivePDFExporter{Loader: service, Renderer: businessreport.ExecutivePDFRenderer{}, BaseDir: reportsDir()}
	model := tui.NewBusinessDashboardWithExports(app.Run, postgresdb.NewDashboardRepository(pool), exporter, service, csvExporter, executivePDFExporter)
	model.SetBatchManager(postgresdb.NewBatchQueue(pool))
	model.SetCRMStatisticsSynchronizer(statsapp.Sync{Source: statsbitrix.Source{Webhook: cfg.BitrixWebhookURL, Client: &http.Client{Timeout: cfg.BitrixHTTPTimeout}, PageDelay: cfg.CRMStatsPageDelay}, Repository: postgresdb.NewCRMStatisticsRepository(pool), Metrics: metrics, Logger: logger, Timeout: cfg.CRMStatsSyncTimeout})
	if _, err = tea.NewProgram(model, tea.WithAltScreen()).Run(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func reportsDir() string {
	if value := strings.TrimSpace(os.Getenv("REPORTS_DIR")); value != "" {
		return value
	}
	return "./reports"
}
