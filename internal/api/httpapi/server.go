package httpapi

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"sync"
	"time"

	apiapp "github.com/portfolio/auditor-ia/internal/api/application"
	apidomain "github.com/portfolio/auditor-ia/internal/api/domain"
	auditdomain "github.com/portfolio/auditor-ia/internal/audit/domain"
	batchdomain "github.com/portfolio/auditor-ia/internal/batch/domain"
	business "github.com/portfolio/auditor-ia/internal/business/domain"
	"github.com/portfolio/auditor-ia/internal/observability"
	"github.com/portfolio/auditor-ia/internal/platform/security"
)

type Logger interface{ Printf(string, ...any) }
type Config struct {
	APIKey         string
	AuditTimeout   time.Duration
	Logger         Logger
	RateLimitRPS   float64
	RateLimitBurst int
	MaxBodyBytes   int64
	AllowedOrigins []string
	ServiceName    string
	ServiceVersion string
	Readiness      ReadinessChecker
	Business       BusinessLoader
	Batches        batchdomain.Queue
}
type BusinessLoader interface {
	Load(context.Context, business.GlobalFilters) (business.Dashboard, error)
}

type ReadinessResult struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
}

type ReadinessChecker interface {
	Check(context.Context) (string, map[string]string)
}
type Server struct {
	repo           apidomain.Repository
	runner         apidomain.AuditRunner
	report         apidomain.ReportRenderer
	apiKey         string
	auditTimeout   time.Duration
	logger         Logger
	mux            *http.ServeMux
	maxBodyBytes   int64
	allowedOrigins map[string]bool
	rate           *tokenBucket
	readyRate      *tokenBucket
	serviceName    string
	serviceVersion string
	readiness      ReadinessChecker
	business       BusinessLoader
	batches        batchdomain.Queue
}

type tokenBucket struct {
	mu                  sync.Mutex
	tokens, rate, burst float64
	last                time.Time
}

func (b *tokenBucket) allow() bool {
	b.mu.Lock()
	defer b.mu.Unlock()
	now := time.Now()
	b.tokens += (now.Sub(b.last).Seconds() * b.rate)
	if b.tokens > b.burst {
		b.tokens = b.burst
	}
	b.last = now
	if b.tokens < 1 {
		return false
	}
	b.tokens--
	return true
}

