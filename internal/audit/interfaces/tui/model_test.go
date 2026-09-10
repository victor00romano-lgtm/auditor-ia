package tui

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/portfolio/auditor-ia/internal/audit/application"
	"github.com/portfolio/auditor-ia/internal/audit/domain"
	business "github.com/portfolio/auditor-ia/internal/business/domain"
	statsdomain "github.com/portfolio/auditor-ia/internal/crmstats/domain"
	dashboard "github.com/portfolio/auditor-ia/internal/dashboard/domain"
)

type fakeBusinessLoader struct {
	dashboard business.Dashboard
	err       error
}

type fakeCRMStatsSyncer struct {
	err   error
	calls int
}

func (f *fakeCRMStatsSyncer) Sync(ctx context.Context, progress func(statsdomain.Progress)) error {
	f.calls++
	progress(statsdomain.Progress{Phase: "deals", DealsCollected: 100})
	progress(statsdomain.Progress{Phase: "activities", DealsCollected: 100, ActivitiesCollected: 50, Markers3264Discovered: 12})
	return f.err
}

type fakeBusinessCSV struct{ path string }

type fakeBusinessPDF struct {
	path    string
	err     error
	filters business.GlobalFilters
	calls   int
}

func (f *fakeBusinessPDF) Export(_ context.Context, filters business.GlobalFilters) (string, error) {
	f.calls++
	f.filters = filters
	return f.path, f.err
}

func (f fakeBusinessCSV) Priorities(context.Context, []business.PriorityOpportunity) (string, error) {
	return f.path, nil
}
func (f fakeBusinessCSV) Divergences(context.Context, []business.CRMAIDivergence) (string, error) {
	return f.path, nil
}
func (f fakeBusinessCSV) CRMQuality(context.Context, business.CRMQualitySummary) (string, error) {
	return f.path, nil
}
func (f fakeBusinessCSV) Objections(context.Context, []business.ObjectionSummary) (string, error) {
	return f.path, nil
}
func (f fakeBusinessCSV) Agents(context.Context, []business.AgentPerformance) (string, error) {
	return f.path, nil
}

func priorityFixture(count int) []business.PriorityOpportunity {
	items := make([]business.PriorityOpportunity, count)
	for i := range items {
		items[i] = business.PriorityOpportunity{BitrixDealID: int64(100 + i), PriorityScore: 100 - i, Title: "Negócio", AnalysisStatus: "COMPLETED"}
	}
	return items
}

func TestPriorityFourthSelectionOpensAndReturnsToFourthItem(t *testing.T) {
	repo := &fakeDashboard{detail: dashboard.DealDetail{BitrixDealID: 103}}
	m := NewBusinessDashboard(applicationRunStub(), repo, nil, fakeBusinessLoader{})
	m.screen = ScreenPriorities
	m.width = 80
	m.height = 24
	m.businessDashboard.Priorities = priorityFixture(10)
	for range 3 {
		m, _ = updateModel(t, m, key("down"))
	}
	if m.listState(ScreenPriorities).Cursor != 3 {
		t.Fatalf("cursor=%d", m.listState(ScreenPriorities).Cursor)
	}
	var cmd tea.Cmd
	m, cmd = updateModel(t, m, key("enter"))
	m = runCmd(t, m, cmd)
	if m.screen != ScreenDealDetail || repo.lastDetailID != 103 {
		t.Fatalf("abriu negócio incorreto: screen=%v id=%d", m.screen, repo.lastDetailID)
	}
	m, _ = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.screen != ScreenPriorities || m.listState(ScreenPriorities).Cursor != 3 || !strings.Contains(m.View(), "> [97] #103") {
		t.Fatalf("retorno não preservou seleção: %#v\n%s", m.listState(ScreenPriorities), m.View())
	}
}

func TestBusinessListNavigationAndFixedFrame(t *testing.T) {
	m := NewBusinessDashboard(applicationRunStub(), &fakeDashboard{}, nil, fakeBusinessLoader{})
	m.screen = ScreenPriorities
	m.width = 80
	m.height = 24
	m.businessDashboard.Priorities = priorityFixture(40)
	for range 30 {
		m, _ = updateModel(t, m, key("down"))
	}
	view := m.View()
	if !strings.Contains(view, "AUDITOR IA — BITRIX24") || !strings.Contains(view, "Esc voltar") || !strings.Contains(view, "> [70] #130") {
		t.Fatalf("frame/cursor ausente:\n%s", view)
	}
	m, _ = updateModel(t, m, key("home"))
	if m.listState(ScreenPriorities).Cursor != 0 {
		t.Fatal("Home falhou")
	}
	m, _ = updateModel(t, m, key("end"))
	if m.listState(ScreenPriorities).Cursor != 39 {
		t.Fatal("End falhou")
	}
}

