package main

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"strings"
	"syscall"
	"time"

	apiapp "github.com/portfolio/auditor-ia/internal/api/application"
	"github.com/portfolio/auditor-ia/internal/api/httpapi"
	"github.com/portfolio/auditor-ia/internal/api/report"
	auditrules "github.com/portfolio/auditor-ia/internal/audit/infrastructure/rules"
	businessapp "github.com/portfolio/auditor-ia/internal/business/application"
	conversationapp "github.com/portfolio/auditor-ia/internal/conversation/application"
	conversationbitrix "github.com/portfolio/auditor-ia/internal/conversation/infrastructure/bitrix"
	conversationollama "github.com/portfolio/auditor-ia/internal/conversation/infrastructure/ollama"
	dealbitrix "github.com/portfolio/auditor-ia/internal/deal/infrastructure/bitrix"
	"github.com/portfolio/auditor-ia/internal/observability"
	platformbitrix "github.com/portfolio/auditor-ia/internal/platform/bitrix"
	platformconfig "github.com/portfolio/auditor-ia/internal/platform/config"
	postgresdb "github.com/portfolio/auditor-ia/internal/platform/postgres"
	"github.com/portfolio/auditor-ia/internal/platform/security"
	"go.opentelemetry.io/contrib/instrumentation/net/http/otelhttp"
)

func main() {
	if len(os.Args) == 2 && os.Args[1] == "--healthcheck" {
		response, err := (&http.Client{Timeout: 3 * time.Second}).Get("http://127.0.0.1:8080/health")
		if err != nil || response.StatusCode != http.StatusOK {
			os.Exit(1)
		}
		_ = response.Body.Close()
		return
	}
	if err := run(); err != nil {
		log.Printf("API encerrada: %s", security.Error(err))
		os.Exit(1)
	}
}

