package config

import (
	"fmt"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
)

type Target string

const (
	API             Target = "api"
	Auditor         Target = "auditor"
	AnalysisSync    Target = "analysissync"
	DealSync        Target = "dealsync"
	MessageSync     Target = "messagesync"
	AssigneeSync    Target = "assigneesync"
	AuditWorker     Target = "auditworker"
	CRMStatsSync    Target = "crmstatssync"
	MessageBackfill Target = "messagebackfill"
	SecurityCheck   Target = "securitycheck"
)

type Config struct {
	AppEnv, DatabaseURL, BitrixWebhookURL, APIKey, OllamaURL, OllamaModel                                              string
	PrivacyMode                                                                                                        string
	AllowPII                                                                                                           bool
	AuditTimeout, BitrixHTTPTimeout, OllamaHTTPTimeout, CRMStatsSyncTimeout, CRMStatsPageDelay, MessageBackfillTimeout time.Duration
	RateLimitRPS                                                                                                       float64
	RateLimitBurst, MaxBodyBytes                                                                                       int
	AllowedOrigins                                                                                                     []string
	Retention                                                                                                          Retention
	LogLevel, LogFormat, ServiceName, ServiceVersion, MetricsAddr                                                      string
	OTLPEndpoint                                                                                                       string
	OTELTracingEnabled                                                                                                 bool
	OTELSampleRatio                                                                                                    float64
	ReadinessTimeout, ReadinessCacheTTL                                                                                time.Duration
	BatchLease, BatchPoll                                                                                              time.Duration
	BatchWorkers, BatchMaxAttempts, BitrixMaxConcurrency, OllamaMaxConcurrency                                         int
	SupportAssigneeIDs                                                                                                 []int64
}

type Retention struct{ MessagesDays, RawAnalysisDays, AssessmentsDays, LogsDays, ReportsDays int }

