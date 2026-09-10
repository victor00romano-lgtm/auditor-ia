package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"

	statsapp "github.com/portfolio/auditor-ia/internal/crmstats/application"
	statsdomain "github.com/portfolio/auditor-ia/internal/crmstats/domain"
	statsbitrix "github.com/portfolio/auditor-ia/internal/crmstats/infrastructure/bitrix"
	"github.com/portfolio/auditor-ia/internal/observability"
	platformconfig "github.com/portfolio/auditor-ia/internal/platform/config"
	postgresdb "github.com/portfolio/auditor-ia/internal/platform/postgres"
	"github.com/portfolio/auditor-ia/internal/platform/security"
)

func main() {
	cfg, err := platformconfig.Load(platformconfig.CRMStatsSync)
	if err != nil {
		fmt.Fprintln(os.Stderr, "configuração:", security.Error(err))
		os.Exit(1)
	}
	logger, err := observability.NewLogger(observability.Config{ServiceName: "crm-stats-sync", ServiceVersion: cfg.ServiceVersion, Environment: cfg.AppEnv, LogLevel: cfg.LogLevel, LogFormat: cfg.LogFormat}, os.Stderr)
	if err != nil {
		fmt.Fprintln(os.Stderr, "logger:", security.Error(err))
		os.Exit(1)
	}
	slog.SetDefault(logger)
	root, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(root, cfg.CRMStatsSyncTimeout)
	defer cancel()
	pool, err := postgresdb.Open(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "PostgreSQL:", security.Error(err))
		os.Exit(1)
	}
	defer pool.Close()
	metrics := observability.NewMetrics()
	observability.SetMetrics(metrics)
	syncer := statsapp.Sync{
		Source:     statsbitrix.Source{Webhook: cfg.BitrixWebhookURL, Client: &http.Client{Timeout: cfg.BitrixHTTPTimeout}, PageDelay: cfg.CRMStatsPageDelay},
		Repository: postgresdb.NewCRMStatisticsRepository(pool), Metrics: metrics, Logger: logger, Timeout: cfg.CRMStatsSyncTimeout,
	}
	progress := func(value statsdomain.Progress) {
		fmt.Printf("negócios coletados: %d | atividades coletadas: %d | marcadores 3264: %d\r", value.DealsCollected, value.ActivitiesCollected, value.Markers3264Discovered)
	}
	if err := syncer.Sync(ctx, progress); err != nil {
		fmt.Fprintln(os.Stderr, "\nsincronização:", security.Error(err))
		os.Exit(1)
	}
	fmt.Println("\nSincronização global concluída.")
}
