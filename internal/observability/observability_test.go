package observability

import (
	"bytes"
	"context"
	"errors"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/prometheus/client_golang/prometheus/testutil"
)

func TestJSONLoggerHasCommonFieldsAndSanitizesErrors(t *testing.T) {
	var output bytes.Buffer
	logger, err := NewLogger(Config{ServiceName: "auditor-test", ServiceVersion: "1", Environment: "test", LogLevel: "INFO", LogFormat: "json"}, &output)
	if err != nil {
		t.Fatal(err)
	}
	logger.Error("operation_failed", "error", SafeError(errors.New("postgres://user:secret@localhost/db")))
	value := output.String()
	for _, expected := range []string{"service.name", "auditor-test", "service.version", "deployment.environment"} {
		if !strings.Contains(value, expected) {
			t.Fatalf("campo ausente %q: %s", expected, value)
		}
	}
	if strings.Contains(value, "secret") || strings.Contains(value, "user:") {
		t.Fatalf("credencial vazou no log: %s", value)
	}
}

func TestReadinessCriticalAndDegradedSemantics(t *testing.T) {
	up := func(context.Context) error { return nil }
	down := func(context.Context) error { return errors.New("down") }
	critical := &Readiness{Postgres: down, Bitrix: up, Ollama: up, Timeout: time.Second}
	status, _ := critical.Check(context.Background())
	if status != "not_ready" {
		t.Fatalf("status = %s", status)
	}
	degraded := &Readiness{Postgres: up, Bitrix: down, Ollama: up, Timeout: time.Second}
	status, _ = degraded.Check(context.Background())
	if status != "degraded" {
		t.Fatalf("status = %s", status)
	}
}

func TestReadinessCacheAvoidsRepeatedChecks(t *testing.T) {
	var calls atomic.Int32
	check := func(context.Context) error { calls.Add(1); return nil }
	readiness := &Readiness{Postgres: check, Bitrix: check, Ollama: check, Timeout: time.Second, TTL: time.Minute}
	readiness.Check(context.Background())
	readiness.Check(context.Background())
	if calls.Load() != 3 {
		t.Fatalf("checks = %d, esperado 3", calls.Load())
	}
}

func TestMetricsUseControlledLabels(t *testing.T) {
	metrics := NewMetrics()
	metrics.Audits.WithLabelValues("completed").Inc()
	if got := testutil.ToFloat64(metrics.Audits.WithLabelValues("completed")); got != 1 {
		t.Fatalf("counter = %v", got)
	}
}