func TestPrioritiesShowDeterministicFinalResult(t *testing.T) {
	m := NewBusinessDashboard(applicationRunStub(), &fakeDashboard{}, nil, fakeBusinessLoader{})
	m.screen = ScreenPriorities
	m.width, m.height = 110, 28
	m.businessDashboard.Priorities = []business.PriorityOpportunity{
		{BitrixDealID: 34710, PriorityScore: 80, Stage: "Em andamento", ProbableResult: "indeterminado", FinalResult: "pós-venda/suporte"},
		{BitrixDealID: 34720, PriorityScore: 70, Stage: "Perdido", ProbableResult: "em negociação", FinalResult: "não venda"},
		{BitrixDealID: 34734, PriorityScore: 60, Stage: "Perdido", ProbableResult: "venda", FinalResult: "não venda"},
	}
	view := m.View()
	if !strings.Contains(view, "34710") || !strings.Contains(view, "pós-venda/sup") || strings.Count(view, "não venda") != 2 || strings.Contains(view, "RESULTADO IA") {
		t.Fatalf("resultados comerciais incorretos:\n%s", view)
	}
}

func TestCRMStatisticsViewIsResponsiveAndScrollable(t *testing.T) {
	stats := statsdomain.Statistics{
		Available: true, TotalDeals: 30000, WonDeals: 4100, LostDeals: 9200, LastSyncAt: time.Date(2026, 8, 17, 18, 30, 0, 0, time.UTC), LastStatus: statsdomain.Completed,
		ByAssignee:          []statsdomain.AssigneeCount{{ID: 3066, Name: "Camila Rodrigues", Count: 8500}, {ID: 7, Name: "Débora Ávila", Count: 7200}, {Name: "Sem responsável", Count: 300}},
		ByMonth:             statsdomain.LastTwelveCalendarMonths(time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC), time.UTC, map[string]int{"2026-08": 932}),
		OutcomesByMonth:     statsdomain.LastTwelveMonthlyOutcomes(time.Date(2026, 8, 17, 12, 0, 0, 0, time.UTC), time.UTC, map[string]int{"2026-08": 120}, map[string]int{"2026-08": 80}),
		Marker3264Available: true, Marker3264Total: 4280, Marker3264ByAssignee: []statsdomain.MarkerAssignment{{Name: "Débora Ávila", Count: 2310, PercentageBasis: 5397}, {Name: "Solange Silva", Count: 1970, PercentageBasis: 4603}},
	}
	for _, size := range [][2]int{{80, 24}, {100, 30}, {120, 40}, {160, 50}} {
		m := NewBusinessDashboard(applicationRunStub(), &fakeDashboard{}, nil, fakeBusinessLoader{})
		m.screen = ScreenExecutive
		m.width, m.height = size[0], size[1]
		m.businessDashboard.CRMStatistics = stats
		crmView := m.viewCRMStatistics()
		if !strings.Contains(crmView, "Vendas concluídas: 4100") || !strings.Contains(crmView, "Negócios perdidos: 9200") || !strings.Contains(crmView, "RESULTADOS POR MÊS") {
			t.Fatalf("estatísticas CRM %dx%d inválidas:\n%s", size[0], size[1], crmView)
		}
		m, _ = updateModel(t, m, key("end"))
		view := m.View()
		if !strings.Contains(view, "DISTRIBUIÇÃO DO CANAL 3264") || !strings.Contains(view, "Solange Silva") || !strings.Contains(view, "Ago/2026") {
			t.Fatalf("tamanho %dx%d inválido:\n%s", size[0], size[1], view)
		}
	}
}

func TestCRMStatisticsAsyncSyncPreventsDuplicatesAndCanCancel(t *testing.T) {
	syncer := &fakeCRMStatsSyncer{}
	m := NewBusinessDashboard(applicationRunStub(), &fakeDashboard{}, nil, fakeBusinessLoader{})
	m.screen = ScreenExecutive
	m.SetCRMStatisticsSynchronizer(syncer)
	m, cmd := updateModel(t, m, key("U"))
	if cmd == nil || !m.crmSyncing {
		t.Fatal("sincronização assíncrona não iniciou")
	}
	request := m.crmSyncRequest
	m, duplicate := updateModel(t, m, key("U"))
	if duplicate != nil || m.crmSyncRequest != request {
		t.Fatal("sincronização duplicada aceita")
	}
	m, _ = updateModel(t, m, key("x"))
	if !strings.Contains(m.feedback, "Cancelamento") {
		t.Fatalf("cancelamento não sinalizado: %q", m.feedback)
	}
	m, _ = updateModel(t, m, crmSyncDoneMsg{request: request - 1})
	if !m.crmSyncing {
		t.Fatal("resposta assíncrona antiga alterou estado")
	}
}

func TestDivergenceSelectionOpensSelectedDeal(t *testing.T) {
	repo := &fakeDashboard{detail: dashboard.DealDetail{BitrixDealID: 202}}
	m := NewBusinessDashboard(applicationRunStub(), repo, nil, fakeBusinessLoader{})
	m.screen = ScreenDivergences
	m.businessDashboard.Divergences = []business.CRMAIDivergence{{BitrixDealID: 201}, {BitrixDealID: 202}}
	m, _ = updateModel(t, m, key("down"))
	m, cmd := updateModel(t, m, key("enter"))
	m = runCmd(t, m, cmd)
	if repo.lastDetailID != 202 {
		t.Fatalf("divergência abriu %d", repo.lastDetailID)
	}
}

