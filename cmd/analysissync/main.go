package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	auditrules "github.com/portfolio/auditor-ia/internal/audit/infrastructure/rules"
	conversationapp "github.com/portfolio/auditor-ia/internal/conversation/application"
	conversationbitrix "github.com/portfolio/auditor-ia/internal/conversation/infrastructure/bitrix"
	conversationollama "github.com/portfolio/auditor-ia/internal/conversation/infrastructure/ollama"
	dealbitrix "github.com/portfolio/auditor-ia/internal/deal/infrastructure/bitrix"
	"github.com/portfolio/auditor-ia/internal/observability"
	platformconfig "github.com/portfolio/auditor-ia/internal/platform/config"
	postgresdb "github.com/portfolio/auditor-ia/internal/platform/postgres"
	"github.com/portfolio/auditor-ia/internal/platform/security"
)

const defaultTimeout = 5 * time.Minute

func main() {
	if len(os.Args) != 2 || strings.TrimSpace(os.Args[1]) == "" {
		fmt.Fprintln(os.Stderr, "uso: go run ./cmd/analysissync <deal_id>")
		os.Exit(2)
	}
	cfg, configErr := platformconfig.Load(platformconfig.AnalysisSync)
	if configErr != nil {
		fmt.Fprintln(os.Stderr, "erro:", security.Error(configErr))
		os.Exit(1)
	}
	webhook := cfg.BitrixWebhookURL
	obsConfig := observability.Config{ServiceName: "auditor-analysissync", ServiceVersion: cfg.ServiceVersion, Environment: cfg.AppEnv, LogLevel: cfg.LogLevel, LogFormat: cfg.LogFormat, OTLPEndpoint: strings.TrimPrefix(strings.TrimPrefix(cfg.OTLPEndpoint, "http://"), "https://"), TracingEnabled: cfg.OTELTracingEnabled, SampleRatio: cfg.OTELSampleRatio}
	logger, loggerErr := observability.NewLogger(obsConfig, os.Stderr)
	if loggerErr != nil {
		fmt.Fprintln(os.Stderr, "erro:", loggerErr)
		os.Exit(1)
	}
	slog.SetDefault(logger)
	timeout, err := configuredTimeout()
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
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
	metrics := observability.NewMetrics()
	httpClient := &http.Client{Timeout: timeout}

	pool, err := postgresdb.Open(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro: abrir PostgreSQL:", err)
		os.Exit(1)
	}
	defer pool.Close()
	ruleEngine, err := auditrules.Load(getenv("RULES_FILE", "configs/rules.yaml"))
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro: carregar regras de auditoria:", err)
		os.Exit(1)
	}
	timeline := conversationbitrix.Timeline{Webhook: webhook, Client: httpClient}
	analyze := conversationapp.Analyze{
		Source:        timeline,
		FormSource:    timeline,
		StatusSource:  timeline,
		AssigneeRoles: conversationapp.NewConfiguredAssigneeRoles(cfg.SupportAssigneeIDs),
		Analyzer: conversationollama.Analyzer{
			URL:         cfg.OllamaURL,
			Model:       cfg.OllamaModel,
			PrivacyMode: cfg.PrivacyMode,
			Client:      httpClient,
		},
	}
	result, err := (conversationapp.SyncAnalysis{
		DealSource:           dealbitrix.Source{Webhook: webhook, Client: httpClient},
		DealRepository:       postgresdb.NewDealRepository(pool),
		MessageRepository:    postgresdb.NewMessageRepository(pool),
		Analyze:              analyze,
		AssessmentRepository: postgresdb.NewAssessmentRepository(pool),
		Rules:                ruleEngine,
		Logger:               logger,
		Metrics:              metrics,
	}).Execute(ctx, os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}

	fmt.Printf("ID do negócio: %d\n", result.DealID)
	fmt.Printf("assessment_id: %d\n", result.AssessmentID)
	fmt.Printf("Quantidade de achados: %d\n", result.FindingCount)
	fmt.Printf("Score: %d\n", result.Score)
	fmt.Printf("Resultado provável: %s\n", result.ProbableResult)
	fmt.Printf("Resultado final: %s\n", result.FinalResult)
	fmt.Printf("Fonte: %s\n", result.ResultSource)
	fmt.Printf("Status: %s\n", result.Status)
}

func configuredTimeout() (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv("ANALYSISSYNC_TIMEOUT"))
	if value == "" {
		return defaultTimeout, nil
	}
	timeout, err := time.ParseDuration(value)
	if err != nil || timeout <= 0 {
		return 0, fmt.Errorf("ANALYSISSYNC_TIMEOUT inválido: use uma duração positiva, como 5m")
	}
	return timeout, nil
}

func getenv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