func New(config Config, repo apidomain.Repository, runner apidomain.AuditRunner, report apidomain.ReportRenderer) (*Server, error) {
	if strings.TrimSpace(config.APIKey) == "" {
		return nil, fmt.Errorf("AUDITOR_API_KEY não configurada")
	}
	if config.AuditTimeout <= 0 {
		config.AuditTimeout = 5 * time.Minute
	}
	if config.Logger == nil {
		config.Logger = log.Default()
	}
	if config.RateLimitRPS <= 0 {
		config.RateLimitRPS = 5
	}
	if config.RateLimitBurst <= 0 {
		config.RateLimitBurst = 10
	}
	if config.MaxBodyBytes <= 0 {
		config.MaxBodyBytes = 1 << 20
	}
	origins := map[string]bool{}
	for _, origin := range config.AllowedOrigins {
		origins[origin] = true
	}
	s := &Server{repo: repo, runner: runner, report: report, apiKey: config.APIKey, auditTimeout: config.AuditTimeout, logger: config.Logger, mux: http.NewServeMux(), maxBodyBytes: config.MaxBodyBytes, allowedOrigins: origins, rate: &tokenBucket{tokens: float64(config.RateLimitBurst), rate: config.RateLimitRPS, burst: float64(config.RateLimitBurst), last: time.Now()}, readyRate: &tokenBucket{tokens: 2, rate: 1, burst: 2, last: time.Now()}, serviceName: config.ServiceName, serviceVersion: config.ServiceVersion, readiness: config.Readiness, business: config.Business, batches: config.Batches}
	s.routes()
	return s, nil
}
func (s *Server) Handler() http.Handler { return s.protect(s.mux) }
func (s *Server) protect(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("X-Content-Type-Options", "nosniff")
		w.Header().Set("Cache-Control", "no-store")
		w.Header().Set("Referrer-Policy", "no-referrer")
		w.Header().Set("X-Frame-Options", "DENY")
		requestID := r.Header.Get("X-Request-ID")
		if requestID == "" {
			var raw [12]byte
			_, _ = rand.Read(raw[:])
			requestID = fmt.Sprintf("%x", raw[:])
		}
		w.Header().Set("X-Request-ID", requestID)
		r = r.WithContext(observability.WithRequestID(r.Context(), requestID))
		if origin := r.Header.Get("Origin"); origin != "" {
			if !s.allowedOrigins[origin] {
				s.writeError(w, r, http.StatusForbidden, "ORIGIN_NOT_ALLOWED", "Origem não permitida", nil)
				return
			}
			w.Header().Set("Access-Control-Allow-Origin", origin)
			w.Header().Set("Vary", "Origin")
		}
		if r.URL.Path != "/ready" && !s.rate.allow() {
			s.writeError(w, r, http.StatusTooManyRequests, "RATE_LIMITED", "Limite de requisições excedido", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) routes() {
	s.mux.HandleFunc("GET /health", s.health)
	s.mux.HandleFunc("GET /ready", s.ready)
	s.mux.Handle("/api/v1/", s.authenticate(http.HandlerFunc(s.apiRoutes)))
}
func (s *Server) apiRoutes(w http.ResponseWriter, r *http.Request) {
	switch {
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/audits":
		s.createAudit(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/audits":
		s.listAudits(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/audits/"):
		s.auditSubresource(w, r)
	case r.Method == http.MethodGet && r.URL.Path == "/api/v1/findings":
		s.listFindings(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/deals/"):
		s.dealAnalysis(w, r)
	case r.Method == http.MethodGet && strings.HasPrefix(r.URL.Path, "/api/v1/dashboard/"):
		s.businessDashboard(w, r)
	case r.Method == http.MethodPost && r.URL.Path == "/api/v1/audit-batches":
		s.createBatch(w, r)
	case (r.Method == http.MethodGet || r.Method == http.MethodPost) && strings.HasPrefix(r.URL.Path, "/api/v1/audit-batches/"):
		s.batchResource(w, r)
	default:
		s.writeError(w, r, http.StatusNotFound, "AUDIT_NOT_FOUND", "Recurso não encontrado", nil)
	}
}

func (s *Server) createBatch(w http.ResponseWriter, r *http.Request) {
	if s.batches == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Processamento em lote indisponível", nil)
		return
	}
	var input struct {
		DealIDs []int64 `json:"bitrix_deal_ids"`
	}
	if err := json.NewDecoder(r.Body).Decode(&input); err != nil {
		s.invalid(w, r, "JSON inválido")
		return
	}
	if len(input.DealIDs) == 0 || len(input.DealIDs) > 10000 {
		s.invalid(w, r, "bitrix_deal_ids deve possuir entre 1 e 10000 itens")
		return
	}
	seen := map[int64]bool{}
	for _, id := range input.DealIDs {
		if id <= 0 {
			s.invalid(w, r, "bitrix_deal_id inválido")
			return
		}
		seen[id] = true
	}
	ids := make([]int64, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	result, err := s.batches.Create(r.Context(), ids)
	if err != nil {
		s.handleInternal(w, r, "criar lote", err)
		return
	}
	writeJSON(w, http.StatusAccepted, envelope[batchdomain.Batch]{Data: result})
}

func (s *Server) batchResource(w http.ResponseWriter, r *http.Request) {
	if s.batches == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Processamento em lote indisponível", nil)
		return
	}
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/audit-batches/")
	cancel := strings.HasSuffix(path, "/cancel")
	if cancel {
		path = strings.TrimSuffix(path, "/cancel")
	}
	id, ok := positiveID(path)
	if !ok {
		s.invalid(w, r, "batch_id inválido")
		return
	}
	if cancel {
		if r.Method != http.MethodPost {
			s.writeError(w, r, http.StatusMethodNotAllowed, "METHOD_NOT_ALLOWED", "Método não permitido", nil)
			return
		}
		if err := s.batches.Cancel(r.Context(), id); errors.Is(err, batchdomain.ErrNotFound) {
			s.writeError(w, r, http.StatusNotFound, "BATCH_NOT_FOUND", "Lote não encontrado", nil)
			return
		} else if err != nil {
			s.handleInternal(w, r, "cancelar lote", err)
			return
		}
	}
	result, err := s.batches.Get(r.Context(), id)
	if errors.Is(err, batchdomain.ErrNotFound) {
		s.writeError(w, r, http.StatusNotFound, "BATCH_NOT_FOUND", "Lote não encontrado", nil)
		return
	}
	if err != nil {
		s.handleInternal(w, r, "consultar lote", err)
		return
	}
	writeJSON(w, http.StatusOK, envelope[batchdomain.Batch]{Data: result})
}

func (s *Server) businessDashboard(w http.ResponseWriter, r *http.Request) {
	if s.business == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Dashboard empresarial indisponível", nil)
		return
	}
	filters := business.GlobalFilters{Period: business.DashboardPeriod(r.URL.Query().Get("period")), Stage: r.URL.Query().Get("stage"), Assignee: r.URL.Query().Get("assignee"), ConversationStatus: r.URL.Query().Get("conversation_status"), OnlyWithoutValue: r.URL.Query().Get("only_without_value") == "true"}
	if filters.Period == "" {
		filters.Period = business.PeriodAll
	}
	result, err := s.business.Load(r.Context(), filters)
	if err != nil {
		s.handleInternal(w, r, "consultar dashboard empresarial", err)
		return
	}
	key := strings.TrimPrefix(r.URL.Path, "/api/v1/dashboard/")
	var data any
	switch key {
	case "executive":
		data = result.Executive
	case "funnel":
		data = result.Funnel
	case "priorities":
		data = result.Priorities
	case "divergences":
		data = result.Divergences
	case "crm-quality":
		data = result.CRMQuality
	case "conversations":
		data = result.Conversations
	case "objections":
		data = result.Objections
	case "agents":
		data = result.Agents
	case "trends":
		data = result.Trends
	case "ai-quality":
		data = result.AIQuality
	case "crm-statistics":
		data = result.CRMStatistics
	default:
		s.writeError(w, r, http.StatusNotFound, "DASHBOARD_NOT_FOUND", "Dashboard não encontrado", nil)
		return
	}
	writeJSON(w, http.StatusOK, envelope[any]{Data: data})
}
func (s *Server) authenticate(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		providedHash := sha256.Sum256([]byte(r.Header.Get("X-API-Key")))
		expectedHash := sha256.Sum256([]byte(s.apiKey))
		if subtle.ConstantTimeCompare(providedHash[:], expectedHash[:]) != 1 {
			s.writeError(w, r, http.StatusUnauthorized, "UNAUTHORIZED", "API key inválida ou ausente", nil)
			return
		}
		next.ServeHTTP(w, r)
	})
}
func (s *Server) health(w http.ResponseWriter, _ *http.Request) {
	if s.serviceName == "" && s.serviceVersion == "" {
		writeJSON(w, http.StatusOK, envelope[map[string]string]{Data: map[string]string{"status": "ok"}})
		return
	}
	writeJSON(w, http.StatusOK, envelope[map[string]string]{Data: map[string]string{"status": "ok", "service": s.serviceName, "version": s.serviceVersion}})
}

func (s *Server) ready(w http.ResponseWriter, r *http.Request) {
	if !s.readyRate.allow() {
		s.writeError(w, r, http.StatusTooManyRequests, "RATE_LIMITED", "Limite de requisições excedido", nil)
		return
	}
	if s.readiness == nil {
		writeJSON(w, http.StatusServiceUnavailable, envelope[ReadinessResult]{Data: ReadinessResult{Status: "not_ready", Checks: map[string]string{"postgres": "not_configured"}}})
		return
	}
	readinessStatus, checks := s.readiness.Check(r.Context())
	result := ReadinessResult{Status: readinessStatus, Checks: checks}
	status := http.StatusOK
	if result.Status == "not_ready" {
		status = http.StatusServiceUnavailable
	}
	writeJSON(w, status, envelope[ReadinessResult]{Data: result})
}

func (s *Server) createAudit(w http.ResponseWriter, r *http.Request) {
	if media := strings.ToLower(strings.TrimSpace(strings.Split(r.Header.Get("Content-Type"), ";")[0])); media != "application/json" {
		s.writeError(w, r, http.StatusUnsupportedMediaType, "INVALID_CONTENT_TYPE", "Content-Type deve ser application/json", nil)
		return
	}
	var body struct {
		BitrixDealID int64 `json:"bitrix_deal_id"`
	}
	decoder := json.NewDecoder(http.MaxBytesReader(w, r.Body, s.maxBodyBytes))
	decoder.DisallowUnknownFields()
	if err := decoder.Decode(&body); err != nil {
		var tooLarge *http.MaxBytesError
		if errors.As(err, &tooLarge) {
			s.writeError(w, r, http.StatusRequestEntityTooLarge, "BODY_TOO_LARGE", "Corpo da requisição excede o limite", nil)
			return
		}
		s.writeError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "bitrix_deal_id deve ser um inteiro positivo", nil)
		return
	}
	if body.BitrixDealID <= 0 || decoder.Decode(&struct{}{}) != io.EOF {
		s.writeError(w, r, http.StatusBadRequest, "INVALID_REQUEST", "bitrix_deal_id deve ser um inteiro positivo", nil)
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), s.auditTimeout)
	defer cancel()
	execution, err := s.runner.Run(ctx, body.BitrixDealID)
	if err != nil {
		s.handleInternal(w, r, "executar auditoria", err)
		return
	}
	audit, err := s.repo.GetAudit(ctx, execution.AssessmentID)
	if err != nil {
		s.handleInternal(w, r, "consultar auditoria criada", err)
		return
	}
	writeJSON(w, http.StatusCreated, envelope[auditDTO]{Data: toAuditDTO(audit)})
}