func TestEmptyBusinessListIgnoresEnterAndDelayedResponse(t *testing.T) {
	m := NewBusinessDashboard(applicationRunStub(), &fakeDashboard{}, nil, fakeBusinessLoader{})
	m.screen = ScreenPriorities
	updated, cmd := updateModel(t, m, key("enter"))
	if cmd != nil || updated.screen != ScreenPriorities {
		t.Fatal("lista vazia abriu item")
	}
	updated.requestSequence = 9
	updated.businessTarget = ScreenPriorities
	updated, _ = updateModel(t, updated, businessMsg{request: 8, target: ScreenPriorities, dashboard: business.Dashboard{Priorities: priorityFixture(2)}})
	if len(updated.businessDashboard.Priorities) != 0 {
		t.Fatal("resposta antiga alterou dashboard")
	}
}

func TestBusinessCSVExportIsAsync(t *testing.T) {
	m := NewBusinessDashboard(applicationRunStub(), &fakeDashboard{}, nil, fakeBusinessLoader{}, fakeBusinessCSV{path: "reports/out.csv"})
	m.screen = ScreenPriorities
	m.businessDashboard.Priorities = priorityFixture(1)
	m, cmd := updateModel(t, m, key("e"))
	if cmd == nil || !m.exporting || !strings.Contains(m.View(), "Exportando") {
		t.Fatal("exportação não iniciou")
	}
	m = runCmd(t, m, cmd)
	if m.exporting || !strings.Contains(m.View(), "CSV salvo em reports/out.csv") {
		t.Fatal("feedback CSV ausente")
	}
}

func TestExecutivePDFExportIsAsyncAndUsesActiveFilters(t *testing.T) {
	pdf := &fakeBusinessPDF{path: `C:\reports\relatorio-executivo-2026-08-13.pdf`}
	m := NewBusinessDashboardWithExports(applicationRunStub(), &fakeDashboard{}, nil, fakeBusinessLoader{}, fakeBusinessCSV{}, pdf)
	m.screen = ScreenExecutive
	m.width, m.height = 100, 30
	m.businessFilters = business.GlobalFilters{Period: business.Period30Days, OnlyWithoutValue: true}
	m, cmd := updateModel(t, m, key("p"))
	if cmd == nil || !m.businessPDFGenerating || pdf.calls != 0 || !strings.Contains(m.View(), "Gerando relatório executivo") {
		t.Fatalf("exportação não iniciou de forma assíncrona: cmd=%t generating=%t calls=%d", cmd != nil, m.businessPDFGenerating, pdf.calls)
	}
	m = runCmd(t, m, cmd)
	if m.businessPDFGenerating || pdf.calls != 1 || pdf.filters != m.businessFilters || !strings.Contains(m.View(), "PDF executivo salvo em") {
		t.Fatalf("exportação executiva incorreta: calls=%d filters=%#v view=%s", pdf.calls, pdf.filters, m.View())
	}
}

func TestExecutivePDFKeyIsLimitedToExecutiveScreen(t *testing.T) {
	pdf := &fakeBusinessPDF{path: "unused"}
	m := NewBusinessDashboardWithExports(applicationRunStub(), &fakeDashboard{}, nil, fakeBusinessLoader{}, fakeBusinessCSV{}, pdf)
	m.screen = ScreenPriorities
	m, cmd := updateModel(t, m, key("p"))
	if cmd != nil || pdf.calls != 0 || m.businessPDFGenerating {
		t.Fatal("tecla p iniciou PDF executivo fora da visão executiva")
	}
}

func TestFriendlyLabelsAndBooleanText(t *testing.T) {
	if divergenceLabel(business.PaymentTextDetectorConflict) != "Texto indica pagamento, detector não confirmou anexo" || boolLabel(true) != "Sim" || boolLabel(false) != "Não" {
		t.Fatal("rótulos não amigáveis")
	}
}

func TestUTF8VisualTruncationAndSupportedWidths(t *testing.T) {
	got := truncateVisual("Cláudio — negociação", 12)
	if !strings.HasSuffix(got, "…") || strings.ContainsRune(got, '�') {
		t.Fatalf("truncamento inválido: %q", got)
	}
	m := NewBusinessDashboard(applicationRunStub(), &fakeDashboard{}, nil, fakeBusinessLoader{})
	m.screen = ScreenPriorities
	m.businessDashboard.Priorities = priorityFixture(20)
	for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 100, Height: 30}, {Width: 120, Height: 40}, {Width: 160, Height: 50}} {
		m, _ = updateModel(t, m, size)
		for _, line := range strings.Split(m.View(), "\n") {
			if lipgloss.Width(line) > size.Width {
				t.Fatalf("linha excedeu %d: %q", size.Width, line)
			}
		}
	}
}

func (f fakeBusinessLoader) Load(context.Context, business.GlobalFilters) (business.Dashboard, error) {
	return f.dashboard, f.err
}