func Load(target Target) (Config, error) {
	c := Config{AppEnv: value("APP_ENV", "development"), PrivacyMode: value("AI_PRIVACY_MODE", "anonymize")}
	var err error
	if c.AppEnv != "development" && c.AppEnv != "production" {
		return c, fmt.Errorf("APP_ENV deve ser development ou production")
	}
	if c.PrivacyMode != "anonymize" && c.PrivacyMode != "preserve" {
		return c, fmt.Errorf("AI_PRIVACY_MODE deve ser anonymize ou preserve")
	}
	c.AllowPII = strings.EqualFold(value("ALLOW_PII_TO_AI", "false"), "true")
	c.LogLevel = value("LOG_LEVEL", "INFO")
	c.LogFormat = value("LOG_FORMAT", map[bool]string{true: "json", false: "text"}[c.AppEnv == "production"])
	c.ServiceName = value("SERVICE_NAME", "auditor-api")
	c.ServiceVersion = value("SERVICE_VERSION", "dev")
	c.MetricsAddr = value("METRICS_ADDR", ":9091")
	c.OTLPEndpoint = value("OTEL_EXPORTER_OTLP_ENDPOINT", "")
	c.OTELTracingEnabled = strings.EqualFold(value("OTEL_TRACING_ENABLED", "false"), "true")
	c.OTELSampleRatio, err = decimal("OTEL_TRACES_SAMPLER_ARG", 1, 0, 1)
	if err != nil {
		return c, err
	}
	c.ReadinessTimeout, err = duration("READINESS_TIMEOUT", 2*time.Second, 100*time.Millisecond, 30*time.Second)
	if err != nil {
		return c, err
	}
	c.ReadinessCacheTTL, err = duration("READINESS_CACHE_TTL", 5*time.Second, 100*time.Millisecond, time.Minute)
	if err != nil {
		return c, err
	}
	if c.AppEnv == "production" && c.PrivacyMode == "preserve" && !c.AllowPII {
		return c, fmt.Errorf("ALLOW_PII_TO_AI=true é obrigatório para AI_PRIVACY_MODE=preserve em production")
	}
	required := map[string]bool{"DATABASE_URL": true}
	if target == API || target == AnalysisSync || target == DealSync || target == MessageSync || target == AssigneeSync || target == AuditWorker || target == CRMStatsSync || target == MessageBackfill {
		required["BITRIX_WEBHOOK_URL"] = true
	}
	if target == API || target == AnalysisSync || target == AuditWorker {
		required["OLLAMA_URL"], required["OLLAMA_MODEL"] = true, true
	}
	if target == API {
		required["AUDITOR_API_KEY"] = true
	}
	missing := []string{}
	for name := range required {
		if strings.TrimSpace(os.Getenv(name)) == "" {
			missing = append(missing, name)
		}
	}
	if len(missing) > 0 {
		return c, fmt.Errorf("variáveis obrigatórias ausentes: %s", strings.Join(missing, ", "))
	}
	c.DatabaseURL = strings.TrimSpace(os.Getenv("DATABASE_URL"))
	if c.DatabaseURL != "" {
		if _, err := pgxpool.ParseConfig(c.DatabaseURL); err != nil {
			return c, fmt.Errorf("DATABASE_URL inválida")
		}
	}
	c.BitrixWebhookURL = strings.TrimSpace(os.Getenv("BITRIX_WEBHOOK_URL"))
	if c.BitrixWebhookURL != "" {
		u, err := url.Parse(c.BitrixWebhookURL)
		if err != nil || u.Host == "" || (u.Scheme != "http" && u.Scheme != "https") {
			return c, fmt.Errorf("BITRIX_WEBHOOK_URL inválida")
		}
		if c.AppEnv == "production" && u.Scheme != "https" {
			return c, fmt.Errorf("BITRIX_WEBHOOK_URL deve usar HTTPS em production")
		}
		c.BitrixWebhookURL = strings.TrimRight(c.BitrixWebhookURL, "/") + "/"
	}
	c.OllamaURL, c.OllamaModel, c.APIKey = strings.TrimRight(strings.TrimSpace(os.Getenv("OLLAMA_URL")), "/"), strings.TrimSpace(os.Getenv("OLLAMA_MODEL")), strings.TrimSpace(os.Getenv("AUDITOR_API_KEY"))
	minKey, err := integer("API_KEY_MIN_LENGTH", 32, 8, 256)
	if err != nil {
		return c, err
	}
	if c.AppEnv == "production" && minKey < 32 {
		minKey = 32
	}
	if target == API && len(c.APIKey) < minKey {
		return c, fmt.Errorf("AUDITOR_API_KEY deve possuir pelo menos %d caracteres", minKey)
	}
	c.AuditTimeout, err = duration("API_AUDIT_TIMEOUT", 10*time.Minute, time.Second, 30*time.Minute)
	if err != nil {
		return c, err
	}
	c.BitrixHTTPTimeout, err = duration("BITRIX_HTTP_TIMEOUT", 30*time.Second, time.Second, 30*time.Minute)
	if err != nil {
		return c, err
	}
	c.OllamaHTTPTimeout, err = duration("OLLAMA_HTTP_TIMEOUT", 9*time.Minute, time.Second, 30*time.Minute)
	if err != nil {
		return c, err
	}
	if c.BitrixHTTPTimeout >= c.OllamaHTTPTimeout {
		return c, fmt.Errorf("BITRIX_HTTP_TIMEOUT deve ser menor que OLLAMA_HTTP_TIMEOUT")
	}
	if c.OllamaHTTPTimeout >= c.AuditTimeout {
		return c, fmt.Errorf("OLLAMA_HTTP_TIMEOUT deve ser menor que API_AUDIT_TIMEOUT")
	}
	c.RateLimitRPS, err = decimal("API_RATE_LIMIT_RPS", 5, .1, 1000)
	if err != nil {
		return c, err
	}
	c.RateLimitBurst, err = integer("API_RATE_LIMIT_BURST", 10, 1, 10000)
	if err != nil {
		return c, err
	}
	c.MaxBodyBytes, err = integer("API_MAX_BODY_BYTES", 1048576, 1024, 32<<20)
	if err != nil {
		return c, err
	}
	c.BatchWorkers, err = integer("AUDIT_WORKERS", 2, 1, 64)
	if err != nil {
		return c, err
	}
	c.BatchMaxAttempts, err = integer("AUDIT_MAX_ATTEMPTS", 5, 1, 20)
	if err != nil {
		return c, err
	}
	c.BitrixMaxConcurrency, err = integer("BITRIX_MAX_CONCURRENCY", 3, 1, 64)
	if err != nil {
		return c, err
	}
	c.OllamaMaxConcurrency, err = integer("OLLAMA_MAX_CONCURRENCY", 1, 1, 16)
	if err != nil {
		return c, err
	}
	c.BatchLease, err = duration("AUDIT_LEASE_DURATION", 15*time.Minute, time.Minute, time.Hour)
	if err != nil {
		return c, err
	}
	if target == AuditWorker && c.BatchLease <= c.AuditTimeout {
		return c, fmt.Errorf("AUDIT_LEASE_DURATION deve ser maior que API_AUDIT_TIMEOUT")
	}
	c.BatchPoll, err = duration("AUDIT_QUEUE_POLL_INTERVAL", time.Second, 100*time.Millisecond, time.Minute)
	if err != nil {
		return c, err
	}
	c.CRMStatsSyncTimeout, err = duration("CRM_STATS_SYNC_TIMEOUT", 30*time.Minute, time.Minute, 2*time.Hour)
	if err != nil {
		return c, err
	}
	c.CRMStatsPageDelay, err = duration("CRM_STATS_PAGE_DELAY", 100*time.Millisecond, 0, 10*time.Second)
	if err != nil {
		return c, err
	}
	c.MessageBackfillTimeout, err = duration("MESSAGE_BACKFILL_TIMEOUT", 30*time.Minute, time.Minute, 24*time.Hour)
	if err != nil {
		return c, err
	}
	c.SupportAssigneeIDs, err = positiveInt64List("BITRIX_SUPPORT_ASSIGNEE_IDS", "3066")
	if err != nil {
		return c, err
	}
	for _, origin := range strings.Split(os.Getenv("API_ALLOWED_ORIGINS"), ",") {
		if origin = strings.TrimSpace(origin); origin != "" {
			c.AllowedOrigins = append(c.AllowedOrigins, origin)
		}
	}
	c.Retention.MessagesDays, err = integer("RETENTION_MESSAGES_DAYS", 180, 0, 36500)
	if err != nil {
		return c, err
	}
	c.Retention.RawAnalysisDays, err = integer("RETENTION_RAW_ANALYSIS_DAYS", 90, 0, 36500)
	if err != nil {
		return c, err
	}
	c.Retention.AssessmentsDays, err = integer("RETENTION_ASSESSMENTS_DAYS", 730, 0, 36500)
	if err != nil {
		return c, err
	}
	c.Retention.LogsDays, err = integer("RETENTION_LOGS_DAYS", 30, 0, 36500)
	if err != nil {
		return c, err
	}
	c.Retention.ReportsDays, err = integer("RETENTION_REPORTS_DAYS", 90, 0, 36500)
	if err != nil {
		return c, err
	}
	return c, nil
}