func (s *Server) auditSubresource(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/audits/")
	if strings.HasSuffix(path, "/report.pdf") {
		idText := strings.TrimSuffix(path, "/report.pdf")
		id, ok := positiveID(idText)
		if !ok {
			s.invalidID(w, r)
			return
		}
		s.reportPDF(w, r, id)
		return
	}
	id, ok := positiveID(path)
	if !ok {
		s.invalidID(w, r)
		return
	}
	audit, err := s.repo.GetAudit(r.Context(), id)
	if errors.Is(err, apidomain.ErrNotFound) {
		s.writeError(w, r, http.StatusNotFound, "AUDIT_NOT_FOUND", "Auditoria não encontrada", nil)
		return
	}
	if err != nil {
		s.handleInternal(w, r, "consultar auditoria", err)
		return
	}
	writeJSON(w, http.StatusOK, envelope[auditDTO]{Data: toAuditDTO(audit)})
}

func (s *Server) listAudits(w http.ResponseWriter, r *http.Request) {
	page, pageSize, err := parsePagination(r)
	if err != nil {
		s.invalid(w, r, err.Error())
		return
	}
	q := r.URL.Query()
	dealID, err := optionalPositive(q.Get("bitrix_deal_id"))
	if err != nil {
		s.invalid(w, r, "bitrix_deal_id inválido")
		return
	}
	status := q.Get("status")
	if status != "" && !oneOf(status, "PENDING", "PROCESSING", "COMPLETED", "FAILED") {
		s.invalid(w, r, "status inválido")
		return
	}
	final := q.Get("final_result")
	if final != "" && !oneOf(final, "venda", "não venda", "em negociação", "pós-venda/suporte", "indeterminado") {
		s.invalid(w, r, "final_result inválido")
		return
	}
	from, err := optionalDate(q.Get("date_from"), false)
	if err != nil {
		s.invalid(w, r, "date_from inválida")
		return
	}
	to, err := optionalDate(q.Get("date_to"), true)
	if err != nil {
		s.invalid(w, r, "date_to inválida")
		return
	}
	result, err := s.repo.ListAudits(r.Context(), apidomain.AuditFilter{Page: page, PageSize: pageSize, BitrixDealID: dealID, Status: status, FinalResult: final, DateFrom: from, DateTo: to})
	if err != nil {
		s.handleInternal(w, r, "listar auditorias", err)
		return
	}
	items := make([]auditDTO, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, toAuditDTO(item))
	}
	writeJSON(w, http.StatusOK, listEnvelope[auditDTO]{Data: items, Pagination: pagination(result.Page, result.PageSize, result.Total)})
}