func TestBusinessMenuAndResponsiveViews(t *testing.T) {
	data := business.Dashboard{Executive: business.ExecutiveSummary{Sample: 61, DealsEvaluated: 61, Results: map[string]int{"em negociação": 30}}, Channel3264: []business.ChannelAssignment{{Assignee: "Débora Fanny", Area: "Vendas", Deals: 35, Lost: 6, LossRate: business.Percent(6, 35)}}, LoadedAt: time.Now()}
	m := NewBusinessDashboard(applicationRunStub(), &fakeDashboard{}, nil, fakeBusinessLoader{dashboard: data})
	m, _ = updateModel(t, m, tea.WindowSizeMsg{Width: 100, Height: 30})
	next, cmd := updateModel(t, m, key("1"))
	next = runCmd(t, next, cmd)
	if next.screen != ScreenExecutive || !strings.Contains(next.View(), "VISÃO EXECUTIVA") {
		t.Fatalf("visão não abriu: screen=%v view=%s", next.screen, next.View())
	}
	if !strings.Contains(next.View(), "Canal 3264") || !strings.Contains(next.View(), "Débora Fanny") || !strings.Contains(next.View(), "17.14%") {
		t.Fatalf("distribuição 3264 ausente: %s", next.View())
	}
	for _, size := range []tea.WindowSizeMsg{{Width: 80, Height: 24}, {Width: 100, Height: 30}, {Width: 120, Height: 40}, {Width: 160, Height: 50}} {
		updated, _ := updateModel(t, next, size)
		if strings.Contains(updated.View(), "Terminal muito pequeno") {
			t.Fatalf("layout rejeitou %dx%d", size.Width, size.Height)
		}
	}
	small, _ := updateModel(t, next, tea.WindowSizeMsg{Width: 69, Height: 19})
	if !strings.Contains(small.View(), "Terminal muito pequeno") {
		t.Fatal("terminal pequeno não tratado")
	}
}

type fakeDashboard struct {
	summary                                  dashboard.Summary
	page                                     dashboard.FindingPage
	rules                                    []string
	detail                                   dashboard.DealDetail
	analysis                                 dashboard.ConversationAnalysisDetail
	err                                      error
	lastFilter                               dashboard.FindingFilter
	summaryCalls, detailCalls, analysisCalls int
	findingsCalls                            int
	lastDetailID, lastAnalysisID             int64
}

func TestAccessDeniedConversationViewExplainsLocatedHistory(t *testing.T) {
	m := Model{screen: ScreenConversationAnalysis, analysis: dashboard.ConversationAnalysisDetail{
		BitrixDealID: 52, ConversationStatus: "ACCESS_DENIED", MessageCount: 0,
		StatusMessage: "Conversa localizada, mas o Bitrix negou acesso ao histórico.",
	}}
	view := m.viewAnalysis(false)
	for _, expected := range []string{"Conversa encontrada: sim", "Mensagens analisadas: 0", "Histórico: acesso negado pelo Bitrix"} {
		if !strings.Contains(view, expected) {
			t.Fatalf("texto %q ausente da TUI: %s", expected, view)
		}
	}
}

type fakeReportExporter struct {
	path         string
	err          error
	calls        int
	assessmentID int64
}

func (f *fakeReportExporter) Export(_ context.Context, id int64) (string, error) {
	f.calls++
	f.assessmentID = id
	return f.path, f.err
}

func (f *fakeDashboard) GetSummary(context.Context) (dashboard.Summary, error) {
	f.summaryCalls++
	return f.summary, f.err
}
func (f *fakeDashboard) ListFindings(_ context.Context, filter dashboard.FindingFilter) (dashboard.FindingPage, error) {
	f.findingsCalls++
	f.lastFilter = filter
	page := f.page
	page.Offset = filter.Offset
	if page.Limit == 0 {
		page.Limit = filter.Limit
	}
	return page, f.err
}
func (f *fakeDashboard) ListRules(context.Context) ([]string, error) { return f.rules, f.err }
func (f *fakeDashboard) GetDealDetail(_ context.Context, id int64) (dashboard.DealDetail, error) {
	f.detailCalls++
	f.lastDetailID = id
	return f.detail, f.err
}
func (f *fakeDashboard) GetLatestConversationAnalysis(_ context.Context, id int64) (dashboard.ConversationAnalysisDetail, error) {
	f.analysisCalls++
	f.lastAnalysisID = id
	return f.analysis, f.err
}

func key(value string) tea.KeyMsg { return tea.KeyMsg{Type: tea.KeyRunes, Runes: []rune(value)} }
func updateModel(t *testing.T, m Model, msg tea.Msg) (Model, tea.Cmd) {
	t.Helper()
	next, cmd := m.Update(msg)
	return next.(Model), cmd
}
func runCmd(t *testing.T, m Model, cmd tea.Cmd) Model {
	t.Helper()
	if cmd == nil {
		return m
	}
	returnMessage := cmd()
	next, _ := m.Update(returnMessage)
	return next.(Model)
}

func TestMenuNavigationAndEscape(t *testing.T) {
	m := New(applicationRunStub())
	m, _ = updateModel(t, m, key("j"))
	if m.menuIndex != 1 {
		t.Fatal("menu não navegou")
	}
	m, _ = updateModel(t, m, key("B"))
	if m.screen != ScreenDealSearch || !m.search.Focused() {
		t.Fatal("busca não abriu")
	}
	m, _ = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.screen != ScreenMenu {
		t.Fatal("Esc não voltou")
	}
}

