package httpapi

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	apiapp "github.com/portfolio/auditor-ia/internal/api/application"
	apidomain "github.com/portfolio/auditor-ia/internal/api/domain"
	batchdomain "github.com/portfolio/auditor-ia/internal/batch/domain"
	business "github.com/portfolio/auditor-ia/internal/business/domain"
	conversationdomain "github.com/portfolio/auditor-ia/internal/conversation/domain"
	statsdomain "github.com/portfolio/auditor-ia/internal/crmstats/domain"
)

type fakeRepo struct {
	audit                            apidomain.Audit
	audits                           apidomain.Page[apidomain.Audit]
	findings                         apidomain.Page[apidomain.Finding]
	analysis                         apidomain.Analysis
	report                           apidomain.ReportData
	err                              error
	auditFilter                      apidomain.AuditFilter
	findingFilter                    apidomain.FindingFilter
	analysisDeal, analysisAssessment int64
}
type fakeBusiness struct {
	dashboard business.Dashboard
	filters   business.GlobalFilters
	err       error
}
type fakeBatchQueue struct {
	batch    batchdomain.Batch
	ids      []int64
	canceled int64
	err      error
}

func (f *fakeBatchQueue) Create(_ context.Context, ids []int64) (batchdomain.Batch, error) {
	f.ids = ids
	return f.batch, f.err
}
func (f *fakeBatchQueue) Get(context.Context, int64) (batchdomain.Batch, error) {
	return f.batch, f.err
}
func (f *fakeBatchQueue) Cancel(_ context.Context, id int64) error { f.canceled = id; return f.err }
func (f *fakeBatchQueue) Claim(context.Context, string, time.Duration) (*batchdomain.Item, error) {
	return nil, nil
}
func (f *fakeBatchQueue) Complete(context.Context, batchdomain.Item, int64) error { return nil }
func (f *fakeBatchQueue) Reschedule(context.Context, batchdomain.Item, time.Time, string, string) error {
	return nil
}
func (f *fakeBatchQueue) Fail(context.Context, batchdomain.Item, string, string) error { return nil }
func (f *fakeBatchQueue) RecoverExpired(context.Context) (int64, error)                { return 0, nil }