func positiveInt64List(name, fallback string) ([]int64, error) {
	raw := value(name, fallback)
	seen := make(map[int64]struct{})
	values := make([]int64, 0)
	for _, token := range strings.Split(raw, ",") {
		token = strings.TrimSpace(token)
		parsed, err := strconv.ParseInt(token, 10, 64)
		if err != nil || parsed <= 0 {
			return nil, fmt.Errorf("%s inválida: informe IDs positivos separados por vírgula", name)
		}
		if _, exists := seen[parsed]; exists {
			continue
		}
		seen[parsed] = struct{}{}
		values = append(values, parsed)
	}
	if len(values) == 0 {
		return nil, fmt.Errorf("%s inválida: informe ao menos um ID", name)
	}
	return values, nil
}
func value(name, fallback string) string {
	if v := strings.TrimSpace(os.Getenv(name)); v != "" {
		return v
	}
	return fallback
}
func integer(name string, fallback, min, max int) (int, error) {
	raw := value(name, strconv.Itoa(fallback))
	v, e := strconv.Atoi(raw)
	if e != nil || v < min || v > max {
		return 0, fmt.Errorf("%s inválida: faixa %d..%d", name, min, max)
	}
	return v, nil
}
func decimal(name string, fallback, min, max float64) (float64, error) {
	raw := value(name, strconv.FormatFloat(fallback, 'f', -1, 64))
	v, e := strconv.ParseFloat(raw, 64)
	if e != nil || v < min || v > max {
		return 0, fmt.Errorf("%s inválida", name)
	}
	return v, nil
}
func duration(name string, fallback, min, max time.Duration) (time.Duration, error) {
	raw := value(name, fallback.String())
	v, e := time.ParseDuration(raw)
	if e != nil || v < min || v > max {
		return 0, fmt.Errorf("%s inválida", name)
	}
	return v, nil
}