func run() error {
	cfg, err := platformconfig.Load(platformconfig.API)
	if err != nil {
		return err
	}
	apiKey, webhook, auditTimeout := cfg.APIKey, cfg.BitrixWebhookURL, cfg.AuditTimeout
	obsConfig := observability.Config{ServiceName: cfg.ServiceName, ServiceVersion: cfg.ServiceVersion, Environment: cfg.AppEnv, LogLevel: cfg.LogLevel, LogFormat: cfg.LogFormat, OTLPEndpoint: strings.TrimPrefix(strings.TrimPrefix(cfg.OTLPEndpoint, "http://"), "https://"), TracingEnabled: cfg.OTELTracingEnabled, SampleRatio: cfg.OTELSampleRatio}
	logger, err := observability.NewLogger(obsConfig, os.Stdout)
	if err != nil {
		return err
	}
	slog.SetDefault(logger)
	rootCtx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	shutdownTracing, traceErr := observability.SetupTracing(rootCtx, obsConfig)
	if traceErr != nil {
		logger.Warn("tracing_disabled", "error", observability.SafeError(traceErr))
		shutdownTracing = func(context.Context) error { return nil }
	}
	defer func() {
		shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()
		_ = shutdownTracing(shutdownCtx)
	}()
	metrics := observability.NewMetrics()
	observability.SetMetrics(metrics)
	defer observability.SetMetrics(nil)
	openCtx, cancel := context.WithTimeout(rootCtx, 10*time.Second)
	defer cancel()
	pool, err := postgresdb.Open(openCtx)
	if err != nil {
		return fmt.Errorf("abrir PostgreSQL: %w", err)
	}
	defer pool.Close()
	engine, err := auditrules.Load(getenv("RULES_FILE", "configs/rules.yaml"))
	if err != nil {
		return fmt.Errorf("carregar regras: %w", err)
	}
	bitrixClient, ollamaClient := dependencyClients(cfg)
	timeline := conversationbitrix.Timeline{Webhook: webhook, Client: bitrixClient}
	analyze := conversationapp.Analyze{Source: timeline, FormSource: timeline, StatusSource: timeline, AssigneeRoles: conversationapp.NewConfiguredAssigneeRoles(cfg.SupportAssigneeIDs), Analyzer: conversationollama.Analyzer{URL: cfg.OllamaURL, Model: cfg.OllamaModel, PrivacyMode: cfg.PrivacyMode, Client: ollamaClient}}
	runner := apiapp.SyncRunner{Sync: conversationapp.SyncAnalysis{DealSource: dealbitrix.Source{Webhook: webhook, Client: bitrixClient}, DealRepository: postgresdb.NewDealRepository(pool), MessageRepository: postgresdb.NewMessageRepository(pool), Analyze: analyze, AssessmentRepository: postgresdb.NewAssessmentRepository(pool), Rules: engine, Logger: logger, Metrics: metrics}}
	readiness := &observability.Readiness{Timeout: cfg.ReadinessTimeout, TTL: cfg.ReadinessCacheTTL, Metrics: metrics, Postgres: func(ctx context.Context) error { var one int; return pool.QueryRow(ctx, "SELECT 1").Scan(&one) }, Bitrix: func(ctx context.Context) error {
		var response any
		return (platformbitrix.Client{Webhook: webhook, HTTP: bitrixClient}).Call(ctx, "profile.json", map[string]any{}, &response)
	}, Ollama: ollamaReadiness(ollamaClient, cfg.OllamaURL, cfg.OllamaModel, metrics)}
	businessService := businessapp.Service{Repository: postgresdb.NewBusinessDashboardRepository(pool), Metrics: metrics}
	handler, err := httpapi.New(httpapi.Config{APIKey: apiKey, AuditTimeout: auditTimeout, Logger: security.RedactingLogger{Next: observability.PrintfLogger{Logger: logger}}, RateLimitRPS: cfg.RateLimitRPS, RateLimitBurst: cfg.RateLimitBurst, MaxBodyBytes: int64(cfg.MaxBodyBytes), AllowedOrigins: cfg.AllowedOrigins, ServiceName: cfg.ServiceName, ServiceVersion: cfg.ServiceVersion, Readiness: readiness, Business: businessService, Batches: postgresdb.NewBatchQueue(pool)}, postgresdb.NewAPIRepository(pool), runner, report.PDFRenderer{})
	if err != nil {
		return err
	}
	addr := getenv("HTTP_ADDR", ":8080")
	server := &http.Server{Addr: addr, Handler: metrics.HTTPMiddleware(otelhttp.NewHandler(handler.Handler(), "http.server")), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: auditTimeout + 30*time.Second, IdleTimeout: 60 * time.Second}
	metricsServer := &http.Server{Addr: cfg.MetricsAddr, Handler: metrics.Handler(), ReadHeaderTimeout: 3 * time.Second, ReadTimeout: 5 * time.Second, WriteTimeout: 10 * time.Second, IdleTimeout: 30 * time.Second}
	errCh := make(chan error, 2)
	go func() { logger.Info("http_server_started", "address", addr); errCh <- server.ListenAndServe() }()
	go func() {
		logger.Info("metrics_server_started", "address", cfg.MetricsAddr)
		errCh <- metricsServer.ListenAndServe()
	}()
	select {
	case <-rootCtx.Done():
		shutdownCtx, shutdownCancel := context.WithTimeout(context.Background(), 15*time.Second)
		defer shutdownCancel()
		if err := server.Shutdown(shutdownCtx); err != nil {
			return fmt.Errorf("encerrar servidor: %w", err)
		}
		_ = metricsServer.Shutdown(shutdownCtx)
		return nil
	case err := <-errCh:
		if err == http.ErrServerClosed {
			return nil
		}
		return err
	}
}

func dependencyClients(cfg platformconfig.Config) (bitrix, ollama *http.Client) {
	return &http.Client{Timeout: cfg.BitrixHTTPTimeout}, &http.Client{Timeout: cfg.OllamaHTTPTimeout}
}

func ollamaReadiness(client *http.Client, baseURL, model string, metrics *observability.Metrics) observability.CheckFunc {
	return func(ctx context.Context) error {
		req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(baseURL, "/")+"/api/tags", nil)
		if err != nil {
			return err
		}
		response, err := client.Do(req)
		if err != nil {
			metrics.ModelAvailable.WithLabelValues().Set(0)
			return err
		}
		defer response.Body.Close()
		if response.StatusCode >= 300 {
			metrics.ModelAvailable.WithLabelValues().Set(0)
			return fmt.Errorf("Ollama HTTP %d", response.StatusCode)
		}
		var payload struct {
			Models []struct {
				Name  string `json:"name"`
				Model string `json:"model"`
			} `json:"models"`
		}
		if err := json.NewDecoder(response.Body).Decode(&payload); err != nil {
			return err
		}
		for _, item := range payload.Models {
			if item.Name == model || item.Model == model {
				metrics.ModelAvailable.WithLabelValues().Set(1)
				return nil
			}
		}
		metrics.ModelAvailable.WithLabelValues().Set(0)
		return fmt.Errorf("modelo configurado indisponível")
	}
}

func envDuration(key string, fallback time.Duration) (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return fallback, nil
	}
	parsed, err := time.ParseDuration(value)
	if err != nil || parsed <= 0 {
		return 0, fmt.Errorf("%s inválido", key)
	}
	return parsed, nil
}
func getenv(key, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(key)); value != "" {
		return value
	}
	return fallback
}