func TestBatchEndpointsCreateProgressAndCancel(t *testing.T) {
	queue := &fakeBatchQueue{batch: batchdomain.Batch{ID: 42, Status: batchdomain.Pending, Total: 2, Pending: 2}}
	server, err := New(Config{APIKey: "secret", Logger: &bufferLogger{}, Batches: queue}, &fakeRepo{}, &fakeRunner{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	created := request(t, server, http.MethodPost, "/api/v1/audit-batches", `{"bitrix_deal_ids":[35318,34700,35318]}`, "secret")
	if created.Code != http.StatusAccepted || len(queue.ids) != 2 || !strings.Contains(created.Body.String(), `"pending":2`) {
		t.Fatalf("create=%d %s ids=%v", created.Code, created.Body.String(), queue.ids)
	}
	progress := request(t, server, http.MethodGet, "/api/v1/audit-batches/42", "", "secret")
	if progress.Code != 200 || !strings.Contains(progress.Body.String(), `"id":42`) {
		t.Fatalf("get=%d %s", progress.Code, progress.Body.String())
	}
	canceled := request(t, server, http.MethodPost, "/api/v1/audit-batches/42/cancel", "", "secret")
	if canceled.Code != 200 || queue.canceled != 42 {
		t.Fatalf("cancel=%d id=%d", canceled.Code, queue.canceled)
	}
}
func TestBatchEndpointRejectsInvalidOrHugeInput(t *testing.T) {
	server, _ := New(Config{APIKey: "secret", Logger: &bufferLogger{}, Batches: &fakeBatchQueue{}}, &fakeRepo{}, &fakeRunner{}, nil)
	for _, body := range []string{`{}`, `{"bitrix_deal_ids":[0]}`, `invalid`} {
		if got := request(t, server, http.MethodPost, "/api/v1/audit-batches", body, "secret"); got.Code != 400 {
			t.Fatalf("body=%q status=%d", body, got.Code)
		}
	}
}

func (f *fakeBusiness) Load(_ context.Context, filters business.GlobalFilters) (business.Dashboard, error) {
	f.filters = filters
	return f.dashboard, f.err
}

func TestBusinessDashboardEndpointRequiresAuthAndOmitsRawResponse(t *testing.T) {
	loader := &fakeBusiness{dashboard: business.Dashboard{Executive: business.ExecutiveSummary{DealsEvaluated: 61}}}
	server, err := New(Config{APIKey: "secret", AuditTimeout: time.Second, Logger: &bufferLogger{}, Business: loader}, &fakeRepo{}, &fakeRunner{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := request(t, server, http.MethodGet, "/api/v1/dashboard/executive?period=last_30_days", "", ""); got.Code != http.StatusUnauthorized {
		t.Fatalf("sem autenticação=%d", got.Code)
	}
	got := request(t, server, http.MethodGet, "/api/v1/dashboard/executive?period=last_30_days&only_without_value=true", "", "secret")
	if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), `"DealsEvaluated":61`) {
		t.Fatalf("resposta=%d %s", got.Code, got.Body.String())
	}
	if strings.Contains(strings.ToLower(got.Body.String()), "raw_response") {
		t.Fatalf("raw_response exposta: %s", got.Body.String())
	}
	if loader.filters.Period != business.Period30Days || !loader.filters.OnlyWithoutValue {
		t.Fatalf("filtro não aplicado: %#v", loader.filters)
	}
}

func TestCRMStatisticsEndpointRequiresAPIKey(t *testing.T) {
	loader := &fakeBusiness{dashboard: business.Dashboard{CRMStatistics: statsdomain.Statistics{Available: true, TotalDeals: 30000, WonDeals: 4100, LostDeals: 9200, OutcomesByMonth: []statsdomain.MonthlyOutcome{{Month: "2026-08", Won: 120, Lost: 80}}, Marker3264Available: false, MarkerUnavailable: "Indisponível"}}}
	server, err := New(Config{APIKey: "secret", Logger: &bufferLogger{}, Business: loader}, &fakeRepo{}, &fakeRunner{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got := request(t, server, http.MethodGet, "/api/v1/dashboard/crm-statistics", "", ""); got.Code != http.StatusUnauthorized {
		t.Fatalf("sem chave=%d", got.Code)
	}
	got := request(t, server, http.MethodGet, "/api/v1/dashboard/crm-statistics", "", "secret")
	if got.Code != http.StatusOK || !strings.Contains(got.Body.String(), `"total_deals":30000`) || !strings.Contains(got.Body.String(), `"won_deals":4100`) || !strings.Contains(got.Body.String(), `"lost_deals":9200`) || !strings.Contains(got.Body.String(), `"outcomes_by_month":[{"month":"2026-08","won":120,"lost":80}]`) || !strings.Contains(got.Body.String(), `"marker_3264_available":false`) {
		t.Fatalf("resposta=%d %s", got.Code, got.Body.String())
	}
}

func TestAnalysisDTOExposesAccessDeniedWithoutServerError(t *testing.T) {
	dto := toAnalysisDTO(apidomain.Analysis{
		Status: "COMPLETED", ConversationStatus: "ACCESS_DENIED",
		UnavailableReason: "Conversa localizada, mas o Bitrix negou acesso ao histórico.",
	}, false)
	if dto.Status != "COMPLETED" || dto.AnalysisAvailable || dto.ConversationStatus != "ACCESS_DENIED" || dto.AnalysisStatus == "" {
		t.Fatalf("DTO ACCESS_DENIED incorreto: %#v", dto)
	}
}

func TestInvalidOllamaResponseIsNotExposedByHTTPOrLogs(t *testing.T) {
	const raw = "conteúdo privado retornado pelo modelo"
	logger := &bufferLogger{}
	runner := &fakeRunner{err: &conversationdomain.InvalidAnalysisResponseError{
		RawResponse: raw,
		Cause:       errors.New("resultado provável inválido"),
		Category:    "invalid_probable_result",
	}}
	server, err := New(Config{APIKey: "secret", AuditTimeout: time.Second, Logger: logger}, &fakeRepo{}, runner, nil)
	if err != nil {
		t.Fatal(err)
	}
	response := request(t, server, http.MethodPost, "/api/v1/audits", `{"bitrix_deal_id":8620}`, "secret")
	if response.Code != http.StatusBadGateway {
		t.Fatalf("status = %d: %s", response.Code, response.Body.String())
	}
	if strings.Contains(response.Body.String(), raw) || strings.Contains(logger.value.String(), raw) {
		t.Fatalf("raw_response exposta: body=%s log=%s", response.Body.String(), logger.value.String())
	}
}

func (f *fakeRepo) GetAudit(context.Context, int64) (apidomain.Audit, error) { return f.audit, f.err }
func (f *fakeRepo) ListAudits(_ context.Context, filter apidomain.AuditFilter) (apidomain.Page[apidomain.Audit], error) {
	f.auditFilter = filter
	return f.audits, f.err
}
func (f *fakeRepo) ListFindings(_ context.Context, filter apidomain.FindingFilter) (apidomain.Page[apidomain.Finding], error) {
	f.findingFilter = filter
	return f.findings, f.err
}
func (f *fakeRepo) GetAnalysis(_ context.Context, deal, assessment int64) (apidomain.Analysis, error) {
	f.analysisDeal, f.analysisAssessment = deal, assessment
	return f.analysis, f.err
}
func (f *fakeRepo) GetReportData(context.Context, int64) (apidomain.ReportData, error) {
	return f.report, f.err
}

type fakeRunner struct {
	assessmentID int64
	err          error
	dealID       int64
}

func (f *fakeRunner) Run(_ context.Context, id int64) (apidomain.AuditExecution, error) {
	f.dealID = id
	return apidomain.AuditExecution{AssessmentID: f.assessmentID}, f.err
}

type fakeReport struct {
	content []byte
	err     error
	summary string
}

func (f *fakeReport) Render(_ context.Context, _ apidomain.ReportData, summary string) ([]byte, error) {
	f.summary = summary
	return f.content, f.err
}

type bufferLogger struct{ value strings.Builder }

func (l *bufferLogger) Printf(format string, args ...any) {
	l.value.WriteString(fmt.Sprintf(format, args...))
}

func testServer(t *testing.T, repo *fakeRepo, runner *fakeRunner, renderer apidomain.ReportRenderer) *Server {
	t.Helper()
	server, err := New(Config{APIKey: "secret", AuditTimeout: time.Second, Logger: &bufferLogger{}}, repo, runner, renderer)
	if err != nil {
		t.Fatal(err)
	}
	return server
}
func request(t *testing.T, server *Server, method, path, body, key string) *httptest.ResponseRecorder {
	t.Helper()
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if key != "" {
		req.Header.Set("X-API-Key", key)
	}
	if body != "" {
		req.Header.Set("Content-Type", "application/json")
	}
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, req)
	return rec
}

func TestSecurityHeadersBodyLimitRateLimitAndCORS(t *testing.T) {
	server, err := New(Config{APIKey: "secret", AuditTimeout: time.Second, Logger: &bufferLogger{}, MaxBodyBytes: 32, RateLimitRPS: .1, RateLimitBurst: 1, AllowedOrigins: []string{"https://allowed.example"}}, &fakeRepo{}, &fakeRunner{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	allowed := httptest.NewRequest("GET", "/health", nil)
	allowed.Header.Set("Origin", "https://allowed.example")
	rec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rec, allowed)
	for _, header := range []string{"X-Content-Type-Options", "Cache-Control", "Referrer-Policy", "X-Frame-Options", "X-Request-ID"} {
		if rec.Header().Get(header) == "" {
			t.Fatalf("header %s ausente", header)
		}
	}
	if rec.Header().Get("Access-Control-Allow-Origin") != "https://allowed.example" {
		t.Fatal("CORS permitido não retornado")
	}
	blocked := httptest.NewRequest("GET", "/health", nil)
	blocked.Header.Set("Origin", "https://evil.example")
	blockedRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(blockedRec, blocked)
	if blockedRec.Code != http.StatusForbidden {
		t.Fatalf("CORS: %d", blockedRec.Code)
	}
	rateRec := httptest.NewRecorder()
	server.Handler().ServeHTTP(rateRec, httptest.NewRequest("GET", "/health", nil))
	if rateRec.Code != http.StatusTooManyRequests {
		t.Fatalf("rate limit: %d", rateRec.Code)
	}
	bodyServer := testServer(t, &fakeRepo{}, &fakeRunner{}, nil)
	bodyServer.maxBodyBytes = 8
	tooLarge := request(t, bodyServer, "POST", "/api/v1/audits", `{"bitrix_deal_id":8620}`, "secret")
	if tooLarge.Code != http.StatusRequestEntityTooLarge {
		t.Fatalf("body limit: %d %s", tooLarge.Code, tooLarge.Body.String())
	}
	wrong := httptest.NewRequest("POST", "/api/v1/audits", strings.NewReader(`{}`))
	wrong.Header.Set("X-API-Key", "secret")
	wrong.Header.Set("Content-Type", "text/plain")
	wrongRec := httptest.NewRecorder()
	bodyServer.Handler().ServeHTTP(wrongRec, wrong)
	if wrongRec.Code != http.StatusUnsupportedMediaType {
		t.Fatalf("content-type: %d", wrongRec.Code)
	}
}
func sampleAudit() apidomain.Audit {
	score := 80.0
	finished := time.Date(2026, 8, 10, 12, 1, 0, 0, time.UTC)
	return apidomain.Audit{AssessmentID: 10, BitrixDealID: 8620, DealTitle: "Lead", Status: "COMPLETED", Score: &score, FinalResult: "venda", ResultSource: "Bitrix", FindingsCount: 2, StartedAt: finished.Add(-time.Minute), FinishedAt: &finished, CreatedAt: finished.Add(-time.Minute)}
}

func TestHealthAndAPIKey(t *testing.T) {
	if _, err := New(Config{}, &fakeRepo{}, &fakeRunner{}, nil); err == nil {
		t.Fatal("servidor iniciou sem AUDITOR_API_KEY")
	}
	server := testServer(t, &fakeRepo{}, &fakeRunner{}, nil)
	health := request(t, server, "GET", "/health", "", "")
	if health.Code != 200 || health.Header().Get("Content-Type") != "application/json" || !strings.Contains(health.Body.String(), `"data":{"status":"ok"}`) {
		t.Fatalf("health inválido: %d %s", health.Code, health.Body.String())
	}
	for _, key := range []string{"", "wrong"} {
		response := request(t, server, "GET", "/api/v1/audits", "", key)
		if response.Code != 401 || !strings.Contains(response.Body.String(), `"code":"UNAUTHORIZED"`) {
			t.Fatalf("autenticação inválida: %d %s", response.Code, response.Body.String())
		}
	}
}

func TestExternalAndUnavailableErrorsAreMapped(t *testing.T) {
	repo := &fakeRepo{audit: sampleAudit()}
	runner := &fakeRunner{err: errors.New("Ollama indisponível")}
	server := testServer(t, repo, runner, nil)
	response := request(t, server, "POST", "/api/v1/audits", `{"bitrix_deal_id":8620}`, "secret")
	if response.Code != http.StatusBadGateway || !strings.Contains(response.Body.String(), `"code":"EXTERNAL_SERVICE_ERROR"`) {
		t.Fatalf("erro externo inválido: %d %s", response.Code, response.Body.String())
	}
	runner.err = context.DeadlineExceeded
	response = request(t, server, "POST", "/api/v1/audits", `{"bitrix_deal_id":8620}`, "secret")
	if response.Code != http.StatusServiceUnavailable {
		t.Fatalf("timeout inválido: %d %s", response.Code, response.Body.String())
	}
}

func TestScoreIsNumericAndSingleResourceEnvelope(t *testing.T) {
	repo := &fakeRepo{audit: sampleAudit()}
	server := testServer(t, repo, &fakeRunner{}, nil)
	response := request(t, server, "GET", "/api/v1/audits/10", "", "secret")
	if response.Code != 200 {
		t.Fatal(response.Body.String())
	}
	var decoded struct {
		Data map[string]any `json:"data"`
	}
	if err := json.Unmarshal(response.Body.Bytes(), &decoded); err != nil {
		t.Fatal(err)
	}
	score, ok := decoded.Data["score"].(float64)
	if !ok || score != 80 {
		t.Fatalf("score não numérico: %#v", decoded.Data["score"])
	}
	if _, exists := decoded.Data["assessment_id"]; !exists {
		t.Fatal("envelope/DTO inválido")
	}
}

func TestStandardErrorAndNotFound(t *testing.T) {
	repo := &fakeRepo{err: apidomain.ErrNotFound}
	server := testServer(t, repo, &fakeRunner{}, nil)
	response := request(t, server, "GET", "/api/v1/audits/99", "", "secret")
	if response.Code != 404 || response.Header().Get("Content-Type") != "application/json" || !strings.Contains(response.Body.String(), `"code":"AUDIT_NOT_FOUND"`) {
		t.Fatalf("erro inválido: %d %s", response.Code, response.Body.String())
	}
	invalid := request(t, server, "GET", "/api/v1/audits/nope", "", "secret")
	if invalid.Code != 400 {
		t.Fatalf("ID inválido=%d", invalid.Code)
	}
}

func TestCreateAuditValidationAndSuccess(t *testing.T) {
	repo := &fakeRepo{audit: sampleAudit()}
	runner := &fakeRunner{assessmentID: 10}
	server := testServer(t, repo, runner, nil)
	for _, body := range []string{`{}`, `{"bitrix_deal_id":0}`, `{"bitrix_deal_id":"x"}`, `{broken`} {
		response := request(t, server, "POST", "/api/v1/audits", body, "secret")
		if response.Code != 400 {
			t.Fatalf("body %q retornou %d", body, response.Code)
		}
	}
	response := request(t, server, "POST", "/api/v1/audits", `{"bitrix_deal_id":8620}`, "secret")
	if response.Code != 201 || runner.dealID != 8620 || !strings.Contains(response.Body.String(), `"assessment_id":10`) {
		t.Fatalf("POST inválido: %d %s", response.Code, response.Body.String())
	}
}

func TestAuditHistoryPaginationLimitsAndFilters(t *testing.T) {
	repo := &fakeRepo{audits: apidomain.Page[apidomain.Audit]{Items: []apidomain.Audit{sampleAudit()}, Total: 21, Page: 2, PageSize: 10}}
	server := testServer(t, repo, &fakeRunner{}, nil)
	response := request(t, server, "GET", "/api/v1/audits?page=2&page_size=10&bitrix_deal_id=8620&status=COMPLETED&final_result=venda&date_from=2026-08-01&date_to=2026-08-10", "", "secret")
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"total_pages":3`) {
		t.Fatalf("histórico inválido: %d %s", response.Code, response.Body.String())
	}
	if repo.auditFilter.Page != 2 || repo.auditFilter.BitrixDealID != 8620 || repo.auditFilter.Status != "COMPLETED" || repo.auditFilter.DateFrom == nil {
		t.Fatalf("filtros não aplicados: %#v", repo.auditFilter)
	}
	for _, path := range []string{"/api/v1/audits?page=0", "/api/v1/audits?page_size=101"} {
		if got := request(t, server, "GET", path, "", "secret").Code; got != 400 {
			t.Fatalf("limite inválido retornou %d", got)
		}
	}
}

func TestFindingsPaginationAndFilters(t *testing.T) {
	finding := apidomain.Finding{ID: 1, AssessmentID: 10, BitrixDealID: 8620, Rule: "rule", EntityType: "deal", EntityID: "8620", Severity: "HIGH", CreatedAt: time.Now()}
	repo := &fakeRepo{findings: apidomain.Page[apidomain.Finding]{Items: []apidomain.Finding{finding}, Total: 1, Page: 1, PageSize: 20}}
	server := testServer(t, repo, &fakeRunner{}, nil)
	response := request(t, server, "GET", "/api/v1/findings?assessment_id=10&bitrix_deal_id=8620&severity=high&rule=rule", "", "secret")
	if response.Code != 200 || repo.findingFilter.Severity != "HIGH" || repo.findingFilter.Rule != "rule" || !strings.Contains(response.Body.String(), `"data":[`) {
		t.Fatalf("achados inválidos: %d %#v %s", response.Code, repo.findingFilter, response.Body.String())
	}
	if request(t, server, "GET", "/api/v1/findings?severity=urgent", "", "secret").Code != 400 {
		t.Fatal("severidade inválida aceita")
	}
}

func TestAnalysisLatestByAssessmentAndRawProtection(t *testing.T) {
	analysis := apidomain.Analysis{ID: 3, AssessmentID: 10, BitrixDealID: 8620, Status: "COMPLETED", RawResponse: "segredo bruto", CreatedAt: time.Now()}
	repo := &fakeRepo{analysis: analysis}
	server := testServer(t, repo, &fakeRunner{}, nil)
	latest := request(t, server, "GET", "/api/v1/deals/8620/analysis", "", "secret")
	if latest.Code != 200 || repo.analysisAssessment != 0 || strings.Contains(latest.Body.String(), "segredo bruto") {
		t.Fatalf("análise recente inválida: %s", latest.Body.String())
	}
	selected := request(t, server, "GET", "/api/v1/deals/8620/analysis?assessment_id=10", "", "secret")
	if selected.Code != 200 || repo.analysisAssessment != 10 {
		t.Fatal("assessment_id não aplicado")
	}
	if request(t, server, "GET", "/api/v1/deals/8620/analysis/raw", "", "").Code != 401 {
		t.Fatal("raw sem proteção")
	}
	raw := request(t, server, "GET", "/api/v1/deals/8620/analysis/raw", "", "secret")
	if raw.Code != 200 || !strings.Contains(raw.Body.String(), "segredo bruto") {
		t.Fatal("raw protegido não retornado")
	}
}

func TestAnalysisReportsConversationUnavailable(t *testing.T) {
	reason := "Análise de conversa indisponível por ausência de mensagens IMOPENLINES e formulário CRM."
	repo := &fakeRepo{analysis: apidomain.Analysis{ID: 3, AssessmentID: 10, BitrixDealID: 8620, Status: "COMPLETED", UnavailableReason: reason, CreatedAt: time.Now()}}
	server := testServer(t, repo, &fakeRunner{}, nil)
	response := request(t, server, "GET", "/api/v1/deals/8620/analysis", "", "secret")
	if response.Code != 200 || !strings.Contains(response.Body.String(), `"analysis_available":false`) || !strings.Contains(response.Body.String(), reason) {
		t.Fatalf("indisponibilidade não exposta pela API: %s", response.Body.String())
	}
}

func TestPDFResponseAndDeterministicSummary(t *testing.T) {
	data := apidomain.ReportData{Audit: sampleAudit(), Findings: []apidomain.Finding{{Severity: "HIGH"}, {Severity: "LOW"}}}
	renderer := &fakeReport{content: []byte("%PDF-1.4 test")}
	repo := &fakeRepo{report: data}
	server := testServer(t, repo, &fakeRunner{}, renderer)
	response := request(t, server, "GET", "/api/v1/audits/10/report.pdf", "", "secret")
	if response.Code != 200 || response.Header().Get("Content-Type") != "application/pdf" || response.Header().Get("Content-Disposition") != `attachment; filename="auditoria-10.pdf"` || !bytes.HasPrefix(response.Body.Bytes(), []byte("%PDF")) {
		t.Fatalf("PDF inválido: %d %#v", response.Code, response.Header())
	}
	first := apiapp.ExecutiveSummary(data)
	second := apiapp.ExecutiveSummary(data)
	if first != second || !strings.Contains(first, "1 altos") || renderer.summary != first {
		t.Fatalf("resumo não determinístico: %q", first)
	}
	repo.err = apidomain.ErrNotFound
	if request(t, server, "GET", "/api/v1/audits/99/report.pdf", "", "secret").Code != 404 {
		t.Fatal("PDF inexistente não retornou 404")
	}
}

func TestInternalErrorDoesNotLeakSecrets(t *testing.T) {
	logger := &bufferLogger{}
	repo := &fakeRepo{err: errors.New("postgres failed DATABASE_URL=postgres://user:password@host/db")}
	server, err := New(Config{APIKey: "secret", Logger: logger}, repo, &fakeRunner{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	response := request(t, server, "GET", "/api/v1/audits/10", "", "secret")
	if response.Code != 500 || strings.Contains(response.Body.String(), "password") || strings.Contains(response.Body.String(), "postgres") {
		t.Fatalf("segredo vazou: %s", response.Body.String())
	}
	if strings.Contains(logger.value.String(), "password") {
		t.Fatalf("segredo vazou no log: %s", logger.value.String())
	}
}
