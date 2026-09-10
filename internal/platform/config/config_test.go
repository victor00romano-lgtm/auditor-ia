package config

import (
	"strings"
	"testing"
	"time"
)

func base(t *testing.T) {
	t.Helper()
	t.Setenv("DATABASE_URL", "postgres://u:p@localhost/db")
	t.Setenv("BITRIX_WEBHOOK_URL", "http://localhost/rest/1/token")
	t.Setenv("OLLAMA_URL", "http://localhost:11434")
	t.Setenv("OLLAMA_MODEL", "model")
	t.Setenv("AUDITOR_API_KEY", strings.Repeat("x", 32))
	t.Setenv("BITRIX_HTTP_TIMEOUT", "30s")
	t.Setenv("OLLAMA_HTTP_TIMEOUT", "9m")
	t.Setenv("API_AUDIT_TIMEOUT", "10m")
}

func TestHTTPTimeoutDefaultsAndParsing(t *testing.T) {
	base(t)
	t.Setenv("BITRIX_HTTP_TIMEOUT", "12s")
	t.Setenv("OLLAMA_HTTP_TIMEOUT", "4m")
	t.Setenv("API_AUDIT_TIMEOUT", "5m")
	c, err := Load(API)
	if err != nil {
		t.Fatal(err)
	}
	if c.BitrixHTTPTimeout != 12*time.Second || c.OllamaHTTPTimeout != 4*time.Minute || c.AuditTimeout != 5*time.Minute {
		t.Fatalf("timeouts incorretos: %#v", c)
	}
}

func TestHTTPTimeoutDefaults(t *testing.T) {
	base(t)
	t.Setenv("BITRIX_HTTP_TIMEOUT", "")
	t.Setenv("OLLAMA_HTTP_TIMEOUT", "")
	t.Setenv("API_AUDIT_TIMEOUT", "")
	c, err := Load(API)
	if err != nil {
		t.Fatal(err)
	}
	if c.BitrixHTTPTimeout != 30*time.Second || c.OllamaHTTPTimeout != 9*time.Minute || c.AuditTimeout != 10*time.Minute {
		t.Fatalf("defaults incorretos: %#v", c)
	}
}

func TestHTTPTimeoutsRejectInvalidValuesAndHierarchy(t *testing.T) {
	tests := []struct{ name, key, value, want string }{
		{"bitrix inválido", "BITRIX_HTTP_TIMEOUT", "0s", "BITRIX_HTTP_TIMEOUT"},
		{"ollama inválido", "OLLAMA_HTTP_TIMEOUT", "-1s", "OLLAMA_HTTP_TIMEOUT"},
		{"bitrix maior que ollama", "BITRIX_HTTP_TIMEOUT", "9m", "menor que OLLAMA_HTTP_TIMEOUT"},
		{"ollama igual à API", "OLLAMA_HTTP_TIMEOUT", "10m", "menor que API_AUDIT_TIMEOUT"},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			base(t)
			t.Setenv(test.key, test.value)
			_, err := Load(API)
			if err == nil || !strings.Contains(err.Error(), test.want) {
				t.Fatalf("erro=%v; esperado conter %q", err, test.want)
			}
		})
	}
}
func TestConfigByExecutableAndWebhookNormalization(t *testing.T) {
	base(t)
	c, e := Load(API)
	if e != nil || !strings.HasSuffix(c.BitrixWebhookURL, "/") {
		t.Fatalf("config: %#v %v", c, e)
	}
	t.Setenv("OLLAMA_MODEL", "")
	if _, e = Load(DealSync); e != nil {
		t.Fatal("dealsync exigiu Ollama")
	}
	if _, e = Load(AnalysisSync); e == nil || !strings.Contains(e.Error(), "OLLAMA_MODEL") {
		t.Fatal("analysissync não validou modelo")
	}
}
func TestProductionPrivacyAndHTTPS(t *testing.T) {
	base(t)
	t.Setenv("APP_ENV", "production")
	t.Setenv("AI_PRIVACY_MODE", "preserve")
	if _, e := Load(API); e == nil {
		t.Fatal("preserve sem consentimento aceito")
	}
	t.Setenv("ALLOW_PII_TO_AI", "true")
	if _, e := Load(API); e == nil || !strings.Contains(e.Error(), "HTTPS") {
		t.Fatal("HTTP aceito em produção")
	}
}