func TestFindingsFiltersPaginationAndDetails(t *testing.T) {
	repo := &fakeDashboard{rules: []string{"rule_a"}, page: dashboard.FindingPage{Items: []dashboard.Finding{{BitrixDealID: 8620}}, Total: 80, Limit: 25}, detail: dashboard.DealDetail{BitrixDealID: 8620, HasAnalysis: true}, analysis: dashboard.ConversationAnalysisDetail{BitrixDealID: 8620}}
	m := New(applicationRunStub(), repo)
	m.screen = ScreenFindings
	m.page = repo.page
	m.rules = repo.rules
	var cmd tea.Cmd
	m, cmd = updateModel(t, m, key("f"))
	m = runCmd(t, m, cmd)
	if repo.lastFilter.Severity != "CRITICAL" {
		t.Fatal("filtro de severidade não aplicado")
	}
	m, cmd = updateModel(t, m, key("F"))
	m = runCmd(t, m, cmd)
	if repo.lastFilter.Rule != "rule_a" || repo.lastFilter.Severity != "CRITICAL" {
		t.Fatal("combinação de filtros não aplicada")
	}
	m, cmd = updateModel(t, m, tea.KeyMsg{Type: tea.KeyPgDown})
	m = runCmd(t, m, cmd)
	if repo.lastFilter.Offset != 25 {
		t.Fatal("PgDown não paginou")
	}
	m, cmd = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEnd})
	m = runCmd(t, m, cmd)
	if repo.lastFilter.Offset != 75 {
		t.Fatal("End não foi ao fim")
	}
	m, cmd = updateModel(t, m, tea.KeyMsg{Type: tea.KeyHome})
	m = runCmd(t, m, cmd)
	if repo.lastFilter.Offset != 0 {
		t.Fatal("Home não voltou")
	}
	m, cmd = updateModel(t, m, key("c"))
	m = runCmd(t, m, cmd)
	if repo.lastFilter.Rule != "" || repo.lastFilter.Severity != "" {
		t.Fatal("filtros não foram limpos")
	}
	m.page = repo.page
	m, cmd = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = runCmd(t, m, cmd)
	if m.screen != ScreenDealDetail {
		t.Fatal("detalhes não abriram")
	}
	m, cmd = updateModel(t, m, key("a"))
	m = runCmd(t, m, cmd)
	if m.screen != ScreenConversationAnalysis {
		t.Fatal("análise não abriu")
	}
}

func TestSearchExistingMissingAndDatabaseError(t *testing.T) {
	repo := &fakeDashboard{detail: dashboard.DealDetail{BitrixDealID: 8620}}
	m := New(applicationRunStub(), repo)
	next, cmd := m.startSearch(false)
	m = next.(Model)
	m = runCmd(t, m, cmd)
	m.search.SetValue("8620")
	var searchCmd tea.Cmd
	m, searchCmd = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = runCmd(t, m, searchCmd)
	if m.screen != ScreenDealDetail {
		t.Fatal("negócio existente não abriu")
	}
	repo.err = dashboard.ErrNotFound
	m = New(applicationRunStub(), repo)
	next, cmd = m.startSearch(false)
	m = next.(Model)
	m.search.SetValue("999999")
	m, searchCmd = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = runCmd(t, m, searchCmd)
	if m.screen != ScreenError || m.err == nil || !strings.Contains(m.err.Error(), "não encontrado") {
		t.Fatal("negócio ausente não foi apresentado sem encerrar a TUI")
	}
	repo.err = dashboard.ErrNotFound
	m = New(applicationRunStub(), repo)
	m.screen = ScreenSummary
	m, searchCmd = updateModel(t, m, key("r"))
	m = runCmd(t, m, searchCmd)
	if m.screen != ScreenError {
		t.Fatal("erro PostgreSQL não foi mantido na TUI")
	}
	repo.err = nil
	m, searchCmd = updateModel(t, m, key("r"))
	m = runCmd(t, m, searchCmd)
	if m.screen != ScreenSummary {
		t.Fatal("retry não recuperou")
	}
}

func TestRetryDealSearchQueriesSameIDAfterNotFound(t *testing.T) {
	repo := &fakeDashboard{err: dashboard.ErrNotFound}
	m := New(applicationRunStub(), repo)
	m.width, m.height = 120, 40
	next, _ := m.startSearch(false)
	m = next.(Model)
	m.search.SetValue("8620")
	m, cmd := updateModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = runCmd(t, m, cmd)
	if m.screen != ScreenError || repo.detailCalls != 1 || repo.lastDetailID != 8620 {
		t.Fatalf("falha inicial não registrada corretamente: screen=%v calls=%d id=%d", m.screen, repo.detailCalls, repo.lastDetailID)
	}
	repo.err = nil
	repo.detail = dashboard.DealDetail{BitrixDealID: 8620, Title: "Persistido externamente"}
	m, cmd = updateModel(t, m, key("r"))
	if cmd == nil || !m.loading || m.err != nil || m.screen != ScreenError {
		t.Fatalf("retry não entrou em carregamento: cmd=%v loading=%t err=%v screen=%v", cmd != nil, m.loading, m.err, m.screen)
	}
	if !strings.Contains(m.View(), "Recarregando dados...") {
		t.Fatal("feedback de recarregamento ausente")
	}
	m = runCmd(t, m, cmd)
	if m.screen != ScreenDealDetail || m.detail.BitrixDealID != 8620 || repo.detailCalls != 2 || repo.lastDetailID != 8620 {
		t.Fatalf("retry não consultou novamente o mesmo negócio: screen=%v detail=%#v calls=%d id=%d", m.screen, m.detail, repo.detailCalls, repo.lastDetailID)
	}
	if !strings.Contains(m.View(), "Dados atualizados às ") {
		t.Fatal("horário da atualização não foi exibido")
	}
}