func (s *Server) listFindings(w http.ResponseWriter, r *http.Request) {
	page, pageSize, err := parsePagination(r)
	if err != nil {
		s.invalid(w, r, err.Error())
		return
	}
	q := r.URL.Query()
	assessmentID, err := optionalPositive(q.Get("assessment_id"))
	if err != nil {
		s.invalid(w, r, "assessment_id inválido")
		return
	}
	dealID, err := optionalPositive(q.Get("bitrix_deal_id"))
	if err != nil {
		s.invalid(w, r, "bitrix_deal_id inválido")
		return
	}
	severity := strings.ToUpper(q.Get("severity"))
	if severity != "" && !oneOf(severity, string(auditdomain.SeverityLow), string(auditdomain.SeverityMedium), string(auditdomain.SeverityHigh), string(auditdomain.SeverityCritical)) {
		s.invalid(w, r, "severity inválida")
		return
	}
	result, err := s.repo.ListFindings(r.Context(), apidomain.FindingFilter{Page: page, PageSize: pageSize, AssessmentID: assessmentID, BitrixDealID: dealID, Severity: severity, Rule: q.Get("rule")})
	if err != nil {
		s.handleInternal(w, r, "listar achados", err)
		return
	}
	items := make([]findingDTO, 0, len(result.Items))
	for _, item := range result.Items {
		items = append(items, toFindingDTO(item))
	}
	writeJSON(w, http.StatusOK, listEnvelope[findingDTO]{Data: items, Pagination: pagination(result.Page, result.PageSize, result.Total)})
}