func TestBatchWorkerConfiguration(t *testing.T) {
	base(t)
	t.Setenv("AUDIT_WORKERS", "4")
	t.Setenv("AUDIT_MAX_ATTEMPTS", "6")
	t.Setenv("AUDIT_LEASE_DURATION", "20m")
	t.Setenv("AUDIT_QUEUE_POLL_INTERVAL", "2s")
	t.Setenv("BITRIX_MAX_CONCURRENCY", "5")
	t.Setenv("OLLAMA_MAX_CONCURRENCY", "1")
	c, err := Load(AuditWorker)
	if err != nil {
		t.Fatal(err)
	}
	if c.BatchWorkers != 4 || c.BatchMaxAttempts != 6 || c.BatchLease != 20*time.Minute || c.BatchPoll != 2*time.Second || c.BitrixMaxConcurrency != 5 || c.OllamaMaxConcurrency != 1 {
		t.Fatalf("configuração de lote: %#v", c)
	}
}

func TestBatchWorkerRejectsInvalidConfiguration(t *testing.T) {
	for _, key := range []string{"AUDIT_WORKERS", "AUDIT_MAX_ATTEMPTS", "BITRIX_MAX_CONCURRENCY", "OLLAMA_MAX_CONCURRENCY"} {
		t.Run(key, func(t *testing.T) {
			base(t)
			t.Setenv(key, "0")
			if _, err := Load(AuditWorker); err == nil || !strings.Contains(err.Error(), key) {
				t.Fatalf("erro=%v", err)
			}
		})
	}
}

func TestBatchLeaseMustExceedAuditTimeout(t *testing.T) {
	base(t)
	t.Setenv("AUDIT_LEASE_DURATION", "5m")
	if _, err := Load(AuditWorker); err == nil || !strings.Contains(err.Error(), "maior que API_AUDIT_TIMEOUT") {
		t.Fatalf("erro=%v", err)
	}
}

func TestSupportAssigneeIDsDefaultsParsesAndRejectsInvalidValues(t *testing.T) {
	base(t)
	t.Setenv("BITRIX_SUPPORT_ASSIGNEE_IDS", "")
	c, err := Load(API)
	if err != nil || len(c.SupportAssigneeIDs) != 1 || c.SupportAssigneeIDs[0] != 3066 {
		t.Fatalf("IDs padrão=%v err=%v", c.SupportAssigneeIDs, err)
	}
	t.Setenv("BITRIX_SUPPORT_ASSIGNEE_IDS", "3066, 4001,3066")
	c, err = Load(API)
	if err != nil || len(c.SupportAssigneeIDs) != 2 || c.SupportAssigneeIDs[0] != 3066 || c.SupportAssigneeIDs[1] != 4001 {
		t.Fatalf("IDs configurados=%v err=%v", c.SupportAssigneeIDs, err)
	}
	for _, invalid := range []string{"0", "-1", "3066,x", "3066,"} {
		t.Setenv("BITRIX_SUPPORT_ASSIGNEE_IDS", invalid)
		if _, err = Load(API); err == nil || !strings.Contains(err.Error(), "BITRIX_SUPPORT_ASSIGNEE_IDS") {
			t.Fatalf("valor inválido %q aceito: %v", invalid, err)
		}
	}
}

func TestCRMStatsSyncConfiguration(t *testing.T) {
	base(t)
	t.Setenv("CRM_STATS_SYNC_TIMEOUT", "25m")
	t.Setenv("CRM_STATS_PAGE_DELAY", "250ms")
	c, err := Load(CRMStatsSync)
	if err != nil {
		t.Fatal(err)
	}
	if c.CRMStatsSyncTimeout != 25*time.Minute || c.CRMStatsPageDelay != 250*time.Millisecond {
		t.Fatalf("configuração=%#v", c)
	}
	for _, test := range []struct{ key, value string }{{"CRM_STATS_SYNC_TIMEOUT", "0s"}, {"CRM_STATS_PAGE_DELAY", "-1ms"}} {
		base(t)
		t.Setenv("CRM_STATS_SYNC_TIMEOUT", "30m")
		t.Setenv("CRM_STATS_PAGE_DELAY", "100ms")
		t.Setenv(test.key, test.value)
		if _, err := Load(CRMStatsSync); err == nil || !strings.Contains(err.Error(), test.key) {
			t.Fatalf("%s=%s aceito: %v", test.key, test.value, err)
		}
	}
}
