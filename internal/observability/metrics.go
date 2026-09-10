package observability

import (
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promhttp"
)

var globalMetrics struct {
	sync.RWMutex
	value *Metrics
}

func SetMetrics(metrics *Metrics) {
	globalMetrics.Lock()
	globalMetrics.value = metrics
	globalMetrics.Unlock()
}

func CurrentMetrics() *Metrics {
	globalMetrics.RLock()
	defer globalMetrics.RUnlock()
	return globalMetrics.value
}

type Metrics struct {
	Registry                                                                                                                                                                               *prometheus.Registry
	Audits, Deals, Messages, Findings, External, Errors, Reports, ConversationUnavailable, DashboardLoads, DashboardExports, DashboardQueryErrors, CRMAIDivergences, PriorityOpportunities *prometheus.CounterVec
	CRMSyncRuns, CRMSyncMarkers                                                                                                                                                            *prometheus.CounterVec
	CRMSyncDeals, CRMSyncActivities                                                                                                                                                        prometheus.Counter
	MessagesPersisted, MessagePersistenceFailures                                                                                                                                          prometheus.Counter
	InProgress, DependencyUp, ModelAvailable                                                                                                                                               *prometheus.GaugeVec
	CRMSyncLastSuccess                                                                                                                                                                     prometheus.Gauge
	AuditDuration, BitrixDuration, RulesDuration, OllamaDuration, PostgresDuration, ReportDuration, HTTPDuration, ExternalDuration, DashboardLoadDuration                                  *prometheus.HistogramVec
	CRMSyncDuration                                                                                                                                                                        prometheus.Histogram
	MessageCollectionGap                                                                                                                                                                   prometheus.Histogram
}

func NewMetrics() *Metrics {
	m := &Metrics{Registry: prometheus.NewRegistry()}
	m.Audits = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "auditor_audits_total", Help: "Auditorias executadas."}, []string{"status"})
	m.Deals = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "auditor_deals_synced_total", Help: "Negócios sincronizados."}, []string{"status"})
	m.Messages = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "auditor_messages_collected_total", Help: "Mensagens coletadas."}, []string{"status"})
	m.Findings = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "auditor_findings_total", Help: "Achados determinísticos."}, []string{"severity"})
	m.External = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "auditor_external_requests_total", Help: "Chamadas externas."}, []string{"dependency", "operation", "status"})
	m.Errors = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "auditor_errors_total", Help: "Erros por operação."}, []string{"operation"})
	m.Reports = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "auditor_reports_generated_total", Help: "Relatórios gerados."}, []string{"status"})
	m.ConversationUnavailable = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "conversation_unavailable_total", Help: "Conversas indisponíveis por motivo."}, []string{"reason"})
	m.DashboardLoads = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "dashboard_load_total", Help: "Carregamentos empresariais."}, []string{"screen", "status"})
	m.DashboardExports = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "dashboard_export_total", Help: "Exportações empresariais."}, []string{"type", "status"})
	m.DashboardQueryErrors = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "dashboard_query_errors_total", Help: "Erros de consulta empresarial."}, []string{"screen"})
	m.CRMAIDivergences = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "crm_ai_divergences_total", Help: "Divergências CRM e IA."}, []string{"type"})
	m.PriorityOpportunities = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "priority_opportunities_total", Help: "Oportunidades prioritárias."}, []string{"priority"})
	m.CRMSyncRuns = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "auditor_crm_sync_runs_total", Help: "Sincronizações globais do CRM por status."}, []string{"status"})
	m.CRMSyncDeals = prometheus.NewCounter(prometheus.CounterOpts{Name: "auditor_crm_sync_deals_total", Help: "Negócios coletados nas sincronizações globais."})
	m.CRMSyncActivities = prometheus.NewCounter(prometheus.CounterOpts{Name: "auditor_crm_sync_activities_total", Help: "Atividades coletadas nas sincronizações globais."})
	m.CRMSyncMarkers = prometheus.NewCounterVec(prometheus.CounterOpts{Name: "auditor_crm_sync_markers_total", Help: "Marcadores únicos coletados."}, []string{"marker"})
	m.CRMSyncLastSuccess = prometheus.NewGauge(prometheus.GaugeOpts{Name: "auditor_crm_sync_last_success_timestamp_seconds", Help: "Timestamp da última sincronização global concluída."})
	m.CRMSyncDuration = prometheus.NewHistogram(prometheus.HistogramOpts{Name: "auditor_crm_sync_duration_seconds", Help: "Duração da sincronização global do CRM.", Buckets: []float64{1, 5, 15, 30, 60, 120, 300, 600, 1200, 1800}})
	m.MessagesPersisted = prometheus.NewCounter(prometheus.CounterOpts{Name: "auditor_messages_persisted_total", Help: "Mensagens confirmadas pelo UPSERT PostgreSQL."})
	m.MessagePersistenceFailures = prometheus.NewCounter(prometheus.CounterOpts{Name: "auditor_message_persistence_failures_total", Help: "Falhas na persistência obrigatória de mensagens."})
	m.MessageCollectionGap = prometheus.NewHistogram(prometheus.HistogramOpts{Name: "auditor_messages_collected_analyzed_gap", Help: "Diferença entre mensagens coletadas e enviadas ao analisador.", Buckets: []float64{0, 1, 10, 25, 50, 100, 250, 500, 1000}})
	m.InProgress = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "auditor_audits_in_progress", Help: "Auditorias em andamento."}, []string{})
	m.DependencyUp = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "auditor_dependency_up", Help: "Disponibilidade das dependências."}, []string{"dependency"})
	m.ModelAvailable = prometheus.NewGaugeVec(prometheus.GaugeOpts{Name: "auditor_ollama_model_available", Help: "Disponibilidade do modelo configurado."}, []string{})
	m.AuditDuration = histogram("auditor_audit_duration_seconds", []string{"status"})
	m.BitrixDuration = histogram("auditor_bitrix_request_duration_seconds", []string{"operation", "status"})
	m.RulesDuration = histogram("auditor_rules_duration_seconds", []string{"status"})
	m.OllamaDuration = histogram("auditor_ollama_duration_seconds", []string{"status"})
	m.PostgresDuration = histogram("auditor_postgres_duration_seconds", []string{"operation", "status"})
	m.ReportDuration = histogram("auditor_report_duration_seconds", []string{"status"})
	m.HTTPDuration = histogram("auditor_http_request_duration_seconds", []string{"method", "route", "status"})
	m.ExternalDuration = histogram("auditor_external_request_duration_seconds", []string{"dependency", "operation", "status"})
	m.DashboardLoadDuration = histogram("dashboard_load_duration_seconds", []string{"screen"})
	m.Registry.MustRegister(m.Audits, m.Deals, m.Messages, m.MessagesPersisted, m.MessagePersistenceFailures, m.MessageCollectionGap, m.Findings, m.External, m.Errors, m.Reports, m.ConversationUnavailable, m.DashboardLoads, m.DashboardExports, m.DashboardQueryErrors, m.CRMAIDivergences, m.PriorityOpportunities, m.CRMSyncRuns, m.CRMSyncDeals, m.CRMSyncActivities, m.CRMSyncMarkers, m.CRMSyncLastSuccess, m.CRMSyncDuration, m.InProgress, m.DependencyUp, m.ModelAvailable, m.AuditDuration, m.BitrixDuration, m.RulesDuration, m.OllamaDuration, m.PostgresDuration, m.ReportDuration, m.HTTPDuration, m.ExternalDuration, m.DashboardLoadDuration)
	return m
}

