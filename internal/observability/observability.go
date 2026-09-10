package observability

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/portfolio/auditor-ia/internal/platform/security"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/exporters/otlp/otlptrace/otlptracegrpc"
	"go.opentelemetry.io/otel/propagation"
	"go.opentelemetry.io/otel/sdk/resource"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

const InstrumentationName = "github.com/portfolio/auditor-ia"

type Config struct {
	ServiceName, ServiceVersion, Environment string
	LogLevel, LogFormat                      string
	OTLPEndpoint                             string
	TracingEnabled                           bool
	SampleRatio                              float64
}

func NewLogger(cfg Config, output io.Writer) (*slog.Logger, error) {
	if output == nil {
		output = os.Stdout
	}
	var level slog.Level
	if err := level.UnmarshalText([]byte(strings.ToUpper(cfg.LogLevel))); err != nil {
		return nil, fmt.Errorf("LOG_LEVEL inválido: %w", err)
	}
	opts := &slog.HandlerOptions{Level: level}
	var handler slog.Handler
	if cfg.LogFormat == "json" {
		handler = slog.NewJSONHandler(output, opts)
	} else {
		handler = slog.NewTextHandler(output, opts)
	}
	return slog.New(handler).With("service.name", cfg.ServiceName, "service.version", cfg.ServiceVersion, "deployment.environment", cfg.Environment), nil
}

func SetupTracing(ctx context.Context, cfg Config) (func(context.Context) error, error) {
	otel.SetTextMapPropagator(propagation.NewCompositeTextMapPropagator(propagation.TraceContext{}, propagation.Baggage{}))
	if !cfg.TracingEnabled || strings.TrimSpace(cfg.OTLPEndpoint) == "" {
		provider := sdktrace.NewTracerProvider(sdktrace.WithSampler(sdktrace.NeverSample()))
		otel.SetTracerProvider(provider)
		return provider.Shutdown, nil
	}
	exporter, err := otlptracegrpc.New(ctx, otlptracegrpc.WithEndpoint(cfg.OTLPEndpoint), otlptracegrpc.WithInsecure())
	if err != nil {
		return nil, fmt.Errorf("configurar exportador OTLP: %w", err)
	}
	res, err := resource.New(ctx, resource.WithAttributes(
		attribute.String("service.name", cfg.ServiceName),
		attribute.String("service.version", cfg.ServiceVersion),
		attribute.String("deployment.environment", cfg.Environment),
	))
	if err != nil {
		return nil, fmt.Errorf("configurar recurso OpenTelemetry: %w", err)
	}
	ratio := cfg.SampleRatio
	if ratio <= 0 || ratio > 1 {
		ratio = 1
	}
	provider := sdktrace.NewTracerProvider(sdktrace.WithBatcher(exporter), sdktrace.WithResource(res), sdktrace.WithSampler(sdktrace.ParentBased(sdktrace.TraceIDRatioBased(ratio))))
	otel.SetTracerProvider(provider)
	return provider.Shutdown, nil
}

func Tracer() trace.Tracer { return otel.Tracer(InstrumentationName) }

func TraceID(ctx context.Context) string {
	spanContext := trace.SpanContextFromContext(ctx)
	if !spanContext.IsValid() {
		return ""
	}
	return spanContext.TraceID().String()
}

func LogAttrs(ctx context.Context, attrs ...any) []any {
	spanContext := trace.SpanContextFromContext(ctx)
	if spanContext.IsValid() {
		attrs = append(attrs, "trace_id", spanContext.TraceID().String(), "span_id", spanContext.SpanID().String())
	}
	if requestID, ok := RequestID(ctx); ok {
		attrs = append(attrs, "request_id", requestID)
	}
	return attrs
}

func SafeError(err error) string {
	if err == nil {
		return ""
	}
	return security.Error(err)
}

type requestIDKey struct{}

func WithRequestID(ctx context.Context, id string) context.Context {
	return context.WithValue(ctx, requestIDKey{}, strings.TrimSpace(id))
}

func RequestID(ctx context.Context) (string, bool) {
	id, ok := ctx.Value(requestIDKey{}).(string)
	return id, ok && id != ""
}

func EnvConfig(service, version, environment string) (Config, error) {
	format := strings.ToLower(env("LOG_FORMAT", map[bool]string{true: "json", false: "text"}[environment == "production"]))
	if format != "json" && format != "text" {
		return Config{}, fmt.Errorf("LOG_FORMAT deve ser json ou text")
	}
	ratio, err := strconv.ParseFloat(env("OTEL_TRACES_SAMPLER_ARG", "1"), 64)
	if err != nil || ratio < 0 || ratio > 1 {
		return Config{}, fmt.Errorf("OTEL_TRACES_SAMPLER_ARG deve estar entre 0 e 1")
	}
	return Config{ServiceName: env("OTEL_SERVICE_NAME", service), ServiceVersion: version, Environment: environment, LogLevel: env("LOG_LEVEL", "INFO"), LogFormat: format, OTLPEndpoint: strings.TrimPrefix(strings.TrimPrefix(env("OTEL_EXPORTER_OTLP_ENDPOINT", ""), "http://"), "https://"), TracingEnabled: strings.EqualFold(env("OTEL_TRACING_ENABLED", "false"), "true"), SampleRatio: ratio}, nil
}

func env(name, fallback string) string {
	if value := strings.TrimSpace(os.Getenv(name)); value != "" {
		return value
	}
	return fallback
}

func DurationMilliseconds(start time.Time) float64 {
	return float64(time.Since(start).Microseconds()) / 1000
}

type PrintfLogger struct{ Logger *slog.Logger }

func (l PrintfLogger) Printf(format string, args ...any) {
	if l.Logger != nil {
		l.Logger.Info(security.Text(fmt.Sprintf(format, args...)))
	}
}