func (s *Server) dealAnalysis(w http.ResponseWriter, r *http.Request) {
	path := strings.TrimPrefix(r.URL.Path, "/api/v1/deals/")
	raw := strings.HasSuffix(path, "/analysis/raw")
	suffix := "/analysis"
	if raw {
		suffix = "/analysis/raw"
	}
	if !strings.HasSuffix(path, suffix) {
		s.writeError(w, r, http.StatusNotFound, "DEAL_NOT_FOUND", "Negócio não encontrado", nil)
		return
	}
	dealID, ok := positiveID(strings.TrimSuffix(path, suffix))
	if !ok {
		s.invalid(w, r, "bitrix_deal_id inválido")
		return
	}
	assessmentID, err := optionalPositive(r.URL.Query().Get("assessment_id"))
	if err != nil {
		s.invalid(w, r, "assessment_id inválido")
		return
	}
	analysis, err := s.repo.GetAnalysis(r.Context(), dealID, assessmentID)
	if errors.Is(err, apidomain.ErrNotFound) {
		s.writeError(w, r, http.StatusNotFound, "DEAL_NOT_FOUND", "Análise não encontrada para o negócio", nil)
		return
	}
	if err != nil {
		s.handleInternal(w, r, "consultar análise", err)
		return
	}
	writeJSON(w, http.StatusOK, envelope[analysisDTO]{Data: toAnalysisDTO(analysis, raw)})
}