func TestRetryDashboardReloadsSummary(t *testing.T) {
	repo := &fakeDashboard{err: errors.New("falha temporária")}
	m := New(applicationRunStub(), repo)
	m.screen = ScreenSummary
	m, cmd := updateModel(t, m, key("r"))
	m = runCmd(t, m, cmd)
	if m.screen != ScreenError || repo.summaryCalls != 1 {
		t.Fatalf("erro do dashboard não registrado: screen=%v calls=%d", m.screen, repo.summaryCalls)
	}
	repo.err = nil
	repo.summary = dashboard.Summary{DealsSynced: 12}
	m, cmd = updateModel(t, m, key("r"))
	if cmd == nil || !m.loading {
		t.Fatal("retry do dashboard não iniciou carregamento")
	}
	m = runCmd(t, m, cmd)
	if m.screen != ScreenSummary || m.summary.DealsSynced != 12 || repo.summaryCalls != 2 {
		t.Fatalf("dashboard não foi recarregado: screen=%v summary=%#v calls=%d", m.screen, m.summary, repo.summaryCalls)
	}
}

func TestErrorEscapeReturnsAndNonRetryableOperationHidesRetry(t *testing.T) {
	m := New(applicationRunStub())
	m.screen = ScreenDealDetail
	m.detail = dashboard.DealDetail{BitrixDealID: 8620}
	next, _ := m.showError(errors.New("falha não repetível"), retryOperation{})
	m = next.(Model)
	if strings.Contains(m.View(), "[r] tentar novamente") {
		t.Fatal("ajuda exibiu retry para operação não repetível")
	}
	m, cmd := updateModel(t, m, key("r"))
	if cmd != nil || m.screen != ScreenError {
		t.Fatal("r executou operação sem retry")
	}
	m, _ = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.screen != ScreenDealDetail || m.detail.BitrixDealID != 8620 || m.err != nil {
		t.Fatalf("Esc não voltou aos detalhes preservados: %#v", m)
	}
}

func TestEscapeNavigationTable(t *testing.T) {
	tests := []struct {
		name      string
		screen    Screen
		want      Screen
		configure func(*Model)
	}{
		{name: "menu", screen: ScreenMenu, want: ScreenMenu},
		{name: "summary", screen: ScreenSummary, want: ScreenMenu},
		{name: "findings", screen: ScreenFindings, want: ScreenMenu},
		{name: "search", screen: ScreenDealSearch, want: ScreenFindings, configure: func(m *Model) { m.searchOrigin = ScreenFindings; m.search.Focus() }},
		{name: "detail", screen: ScreenDealDetail, want: ScreenFindings, configure: func(m *Model) { m.detailOrigin = ScreenFindings }},
		{name: "analysis", screen: ScreenConversationAnalysis, want: ScreenDealDetail},
		{name: "raw", screen: ScreenRawResponse, want: ScreenConversationAnalysis},
		{name: "error", screen: ScreenError, want: ScreenSummary, configure: func(m *Model) { m.errorReturn = ScreenSummary; m.err = errors.New("falha") }},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			m := New(applicationRunStub())
			m.screen = test.screen
			if test.configure != nil {
				test.configure(&m)
			}
			m, _ = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEsc})
			if m.screen != test.want || (test.screen != ScreenMenu && m.screen == test.screen) {
				t.Fatalf("Esc: tela=%v, esperada=%v", m.screen, test.want)
			}
			if m.err != nil || (m.screen != ScreenDealSearch && m.search.Focused()) {
				t.Fatal("estado residual após Esc")
			}
		})
	}
}

func TestReturningFromDetailPreservesFindingsState(t *testing.T) {
	repo := &fakeDashboard{detail: dashboard.DealDetail{BitrixDealID: 8620}}
	m := New(applicationRunStub(), repo)
	m.screen = ScreenFindings
	m.filter = dashboard.FindingFilter{Severity: "HIGH", Rule: "rule_a", Offset: 25, Limit: 25}
	m.page = dashboard.FindingPage{Items: []dashboard.Finding{{BitrixDealID: 1}, {BitrixDealID: 8620}}, Total: 50, Offset: 25, Limit: 25}
	m.selected, m.offset = 1, 7
	m.detailOrigin = ScreenFindings
	m, cmd := updateModel(t, m, tea.KeyMsg{Type: tea.KeyEnter})
	m = runCmd(t, m, cmd)
	m.offset = 3
	m, _ = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.screen != ScreenFindings || m.selected != 1 || m.offset != 7 || m.filter.Offset != 25 || m.filter.Severity != "HIGH" || m.filter.Rule != "rule_a" {
		t.Fatalf("estado da lista não preservado: %#v", m)
	}
}