func histogram(name string, labels []string) *prometheus.HistogramVec {
	return prometheus.NewHistogramVec(prometheus.HistogramOpts{Name: name, Help: name, Buckets: append(append([]float64(nil), prometheus.DefBuckets...), 30, 60, 120, 300, 600)}, labels)
}

func (m *Metrics) Handler() http.Handler {
	return promhttp.HandlerFor(m.Registry, promhttp.HandlerOpts{})
}

func (m *Metrics) HTTPMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		wrapped := &statusWriter{ResponseWriter: w, status: http.StatusOK}
		next.ServeHTTP(wrapped, r)
		m.HTTPDuration.WithLabelValues(r.Method, normalizedRoute(r.URL.Path), strconv.Itoa(wrapped.status)).Observe(time.Since(start).Seconds())
	})
}

type statusWriter struct {
	http.ResponseWriter
	status int
}

func (w *statusWriter) WriteHeader(status int) {
	w.status = status
	w.ResponseWriter.WriteHeader(status)
}

func normalizedRoute(path string) string {
	switch {
	case path == "/health", path == "/ready":
		return path
	case path == "/api/v1/audits":
		return "/api/v1/audits"
	case len(path) >= len("/api/v1/audits/") && path[:len("/api/v1/audits/")] == "/api/v1/audits/":
		return "/api/v1/audits/{id}"
	case len(path) >= len("/api/v1/deals/") && path[:len("/api/v1/deals/")] == "/api/v1/deals/":
		return "/api/v1/deals/{id}/analysis"
	case len(path) >= len("/api/v1/dashboard/") && path[:len("/api/v1/dashboard/")] == "/api/v1/dashboard/":
		return "/api/v1/dashboard/{screen}"
	default:
		return "unmatched"
	}
}