func (s *Server) reportPDF(w http.ResponseWriter, r *http.Request, id int64) {
	data, err := s.repo.GetReportData(r.Context(), id)
	if errors.Is(err, apidomain.ErrNotFound) {
		s.writeError(w, r, http.StatusNotFound, "AUDIT_NOT_FOUND", "Auditoria não encontrada", nil)
		return
	}
	if err != nil {
		s.handleInternal(w, r, "consultar dados do relatório", err)
		return
	}
	if s.report == nil {
		s.writeError(w, r, http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Gerador de relatório indisponível", nil)
		return
	}
	summary := apiapp.ExecutiveSummary(data)
	content, err := s.report.Render(r.Context(), data, summary)
	if err != nil {
		s.handleInternal(w, r, "gerar relatório", err)
		return
	}
	w.Header().Set("Content-Type", "application/pdf")
	w.Header().Set("Content-Disposition", fmt.Sprintf(`attachment; filename="auditoria-%d.pdf"`, id))
	w.WriteHeader(http.StatusOK)
	_, _ = w.Write(content)
}

func writeJSON(w http.ResponseWriter, status int, value any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(value)
}
func (s *Server) writeError(w http.ResponseWriter, r *http.Request, status int, code, message string, technical error) {
	if technical != nil {
		s.logger.Printf("API %s %s: %s", r.Method, r.URL.Path, redactTechnical(technical.Error()))
	}
	writeJSON(w, status, errorEnvelope{Error: errorDTO{Code: code, Message: message}})
}

func redactTechnical(message string) string {
	return security.Text(message)
}
func (s *Server) handleInternal(w http.ResponseWriter, r *http.Request, operation string, err error) {
	status, code, message := http.StatusInternalServerError, "INTERNAL_ERROR", "Erro interno"
	lower := strings.ToLower(err.Error())
	if strings.Contains(lower, "negócio não encontrado") || strings.Contains(lower, "deal not found") {
		status, code, message = http.StatusNotFound, "DEAL_NOT_FOUND", "Negócio não encontrado"
	} else if errors.Is(err, context.DeadlineExceeded) || strings.Contains(lower, "timeout") {
		status, code, message = http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Serviço temporariamente indisponível"
	} else if strings.Contains(lower, "bitrix") || strings.Contains(lower, "ollama") || strings.Contains(lower, "analisar negócio") {
		status, code, message = http.StatusBadGateway, "EXTERNAL_SERVICE_ERROR", "Falha em serviço externo"
	} else if strings.Contains(lower, "postgresql indisponível") || strings.Contains(lower, "conexão") {
		status, code, message = http.StatusServiceUnavailable, "SERVICE_UNAVAILABLE", "Serviço temporariamente indisponível"
	}
	s.writeError(w, r, status, code, message, fmt.Errorf("%s: %w", operation, err))
}
func (s *Server) invalid(w http.ResponseWriter, r *http.Request, message string) {
	s.writeError(w, r, http.StatusBadRequest, "INVALID_REQUEST", message, nil)
}
func (s *Server) invalidID(w http.ResponseWriter, r *http.Request) {
	s.invalid(w, r, "assessment_id inválido")
}
func parsePagination(r *http.Request) (int, int, error) {
	page := 1
	size := 20
	var err error
	if value := r.URL.Query().Get("page"); value != "" {
		page, err = strconv.Atoi(value)
		if err != nil || page < 1 {
			return 0, 0, fmt.Errorf("page deve ser maior que zero")
		}
	}
	if value := r.URL.Query().Get("page_size"); value != "" {
		size, err = strconv.Atoi(value)
		if err != nil || size < 1 || size > 100 {
			return 0, 0, fmt.Errorf("page_size deve estar entre 1 e 100")
		}
	}
	return page, size, nil
}
func positiveID(value string) (int64, bool) {
	id, err := strconv.ParseInt(value, 10, 64)
	return id, err == nil && id > 0
}
func optionalPositive(value string) (int64, error) {
	if value == "" {
		return 0, nil
	}
	id, ok := positiveID(value)
	if !ok {
		return 0, fmt.Errorf("ID inválido")
	}
	return id, nil
}
func optionalDate(value string, end bool) (*time.Time, error) {
	if value == "" {
		return nil, nil
	}
	parsed, err := time.Parse("2006-01-02", value)
	if err != nil {
		parsed, err = time.Parse(time.RFC3339, value)
	}
	if err != nil {
		return nil, err
	}
	if end && len(value) == 10 {
		parsed = parsed.Add(24*time.Hour - time.Nanosecond)
	}
	return &parsed, nil
}
func pagination(page, size, total int) paginationDTO {
	pages := 0
	if total > 0 {
		pages = (total + size - 1) / size
	}
	return paginationDTO{page, size, total, pages}
}
func oneOf(value string, values ...string) bool {
	for _, candidate := range values {
		if value == candidate {
			return true
		}
	}
	return false
}