func TestRetryFindingsPreservesFiltersAndPreventsConcurrentReload(t *testing.T) {
	repo := &fakeDashboard{err: errors.New("temporário")}
	m := New(applicationRunStub(), repo)
	m.screen = ScreenFindings
	m.filter = dashboard.FindingFilter{Severity: "HIGH", Rule: "stale_deal", BitrixDealID: 8260, Limit: 50, Offset: 50}
	m, cmd := updateModel(t, m, key("r"))
	m = runCmd(t, m, cmd)
	if m.screen != ScreenError || repo.lastFilter != m.filter {
		t.Fatalf("filtros não preservados no erro: got=%#v want=%#v", repo.lastFilter, m.filter)
	}
	repo.err = nil
	m, cmd = updateModel(t, m, key("r"))
	if cmd == nil || !m.loading {
		t.Fatal("retry não iniciou")
	}
	before := repo.findingsCalls
	m2, duplicate := updateModel(t, m, key("r"))
	if duplicate != nil || !m2.loading || repo.findingsCalls != before {
		t.Fatal("recarregamento simultâneo foi aceito")
	}
	m = runCmd(t, m, cmd)
	if m.screen != ScreenFindings || repo.lastFilter.Severity != "HIGH" || repo.lastFilter.Rule != "stale_deal" || repo.lastFilter.BitrixDealID != 8260 {
		t.Fatalf("retry alterou filtros: %#v", repo.lastFilter)
	}
}

func TestDelayedReadDoesNotOverwriteCurrentScreenOrDeal(t *testing.T) {
	repo := &fakeDashboard{detail: dashboard.DealDetail{BitrixDealID: 8620}}
	m := New(applicationRunStub(), repo)
	m.screen = ScreenDealSearch
	m.detailOrigin = ScreenDealSearch
	next, delayed := m.beginRead(retryDetail, 8620, 0, false)
	m = next.(Model)
	m, _ = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	m.detail = dashboard.DealDetail{BitrixDealID: 9000}
	m = runCmd(t, m, delayed)
	if m.screen != ScreenMenu || m.detail.BitrixDealID != 9000 || m.loading {
		t.Fatalf("resposta atrasada sobrescreveu estado atual: %#v", m)
	}
}

func TestHelpOnlyShowsAvailableShortcuts(t *testing.T) {
	m := NewWithReportExporter(applicationRunStub(), &fakeDashboard{}, &fakeReportExporter{})
	m.width, m.height = 120, 40
	if strings.Contains(m.View(), "Esc voltar") || strings.Contains(m.View(), "[p]") || strings.Contains(m.View(), "[a]") {
		t.Fatalf("menu anunciou atalho inválido:\n%s", m.View())
	}
	m.screen = ScreenDealDetail
	m.detail = dashboard.DealDetail{BitrixDealID: 8620}
	view := m.View()
	if strings.Contains(view, "[a]") || strings.Contains(view, "[p]") || !strings.Contains(view, "[r]") {
		t.Fatalf("detalhes sem avaliação anunciaram atalhos inválidos:\n%s", view)
	}
	m.detail = dashboard.DealDetail{BitrixDealID: 8620, AssessmentID: 44, HasAnalysis: true}
	view = m.View()
	if !strings.Contains(view, "[a]") || !strings.Contains(view, "[p]") {
		t.Fatalf("atalhos disponíveis não foram anunciados:\n%s", view)
	}
	m.screen = ScreenConversationAnalysis
	m.analysis = dashboard.ConversationAnalysisDetail{BitrixDealID: 8620}
	if strings.Contains(m.View(), "[v]") {
		t.Fatal("v foi anunciado sem raw_response")
	}
	m.analysis.RawResponse = "resposta"
	if !strings.Contains(m.View(), "[v]") {
		t.Fatal("v não foi anunciado com raw_response")
	}
}

func TestResponsiveViewsAndEmptyStates(t *testing.T) {
	m := New(applicationRunStub())
	m, _ = updateModel(t, m, tea.WindowSizeMsg{Width: 50, Height: 10})
	if !strings.Contains(m.View(), "Terminal muito pequeno") {
		t.Fatal("aviso de terminal pequeno ausente")
	}
	m, _ = updateModel(t, m, tea.WindowSizeMsg{Width: 80, Height: 24})
	m.screen = ScreenFindings
	if !strings.Contains(m.View(), "Nenhum achado") {
		t.Fatal("estado vazio ausente")
	}
	m.offset = -10
	m, _ = updateModel(t, m, tea.WindowSizeMsg{Width: 160, Height: 50})
	if m.offset < 0 {
		t.Fatal("índice negativo após resize")
	}
}

func TestEscapeFromConversationAnalysisReturnsToPreviousScreenAndPreservesDeal(t *testing.T) {
	m := New(applicationRunStub())
	m.screen = ScreenConversationAnalysis
	m.detailOrigin = ScreenFindings
	m.detail = dashboard.DealDetail{BitrixDealID: 8620, Title: "Negócio selecionado"}
	m.analysis = dashboard.ConversationAnalysisDetail{BitrixDealID: 8620}
	m, _ = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.screen != ScreenDealDetail {
		t.Fatalf("Esc voltou para a tela %v, esperado detalhe", m.screen)
	}
	if m.detail.BitrixDealID != 8620 || m.detail.Title != "Negócio selecionado" {
		t.Fatalf("negócio selecionado foi perdido: %#v", m.detail)
	}
}

