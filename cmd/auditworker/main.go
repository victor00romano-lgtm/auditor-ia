package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	apiapp "github.com/portfolio/auditor-ia/internal/api/application"
	auditrules "github.com/portfolio/auditor-ia/internal/audit/infrastructure/rules"
	batchapp "github.com/portfolio/auditor-ia/internal/batch/application"
	conversationapp "github.com/portfolio/auditor-ia/internal/conversation/application"
	conversationbitrix "github.com/portfolio/auditor-ia/internal/conversation/infrastructure/bitrix"
	conversationollama "github.com/portfolio/auditor-ia/internal/conversation/infrastructure/ollama"
	dealbitrix "github.com/portfolio/auditor-ia/internal/deal/infrastructure/bitrix"
	"github.com/portfolio/auditor-ia/internal/observability"
	platformconfig "github.com/portfolio/auditor-ia/internal/platform/config"
	"github.com/portfolio/auditor-ia/internal/platform/httpclient"
	postgresdb "github.com/portfolio/auditor-ia/internal/platform/postgres"
	"github.com/portfolio/auditor-ia/internal/platform/security"
)

func main() {
	if err := run(); err != nil {
		slog.Error("audit_worker_stopped", "error", security.Error(err))
		os.Exit(1)
	}
}

func run() error {
	cfg, err := platformconfig.Load(platformconfig.AuditWorker)
	if err != nil {
		return err
	}
	logger, err := observability.NewLogger(observability.Config{ServiceName: "audit-worker", ServiceVersion: cfg.ServiceVersion, Environment: cfg.AppEnv, LogLevel: cfg.LogLevel, LogFormat: cfg.LogFormat}, os.Stdout)
	if err != nil {
		return err
	}
	slog.SetDefault(logger)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	pool, err := postgresdb.Open(ctx)
	if err != nil {
		return fmt.Errorf("abrir PostgreSQL: %w", err)
	}
	defer pool.Close()
	engine, err := auditrules.Load(env("RULES_FILE", "configs/rules.yaml"))
	if err != nil {
		return fmt.Errorf("carregar regras: %w", err)
	}
	bitrixClient := &http.Client{Timeout: cfg.BitrixHTTPTimeout, Transport: httpclient.NewLimitTransport(http.DefaultTransport, cfg.BitrixMaxConcurrency)}
	ollamaClient := &http.Client{Timeout: cfg.OllamaHTTPTimeout, Transport: httpclient.NewLimitTransport(http.DefaultTransport, cfg.OllamaMaxConcurrency)}
	timeline := conversationbitrix.Timeline{Webhook: cfg.BitrixWebhookURL, Client: bitrixClient}
	analyze := conversationapp.Analyze{Source: timeline, FormSource: timeline, StatusSource: timeline, AssigneeRoles: conversationapp.NewConfiguredAssigneeRoles(cfg.SupportAssigneeIDs), Analyzer: conversationollama.Analyzer{URL: cfg.OllamaURL, Model: cfg.OllamaModel, PrivacyMode: cfg.PrivacyMode, Client: ollamaClient}}
	runner := apiapp.SyncRunner{Sync: conversationapp.SyncAnalysis{DealSource: dealbitrix.Source{Webhook: cfg.BitrixWebhookURL, Client: bitrixClient}, DealRepository: postgresdb.NewDealRepository(pool), MessageRepository: postgresdb.NewMessageRepository(pool), Analyze: analyze, AssessmentRepository: postgresdb.NewAssessmentRepository(pool), Rules: engine, Logger: logger, Metrics: observability.NewMetrics()}}
	processor := batchapp.AuditProcessor{Runner: runner, Timeout: cfg.AuditTimeout}
	queue := postgresdb.NewBatchQueue(pool)
	host, _ := os.Hostname()
	if host == "" {
		host = "worker"
	}
	var group sync.WaitGroup
	errCh := make(chan error, cfg.BatchWorkers)
	for i := 0; i < cfg.BatchWorkers; i++ {
		group.Add(1)
		go func(index int) {
			defer group.Done()
			id := strings.Join([]string{host, strconv.Itoa(os.Getpid()), strconv.Itoa(index)}, "-")
			worker := batchapp.Worker{Queue: queue, Processor: processor, ID: id, Lease: cfg.BatchLease, Poll: cfg.BatchPoll, MaxAttempts: cfg.BatchMaxAttempts, Now: time.Now}
			if err := worker.Run(ctx); err != nil {
				errCh <- err
				stop()
			}
		}(i)
	}
	done := make(chan struct{})
	go func() { group.Wait(); close(done) }()
	select {
	case err := <-errCh:
		return err
	case <-done:
		return nil
	case <-ctx.Done():
		<-done
		return nil
	}
}

func env(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