func TestEscapeFromRawResponseClosesRawBeforeLeavingAnalysis(t *testing.T) {
	m := New(applicationRunStub())
	m.screen = ScreenRawResponse
	m.detailOrigin = ScreenFindings
	m.detail = dashboard.DealDetail{BitrixDealID: 8620}
	m.analysis = dashboard.ConversationAnalysisDetail{BitrixDealID: 8620, RawResponse: "original"}
	m, _ = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.screen != ScreenConversationAnalysis {
		t.Fatalf("primeiro Esc deveria fechar raw_response, tela=%v", m.screen)
	}
	m, _ = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.screen != ScreenDealDetail {
		t.Fatalf("segundo Esc deveria voltar ao detalhe, tela=%v", m.screen)
	}
	if m.detail.BitrixDealID != 8620 {
		t.Fatal("negócio selecionado foi perdido")
	}
}

func TestConversationUnavailableIsShownInTUI(t *testing.T) {
	m := New(applicationRunStub())
	m.screen = ScreenConversationAnalysis
	m.width, m.height = 120, 40
	m.analysis = dashboard.ConversationAnalysisDetail{
		BitrixDealID:  8620,
		Title:         "Negócio sem conversa",
		MessageCount:  0,
		FinalResult:   "indeterminado",
		ResultSource:  "regras determinísticas; conversa indisponível",
		StatusMessage: "Análise de conversa indisponível por ausência de mensagens IMOPENLINES e formulário CRM.",
	}
	view := m.View()
	if !strings.Contains(view, "ANÁLISE DE CONVERSA INDISPONÍVEL") || strings.Contains(view, "Motivo principal:") {
		t.Fatalf("estado indisponível renderizado incorretamente:\n%s", view)
	}
}

func TestQQuitsOnlyOutsideSearchField(t *testing.T) {
	m := New(applicationRunStub())
	_, cmd := m.Update(key("q"))
	if cmd == nil {
		t.Fatal("q deveria sair quando a busca não está ativa")
	}
	next, _ := m.startSearch(false)
	m = next.(Model)
	m, _ = updateModel(t, m, key("q"))
	if m.search.Value() != "q" {
		t.Fatal("q deveria ser digitado no campo de busca")
	}
}

func TestPDFExportFromDealDetailIsAsyncAndShowsSuccess(t *testing.T) {
	exporter := &fakeReportExporter{path: `C:\reports\auditoria-44.pdf`}
	m := NewWithReportExporter(applicationRunStub(), &fakeDashboard{}, exporter)
	m.screen = ScreenDealDetail
	m.width, m.height = 100, 30
	m.detail = dashboard.DealDetail{AssessmentID: 44, BitrixDealID: 8620}
	m, cmd := updateModel(t, m, key("p"))
	if cmd == nil || !m.reportGenerating || exporter.calls != 0 {
		t.Fatalf("geração não iniciou de forma assíncrona: cmd=%v loading=%v calls=%d", cmd != nil, m.reportGenerating, exporter.calls)
	}
	if !strings.Contains(m.View(), "Gerando relatório...") {
		t.Fatal("estado de carregamento ausente")
	}
	m = runCmd(t, m, cmd)
	if m.screen != ScreenDealDetail || m.reportGenerating || m.reportPath != exporter.path || exporter.assessmentID != 44 {
		t.Fatalf("sucesso não exibido: %#v", m)
	}
	if !strings.Contains(m.View(), exporter.path) {
		t.Fatal("caminho absoluto não exibido")
	}
}

func TestPDFExportErrorKeepsTUIRunning(t *testing.T) {
	exporter := &fakeReportExporter{err: errors.New("falha de disco")}
	m := NewWithReportExporter(applicationRunStub(), &fakeDashboard{}, exporter)
	m.screen = ScreenDealDetail
	m.detail = dashboard.DealDetail{AssessmentID: 44}
	m, cmd := updateModel(t, m, key("p"))
	m = runCmd(t, m, cmd)
	if m.screen != ScreenError || m.err == nil || !strings.Contains(m.View(), "falha de disco") {
		t.Fatal("erro de PDF não foi mantido no painel da TUI")
	}
}

func TestPDFKeyIsIgnoredOutsideDealDetail(t *testing.T) {
	exporter := &fakeReportExporter{path: "unused"}
	m := NewWithReportExporter(applicationRunStub(), &fakeDashboard{}, exporter)
	m.screen = ScreenMenu
	m, cmd := updateModel(t, m, key("p"))
	if cmd != nil || exporter.calls != 0 || m.reportGenerating {
		t.Fatal("tecla p não foi ignorada fora dos detalhes")
	}
}

func TestKnownAndUnknownProgressViews(t *testing.T) {
	m := New(applicationRunStub())
	m.screen = ScreenAuditProgress
	m.loading = true
	m.progress = application.AuditProgress{Phase: "coletando"}
	if !strings.Contains(m.View(), "Total ainda desconhecido") {
		t.Fatal("progresso sem total inventou porcentagem")
	}
	m.progress = application.AuditProgress{Phase: "regras", Processed: 5, Total: 10, Findings: 2}
	if !strings.Contains(m.View(), "5/10 — 50%") {
		t.Fatal("progresso conhecido não exibiu valores reais")
	}
	m.audit = &domain.Audit{}
	m.screen = ScreenAuditResult
	if strings.Contains(m.View(), "100%") {
		t.Fatal("progresso conhecido inválido")
	}
}

func applicationRunStub() application.RunAudit { return application.RunAudit{} }
