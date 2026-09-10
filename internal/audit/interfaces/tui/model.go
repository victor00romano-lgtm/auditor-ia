package tui

import (
	"context"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/charmbracelet/bubbles/spinner"
	"github.com/charmbracelet/bubbles/textinput"
	tea "github.com/charmbracelet/bubbletea"
	"github.com/charmbracelet/lipgloss"
	"github.com/portfolio/auditor-ia/internal/audit/application"
	"github.com/portfolio/auditor-ia/internal/audit/domain"
	batchdomain "github.com/portfolio/auditor-ia/internal/batch/domain"
	business "github.com/portfolio/auditor-ia/internal/business/domain"
	statsdomain "github.com/portfolio/auditor-ia/internal/crmstats/domain"
	dashboard "github.com/portfolio/auditor-ia/internal/dashboard/domain"
	"github.com/portfolio/auditor-ia/internal/platform/security"
)

type Screen int

const (
	ScreenMenu Screen = iota
	ScreenAuditProgress
	ScreenAuditResult
	ScreenSummary
	ScreenFindings
	ScreenDealSearch
	ScreenDealDetail
	ScreenConversationAnalysis
	ScreenRawResponse
	ScreenExecutive
	ScreenFunnel
	ScreenPriorities
	ScreenDivergences
	ScreenCRMQuality
	ScreenConversationQuality
	ScreenObjections
	ScreenAgents
	ScreenTrends
	ScreenAIQuality
	ScreenHelp
	ScreenBatchImport
	ScreenBatchProgress
	ScreenError
)

type resultMsg struct {
	audit   *domain.Audit
	request uint64
	err     error
}
type summaryMsg struct {
	summary dashboard.Summary
	request uint64
	err     error
}
type findingsMsg struct {
	page    dashboard.FindingPage
	rules   []string
	request uint64
	err     error
}
type detailMsg struct {
	detail  dashboard.DealDetail
	id      int64
	request uint64
	err     error
}
type analysisMsg struct {
	analysis     dashboard.ConversationAnalysisDetail
	detail       dashboard.DealDetail
	id           int64
	assessmentID int64
	request      uint64
	err          error
}
type elapsedMsg time.Time
type auditProgressMsg struct {
	progress application.AuditProgress
	request  uint64
}
type progressClosedMsg struct{ request uint64 }
type reportMsg struct {
	path         string
	assessmentID int64
	err          error
}
type businessMsg struct {
	dashboard business.Dashboard
	target    Screen
	request   uint64
	err       error
}
type csvMsg struct {
	path    string
	target  Screen
	request uint64
	err     error
}
type executivePDFMsg struct {
	path    string
	request uint64
	err     error
}
type batchCreatedMsg struct {
	batch   batchdomain.Batch
	request uint64
	err     error
}
type batchProgressMsg struct {
	batch   batchdomain.Batch
	request uint64
	err     error
}
type batchCanceledMsg struct {
	request uint64
	err     error
}
type batchPollMsg struct{ request uint64 }
type crmSyncProgressMsg struct {
	progress statsdomain.Progress
	request  uint64
}
type crmSyncDoneMsg struct {
	request uint64
	err     error
}
type crmSyncProgressClosedMsg struct{ request uint64 }

type ListState struct {
	Cursor, Offset, Page, PageSize int
	SortField                      string
	SortDesc                       bool
	SelectedID                     string
}

type navigationEntry struct {
	Screen  Screen
	Filters business.GlobalFilters
}

type BusinessCSVExporter interface {
	Priorities(context.Context, []business.PriorityOpportunity) (string, error)
	Divergences(context.Context, []business.CRMAIDivergence) (string, error)
	CRMQuality(context.Context, business.CRMQualitySummary) (string, error)
	Objections(context.Context, []business.ObjectionSummary) (string, error)
	Agents(context.Context, []business.AgentPerformance) (string, error)
}

type BusinessDashboardLoader interface {
	Load(context.Context, business.GlobalFilters) (business.Dashboard, error)
}

type BusinessPDFExporter interface {
	Export(context.Context, business.GlobalFilters) (string, error)
}
type BatchManager interface {
	Create(context.Context, []int64) (batchdomain.Batch, error)
	Get(context.Context, int64) (batchdomain.Batch, error)
	Cancel(context.Context, int64) error
}
type CRMStatisticsSynchronizer interface {
	Sync(context.Context, func(statsdomain.Progress)) error
}

type retryOperationKind uint8

const (
	retryNone retryOperationKind = iota
	retryDashboard
	retryFindings
	retryDetail
	retryAnalysis
)

type retryOperation struct {
	kind         retryOperationKind
	dealID       int64
	assessmentID int64
	returnTo     Screen
}

type activeRead struct {
	request   uint64
	operation retryOperation
	refresh   bool
}

type ReportExporter interface {
	Export(context.Context, int64) (string, error)
}

type Model struct {
	run                                     application.RunAudit
	repo                                    dashboard.Repository
	spinner                                 spinner.Model
	search                                  textinput.Model
	batchInput                              textinput.Model
	screen                                  Screen
	searchOrigin, detailOrigin, errorReturn Screen
	offsets                                 [ScreenError + 1]int
	menuIndex                               int
	width, height, offset, selected         int
	loading                                 bool
	err                                     error
	retry                                   retryOperation
	active                                  activeRead
	requestSequence                         uint64
	auditRequest                            uint64
	updatedAt                               time.Time
	audit                                   *domain.Audit
	auditStarted                            time.Time
	elapsed                                 time.Duration
	summary                                 dashboard.Summary
	page                                    dashboard.FindingPage
	filter                                  dashboard.FindingFilter
	rules                                   []string
	ruleIndex, severityIndex                int
	detail                                  dashboard.DealDetail
	analysis                                dashboard.ConversationAnalysisDetail
	searchAnalysis                          bool
	progress                                application.AuditProgress
	progressCh                              chan application.AuditProgress
	reportExporter                          ReportExporter
	reportGenerating                        bool
	reportPath                              string
	businessLoader                          BusinessDashboardLoader
	businessDashboard                       business.Dashboard
	businessFilters                         business.GlobalFilters
	businessTarget                          Screen
	listStates                              map[Screen]ListState
	navigation                              []navigationEntry
	filterDraft                             business.GlobalFilters
	filterField                             int
	filterOpen                              bool
	csvExporter                             BusinessCSVExporter
	businessPDFExporter                     BusinessPDFExporter
	businessPDFGenerating                   bool
	businessPDFRequest                      uint64
	exporting                               bool
	feedback                                string
	batchManager                            BatchManager
	batch                                   batchdomain.Batch
	batchRequest                            uint64
	batchLoading                            bool
	crmSyncer                               CRMStatisticsSynchronizer
	crmSyncing                              bool
	crmSyncRequest                          uint64
	crmSyncProgress                         statsdomain.Progress
	crmSyncProgressCh                       chan statsdomain.Progress
	crmSyncCancel                           context.CancelFunc
}

var severities = []string{"", "CRITICAL", "HIGH", "MEDIUM", "LOW"}

func New(run application.RunAudit, repositories ...dashboard.Repository) Model {
	s := spinner.New()
	s.Spinner = spinner.Dot
	s.Style = lipgloss.NewStyle().Foreground(lipgloss.Color("205"))
	input := textinput.New()
	input.Placeholder = "ID do negócio no Bitrix"
	input.CharLimit = 20
	input.Width = 30
	batchInput := textinput.New()
	batchInput.Placeholder = "IDs separados por vírgula ou @C:\\caminho\\negocios.csv"
	batchInput.CharLimit = 100000
	batchInput.Width = 64
	var repo dashboard.Repository
	if len(repositories) > 0 {
		repo = repositories[0]
	}
	return Model{run: run, repo: repo, spinner: s, search: input, batchInput: batchInput, screen: ScreenMenu, filter: dashboard.FindingFilter{Limit: 25}, listStates: map[Screen]ListState{}}
}

func NewWithReportExporter(run application.RunAudit, repository dashboard.Repository, exporter ReportExporter) Model {
	model := New(run, repository)
	model.reportExporter = exporter
	return model
}

func NewBusinessDashboard(run application.RunAudit, repository dashboard.Repository, exporter ReportExporter, loader BusinessDashboardLoader, csv ...BusinessCSVExporter) Model {
	model := NewWithReportExporter(run, repository, exporter)
	model.businessLoader = loader
	model.businessFilters.Period = business.PeriodAll
	if len(csv) > 0 {
		model.csvExporter = csv[0]
	}
	return model
}

func NewBusinessDashboardWithExports(run application.RunAudit, repository dashboard.Repository, exporter ReportExporter, loader BusinessDashboardLoader, csv BusinessCSVExporter, pdf BusinessPDFExporter) Model {
	model := NewBusinessDashboard(run, repository, exporter, loader, csv)
	model.businessPDFExporter = pdf
	return model
}

func (m *Model) SetBatchManager(manager BatchManager) { m.batchManager = manager }
func (m *Model) SetCRMStatisticsSynchronizer(syncer CRMStatisticsSynchronizer) {
	m.crmSyncer = syncer
}

func (m Model) Init() tea.Cmd { return nil }

func (m Model) Update(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		if key.Type == tea.KeyEsc {
			if m.filterOpen {
				m.filterOpen = false
				return m, nil
			}
			return m.navigateBack()
		}
		if key.String() != "r" {
			m.updatedAt = time.Time{}
		}
		if m.search.Focused() {
			return m.updateSearch(key)
		}
		if m.batchInput.Focused() {
			return m.updateBatchInput(key)
		}
		return m.handleKey(key)
	}
	if size, ok := msg.(tea.WindowSizeMsg); ok {
		m.width = size.Width
		m.height = size.Height
		if m.isBusinessList(m.screen) {
			m.setListState(m.screen, m.listState(m.screen))
		}
		m.clamp()
		return m, nil
	}
	switch msg := msg.(type) {
	case spinner.TickMsg:
		var cmd tea.Cmd
		m.spinner, cmd = m.spinner.Update(msg)
		return m, cmd
	case elapsedMsg:
		if m.loading && m.screen == ScreenAuditProgress {
			m.elapsed = time.Since(m.auditStarted)
			return m, tickElapsed()
		}
	case auditProgressMsg:
		if msg.request != m.auditRequest || m.screen != ScreenAuditProgress {
			return m, nil
		}
		m.progress = msg.progress
		return m, m.waitProgress()
	case progressClosedMsg:
		return m, nil
	case reportMsg:
		if msg.assessmentID != m.detail.AssessmentID || m.screen != ScreenDealDetail {
			return m, nil
		}
		m.reportGenerating = false
		if msg.err != nil {
			return m.showError(msg.err, retryOperation{})
		}
		m.reportPath = msg.path
		m.screen = ScreenDealDetail
		return m, nil
	case csvMsg:
		if msg.request != m.requestSequence || msg.target != m.screen {
			return m, nil
		}
		m.exporting = false
		if msg.err != nil {
			return m.showError(msg.err, retryOperation{})
		}
		m.feedback = "CSV salvo em " + msg.path
		return m, nil
	case executivePDFMsg:
		if msg.request != m.businessPDFRequest {
			return m, nil
		}
		m.businessPDFGenerating = false
		if msg.err != nil {
			m.errorReturn = ScreenExecutive
			return m.showError(msg.err, retryOperation{})
		}
		if m.screen == ScreenExecutive {
			m.feedback = "PDF executivo salvo em " + msg.path
		}
		return m, nil
	case batchCreatedMsg:
		if msg.request != m.batchRequest {
			return m, nil
		}
		m.batchLoading = false
		if msg.err != nil {
			m.err = safeUIError(msg.err)
			return m, nil
		}
		m.batch = msg.batch
		m.screen = ScreenBatchProgress
		m.feedback = "Lote criado"
		m.batchInput.Blur()
		return m, batchPollCmd(msg.request)
	case batchPollMsg:
		if msg.request != m.batchRequest || m.screen != ScreenBatchProgress || isBatchTerminal(m.batch.Status) {
			return m, nil
		}
		return m, m.loadBatchProgress(msg.request, m.batch.ID)
	case batchProgressMsg:
		if msg.request != m.batchRequest || m.screen != ScreenBatchProgress {
			return m, nil
		}
		if msg.err != nil {
			m.err = safeUIError(msg.err)
			return m, batchPollCmd(msg.request)
		}
		m.err = nil
		m.batch = msg.batch
		m.updatedAt = time.Now()
		return m, batchPollCmd(msg.request)
	case batchCanceledMsg:
		if msg.request != m.batchRequest {
			return m, nil
		}
		m.batchLoading = false
		if msg.err != nil {
			m.err = safeUIError(msg.err)
			return m, nil
		}
		m.feedback = "Cancelamento solicitado"
		return m, m.loadBatchProgress(msg.request, m.batch.ID)
	case crmSyncProgressMsg:
		if msg.request != m.crmSyncRequest || !m.crmSyncing {
			return m, nil
		}
		m.crmSyncProgress = msg.progress
		return m, m.waitCRMStatsProgress(msg.request)
	case crmSyncProgressClosedMsg:
		return m, nil
	case crmSyncDoneMsg:
		if msg.request != m.crmSyncRequest {
			return m, nil
		}
		m.crmSyncing = false
		m.crmSyncCancel = nil
		if msg.err != nil {
			m.err = safeUIError(msg.err)
			m.feedback = "Sincronização global não concluída"
			return m, nil
		}
		m.feedback = "Sincronização global concluída"
		if m.screen == ScreenExecutive {
			return m.loadBusiness(ScreenExecutive)
		}
		return m, nil
	case businessMsg:
		if msg.request != m.requestSequence || msg.target != m.businessTarget {
			return m, nil
		}
		m.loading = false
		if msg.err != nil {
			return m.showError(msg.err, retryOperation{})
		}
		selectedID := m.selectedBusinessID(msg.target)
		m.businessDashboard = msg.dashboard
		m.restoreBusinessSelection(msg.target, selectedID)
		m.updatedAt = msg.dashboard.LoadedAt
		m.screen = msg.target
		return m, nil
	case resultMsg:
		if msg.request != m.auditRequest || m.screen != ScreenAuditProgress {
			return m, nil
		}
		m.loading = false
		m.audit = msg.audit
		if msg.err != nil {
			next, cmd := m.showError(msg.err, retryOperation{})
			errorModel := next.(Model)
			errorModel.errorReturn = ScreenMenu
			return errorModel, cmd
		}
		m.retry = retryOperation{}
		m.screen = ScreenAuditResult
		m.offset = 0
	case summaryMsg:
		if !m.isCurrent(msg.request, retryDashboard, 0) {
			return m, nil
		}
		refreshed := m.active.refresh
		m.active = activeRead{}
		m.loading = false
		if msg.err != nil {
			return m.showError(msg.err, m.retryFor(retryDashboard, 0, 0))
		}
		m.retry = retryOperation{}
		m.summary = msg.summary
		m.moveTo(ScreenSummary, !refreshed)
		m.markUpdated(refreshed)
	case findingsMsg:
		if !m.isCurrent(msg.request, retryFindings, 0) {
			return m, nil
		}
		refreshed := m.active.refresh
		m.active = activeRead{}
		m.loading = false
		if msg.err != nil {
			return m.showError(msg.err, m.retryFor(retryFindings, 0, 0))
		}
		m.retry = retryOperation{}
		m.page = msg.page
		if msg.rules != nil {
			m.rules = msg.rules
		}
		m.filter.Offset = msg.page.Offset
		m.normalizeSelection()
		m.moveTo(ScreenFindings, !refreshed)
		m.markUpdated(refreshed)
	case detailMsg:
		if !m.isCurrent(msg.request, retryDetail, msg.id) {
			return m, nil
		}
		refreshed := m.active.refresh
		m.active = activeRead{}
		m.loading = false
		if msg.err != nil {
			return m.showError(msg.err, m.retryFor(retryDetail, msg.id, 0))
		}
		m.retry = retryOperation{}
		if msg.detail.BitrixDealID != msg.id {
			return m.showError(errors.New("resposta de negócio incompatível com a busca"), m.retryFor(retryDetail, msg.id, 0))
		}
		m.detail = msg.detail
		m.moveTo(ScreenDealDetail, !refreshed)
		m.markUpdated(refreshed)
	case analysisMsg:
		if !m.isCurrent(msg.request, retryAnalysis, msg.id) {
			return m, nil
		}
		refreshed := m.active.refresh
		m.active = activeRead{}
		m.loading = false
		if msg.err != nil {
			return m.showError(msg.err, m.retryFor(retryAnalysis, msg.id, msg.assessmentID))
		}
		m.retry = retryOperation{}
		if msg.analysis.BitrixDealID != msg.id || (msg.assessmentID > 0 && msg.analysis.AssessmentID != msg.assessmentID) {
			return m.showError(errors.New("a análise recebida não pertence à avaliação selecionada"), m.retryFor(retryAnalysis, msg.id, msg.assessmentID))
		}
		m.analysis = msg.analysis
		m.detail = msg.detail
		m.moveTo(ScreenConversationAnalysis, !refreshed)
		m.markUpdated(refreshed)
	}
	m.clamp()
	return m, nil
}

func (m Model) handleKey(key tea.KeyMsg) (tea.Model, tea.Cmd) {
	k := key.String()
	if k == "ctrl+c" {
		return m, tea.Quit
	}
	if k == "q" {
		return m, tea.Quit
	}
	if m.loading {
		return m, nil
	}
	if m.filterOpen {
		return m.handleFilterKey(k)
	}
	switch m.screen {
	case ScreenMenu:
		switch k {
		case "up", "k":
			if m.menuIndex > 0 {
				m.menuIndex--
			}
		case "down", "j":
			if m.menuIndex < 13 {
				m.menuIndex++
			}
		case "1", "2", "3", "4", "5", "6", "7", "8", "9", "0", "a", "A", "b", "B", "c", "C", "l", "L", "?":
			return m.activateBusinessMenuKey(k)
		case "enter":
			return m.activateBusinessMenu(m.menuIndex)
		}
	case ScreenExecutive, ScreenFunnel, ScreenPriorities, ScreenDivergences, ScreenCRMQuality, ScreenConversationQuality, ScreenObjections, ScreenAgents, ScreenTrends, ScreenAIQuality, ScreenHelp:
		switch k {
		case "r":
			return m.loadBusiness(m.screen)
		case "c":
			m.businessFilters = business.GlobalFilters{Period: business.PeriodAll}
			m.feedback = "Filtros limpos"
			return m.loadBusiness(m.screen)
		case "d":
			m.businessFilters.Period = nextPeriod(m.businessFilters.Period)
			return m.loadBusiness(m.screen)
		case "f":
			m.filterDraft = m.businessFilters
			m.filterOpen = true
			m.filterField = 0
			return m, nil
		case "s":
			m.cycleSort(m.screen)
			m.feedback = "Ordenação alterada"
			return m, nil
		case "e":
			return m.beginCSVExport()
		case "p":
			if m.screen == ScreenExecutive && m.businessPDFExporter != nil && !m.businessPDFGenerating {
				m.businessPDFGenerating = true
				m.businessPDFRequest++
				m.feedback = "Gerando relatório executivo..."
				return m, m.exportExecutivePDF(m.businessPDFRequest, m.businessFilters)
			}
		case "u", "U":
			if m.screen == ScreenExecutive {
				return m.startCRMStatsSync()
			}
		case "x", "X":
			if m.screen == ScreenExecutive && m.crmSyncing && m.crmSyncCancel != nil {
				m.crmSyncCancel()
				m.feedback = "Cancelamento da sincronização solicitado"
			}
		case "enter":
			return m.openBusinessSelection()
		}
		if m.isBusinessList(m.screen) {
			m.moveBusinessCursor(k)
		} else {
			m.scrollKey(k)
		}
	case ScreenAuditResult:
		if k == "enter" {
			return m.openFindings()
		}
	case ScreenSummary:
		if k == "r" {
			return m.beginRead(retryDashboard, 0, 0, true)
		}
		m.scrollKey(k)
	case ScreenFindings:
		switch k {
		case "up", "k":
			if m.selected > 0 {
				m.selected--
			}
		case "down", "j":
			if m.selected+1 < len(m.page.Items) {
				m.selected++
			}
		case "pgdown":
			if m.page.Offset+m.page.Limit < m.page.Total {
				m.filter.Offset += m.page.Limit
				return m.beginRead(retryFindings, 0, 0, false)
			}
		case "pgup":
			m.filter.Offset -= m.page.Limit
			if m.filter.Offset < 0 {
				m.filter.Offset = 0
			}
			return m.beginRead(retryFindings, 0, 0, false)
		case "home", "g":
			m.filter.Offset = 0
			return m.beginRead(retryFindings, 0, 0, false)
		case "end", "G":
			if m.page.Total > 0 {
				m.filter.Offset = ((m.page.Total - 1) / m.page.Limit) * m.page.Limit
			}
			return m.beginRead(retryFindings, 0, 0, false)
		case "f":
			m.severityIndex = (m.severityIndex + 1) % len(severities)
			m.filter.Severity = severities[m.severityIndex]
			m.filter.Offset = 0
			return m.beginRead(retryFindings, 0, 0, false)
		case "F":
			if len(m.rules) > 0 {
				m.ruleIndex = (m.ruleIndex + 1) % (len(m.rules) + 1)
				if m.ruleIndex == 0 {
					m.filter.Rule = ""
				} else {
					m.filter.Rule = m.rules[m.ruleIndex-1]
				}
				m.filter.Offset = 0
				return m.beginRead(retryFindings, 0, 0, false)
			}
		case "c":
			m.filter.Severity = ""
			m.filter.Rule = ""
			m.filter.BitrixDealID = 0
			m.severityIndex = 0
			m.ruleIndex = 0
			m.filter.Offset = 0
			return m.beginRead(retryFindings, 0, 0, false)
		case "/":
			return m.startSearch(false)
		case "r":
			return m.beginRead(retryFindings, 0, 0, true)
		case "enter":
			if len(m.page.Items) > 0 {
				m.detailOrigin = ScreenFindings
				return m.beginRead(retryDetail, m.page.Items[m.selected].BitrixDealID, 0, false)
			}
		}
	case ScreenDealDetail:
		if k == "r" {
			return m.beginRead(retryDetail, m.detail.BitrixDealID, 0, true)
		}
		if k == "a" && m.detail.HasAnalysis {
			m.pushNavigation()
			return m.beginRead(retryAnalysis, m.detail.BitrixDealID, m.detail.AssessmentID, false)
		}
		if k == "p" && !m.reportGenerating && m.detail.AssessmentID > 0 && m.reportExporter != nil {
			m.reportGenerating = true
			m.reportPath = ""
			return m, m.exportPDF(m.detail.AssessmentID)
		}
		m.scrollKey(k)
	case ScreenConversationAnalysis:
		if k == "r" {
			return m.beginRead(retryAnalysis, m.analysis.BitrixDealID, m.analysis.AssessmentID, true)
		}
		if k == "v" && m.analysis.RawResponse != "" {
			m.pushNavigation()
			m.moveTo(ScreenRawResponse, true)
			return m, nil
		}
		m.scrollKey(k)
	case ScreenRawResponse:
		m.scrollKey(k)
	case ScreenBatchProgress:
		switch k {
		case "r":
			return m, m.loadBatchProgress(m.batchRequest, m.batch.ID)
		case "c":
			if m.batchLoading || isBatchTerminal(m.batch.Status) {
				return m, nil
			}
			m.batchLoading = true
			return m, tea.Batch(m.cancelBatch(m.batchRequest, m.batch.ID), m.spinner.Tick)
		}
	case ScreenError:
		if k == "r" && m.retry.kind != retryNone {
			return m.beginRead(m.retry.kind, m.retry.dealID, m.retry.assessmentID, true)
		}
	}
	m.clamp()
	return m, nil
}

func (m Model) activateBusinessMenuKey(k string) (tea.Model, tea.Cmd) {
	indexes := map[string]int{"1": 0, "2": 1, "3": 2, "4": 3, "5": 4, "6": 5, "7": 6, "8": 7, "9": 8, "0": 9, "a": 10, "A": 10, "b": 11, "B": 11, "c": 12, "C": 12, "?": 12, "l": 13, "L": 13}
	index, ok := indexes[k]
	if !ok {
		return m, nil
	}
	m.menuIndex = index
	return m.activateBusinessMenu(index)
}

func (m Model) activateBusinessMenu(index int) (tea.Model, tea.Cmd) {
	screens := []Screen{ScreenExecutive, ScreenFunnel, ScreenPriorities, ScreenDivergences, ScreenCRMQuality, ScreenConversationQuality, ScreenObjections, ScreenAgents, ScreenTrends, ScreenAIQuality}
	if index < len(screens) {
		return m.loadBusiness(screens[index])
	}
	switch index {
	case 10:
		return m.openFindings()
	case 11:
		return m.startSearch(false)
	case 13:
		m.screen = ScreenBatchImport
		m.err = nil
		m.batchInput.SetValue("")
		return m, m.batchInput.Focus()
	default:
		m.screen = ScreenHelp
		m.offset = 0
		return m, nil
	}
}

func (m Model) loadBusiness(target Screen) (tea.Model, tea.Cmd) {
	if m.loading {
		return m, nil
	}
	if m.businessLoader == nil {
		return m.showError(errors.New("dashboard empresarial não configurado"), retryOperation{})
	}
	m.requestSequence++
	request := m.requestSequence
	m.businessTarget = target
	m.loading = true
	m.feedback = ""
	loader := m.businessLoader
	filters := m.businessFilters
	return m, func() tea.Msg {
		dashboard, err := loader.Load(context.Background(), filters)
		return businessMsg{dashboard: dashboard, target: target, request: request, err: err}
	}
}

func nextPeriod(period business.DashboardPeriod) business.DashboardPeriod {
	periods := []business.DashboardPeriod{business.PeriodAll, business.PeriodToday, business.PeriodYesterday, business.Period7Days, business.Period30Days, business.PeriodCurrentMonth, business.PeriodPreviousMonth}
	for i, v := range periods {
		if v == period {
			return periods[(i+1)%len(periods)]
		}
	}
	return business.PeriodAll
}

func (m Model) updateSearch(msg tea.Msg) (tea.Model, tea.Cmd) {
	if key, ok := msg.(tea.KeyMsg); ok {
		switch key.String() {
		case "enter":
			value := strings.TrimSpace(m.search.Value())
			id, err := strconv.ParseInt(value, 10, 64)
			if err != nil || id <= 0 {
				m.err = errors.New("informe um ID numérico válido")
				return m, nil
			}
			m.search.Blur()
			m.detailOrigin = ScreenDealSearch
			if m.searchAnalysis {
				return m.beginRead(retryAnalysis, id, 0, false)
			}
			return m.beginRead(retryDetail, id, 0, false)
		}
	}
	var cmd tea.Cmd
	m.search, cmd = m.search.Update(msg)
	return m, cmd
}

func (m Model) startAudit() (tea.Model, tea.Cmd) {
	m.moveTo(ScreenAuditProgress, true)
	m.loading = true
	m.requestSequence++
	m.auditRequest = m.requestSequence
	m.audit = nil
	m.auditStarted = time.Now()
	m.elapsed = 0
	m.progress = application.AuditProgress{Phase: "coletando negócios"}
	m.progressCh = make(chan application.AuditProgress, 16)
	return m, tea.Batch(m.spinner.Tick, tickElapsed(), m.executeAudit(), m.waitProgress())
}
func (m Model) startSearch(analysis bool) (tea.Model, tea.Cmd) {
	m.searchOrigin = m.screen
	m.moveTo(ScreenDealSearch, true)
	m.searchAnalysis = analysis
	m.err = nil
	m.search.SetValue("")
	return m, m.search.Focus()
}
func (m Model) openFindings() (tea.Model, tea.Cmd) {
	m.filter.Offset = 0
	return m.beginRead(retryFindings, 0, 0, false)
}
func (m Model) showError(err error, retry retryOperation) (tea.Model, tea.Cmd) {
	if m.screen != ScreenError {
		m.errorReturn = m.screen
	}
	if retry.returnTo == ScreenError {
		retry.returnTo = m.errorReturn
	}
	m.moveTo(ScreenError, true)
	m.err = safeUIError(err)
	m.loading = false
	m.retry = retry
	return m, nil
}

func (m Model) retryFor(kind retryOperationKind, dealID, assessmentID int64) retryOperation {
	returnTo := m.screen
	if returnTo == ScreenError {
		returnTo = m.errorReturn
	}
	return retryOperation{kind: kind, dealID: dealID, assessmentID: assessmentID, returnTo: returnTo}
}
func safeUIError(err error) error {
	if err == nil {
		return nil
	}
	return fmt.Errorf("%s", security.Error(err))
}
func tickElapsed() tea.Cmd {
	return tea.Tick(time.Second, func(t time.Time) tea.Msg { return elapsedMsg(t) })
}
func (m Model) executeAudit() tea.Cmd {
	updates := m.progressCh
	request := m.auditRequest
	return func() tea.Msg {
		run := m.run
		run.Progress = func(progress application.AuditProgress) { updates <- progress }
		a, err := run.Execute(context.Background(), "demo-company")
		close(updates)
		return resultMsg{audit: a, request: request, err: err}
	}
}
func (m Model) waitProgress() tea.Cmd {
	updates := m.progressCh
	request := m.auditRequest
	return func() tea.Msg {
		progress, ok := <-updates
		if !ok {
			return progressClosedMsg{request: request}
		}
		return auditProgressMsg{progress: progress, request: request}
	}
}
func (m Model) loadSummary(request uint64) tea.Cmd {
	return func() tea.Msg {
		if m.repo == nil {
			return summaryMsg{request: request, err: errors.New("PostgreSQL não configurado")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		s, err := m.repo.GetSummary(ctx)
		return summaryMsg{summary: s, request: request, err: err}
	}
}
func (m Model) loadFindings(request uint64) tea.Cmd {
	filter := m.filter
	return func() tea.Msg {
		if m.repo == nil {
			return findingsMsg{request: request, err: errors.New("PostgreSQL não configurado")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		p, err := m.repo.ListFindings(ctx, filter)
		if err != nil {
			return findingsMsg{request: request, err: err}
		}
		rules, err := m.repo.ListRules(ctx)
		return findingsMsg{page: p, rules: rules, request: request, err: err}
	}
}
func (m Model) loadDetail(request uint64, id int64) tea.Cmd {
	return func() tea.Msg {
		if m.repo == nil {
			return detailMsg{id: id, request: request, err: errors.New("PostgreSQL não configurado")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		v, err := m.repo.GetDealDetail(ctx, id)
		return detailMsg{detail: v, id: id, request: request, err: err}
	}
}
func (m Model) loadAnalysis(request uint64, id, assessmentID int64) tea.Cmd {
	return func() tea.Msg {
		if m.repo == nil {
			return analysisMsg{id: id, assessmentID: assessmentID, request: request, err: errors.New("PostgreSQL não configurado")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		detail, err := m.repo.GetDealDetail(ctx, id)
		if err != nil {
			return analysisMsg{id: id, assessmentID: assessmentID, request: request, err: err}
		}
		if assessmentID > 0 && detail.AssessmentID != assessmentID {
			return analysisMsg{id: id, assessmentID: assessmentID, request: request, err: errors.New("a avaliação selecionada deixou de ser a mais recente")}
		}
		v, err := m.repo.GetLatestConversationAnalysis(ctx, id)
		return analysisMsg{analysis: v, detail: detail, id: id, assessmentID: detail.AssessmentID, request: request, err: err}
	}
}

func (m Model) beginRead(kind retryOperationKind, dealID, assessmentID int64, refresh bool) (tea.Model, tea.Cmd) {
	if m.loading {
		return m, nil
	}
	operation := retryOperation{kind: kind, dealID: dealID, assessmentID: assessmentID, returnTo: m.screen}
	if m.screen == ScreenError && m.retry.kind == kind {
		operation.returnTo = m.retry.returnTo
	}
	m.requestSequence++
	m.active = activeRead{request: m.requestSequence, operation: operation, refresh: refresh}
	m.retry = operation
	m.err = nil
	m.loading = true
	request := m.requestSequence
	switch kind {
	case retryDashboard:
		return m, m.loadSummary(request)
	case retryFindings:
		return m, m.loadFindings(request)
	case retryDetail:
		return m, m.loadDetail(request, dealID)
	case retryAnalysis:
		return m, m.loadAnalysis(request, dealID, assessmentID)
	default:
		m.loading = false
		m.active = activeRead{}
		return m, nil
	}
}

func (m Model) isCurrent(request uint64, kind retryOperationKind, dealID int64) bool {
	return request != 0 && m.active.request == request && m.active.operation.kind == kind && (dealID == 0 || m.active.operation.dealID == dealID)
}

func (m *Model) markUpdated(refreshed bool) {
	if refreshed {
		m.updatedAt = time.Now()
	}
}

func (m *Model) moveTo(target Screen, reset bool) {
	if target == m.screen {
		return
	}
	m.offsets[m.screen] = m.offset
	m.screen = target
	if reset {
		m.offsets[target] = 0
	}
	m.offset = m.offsets[target]
}

func (m Model) navigateBack() (tea.Model, tea.Cmd) {
	m.requestSequence++
	m.batchRequest = m.requestSequence
	m.batchLoading = false
	m.active = activeRead{}
	m.loading = false
	m.reportGenerating = false
	m.err = nil
	m.retry = retryOperation{}
	m.updatedAt = time.Time{}
	if len(m.navigation) > 0 && (m.screen == ScreenDealDetail || m.screen == ScreenConversationAnalysis || m.screen == ScreenRawResponse || m.screen == ScreenError) {
		entry := m.navigation[len(m.navigation)-1]
		m.navigation = m.navigation[:len(m.navigation)-1]
		m.businessFilters = entry.Filters
		m.moveTo(entry.Screen, false)
		return m, nil
	}
	target := ScreenMenu
	switch m.screen {
	case ScreenMenu:
		return m, nil
	case ScreenSummary, ScreenFindings, ScreenAuditProgress, ScreenAuditResult, ScreenExecutive, ScreenFunnel, ScreenPriorities, ScreenDivergences, ScreenCRMQuality, ScreenConversationQuality, ScreenObjections, ScreenAgents, ScreenTrends, ScreenAIQuality, ScreenBatchProgress, ScreenHelp:
		target = ScreenMenu
	case ScreenBatchImport:
		m.batchInput.Blur()
		m.batchInput.SetValue("")
		target = ScreenMenu
	case ScreenDealSearch:
		m.search.Blur()
		m.search.SetValue("")
		m.searchAnalysis = false
		target = m.searchOrigin
	case ScreenDealDetail:
		target = m.detailOrigin
		if target != ScreenFindings && target != ScreenDealSearch {
			target = ScreenMenu
		}
		if target == ScreenDealSearch {
			m.moveTo(target, false)
			m.search.SetValue("")
			return m, m.search.Focus()
		}
	case ScreenConversationAnalysis:
		target = ScreenDealDetail
	case ScreenRawResponse:
		target = ScreenConversationAnalysis
	case ScreenError:
		target = m.errorReturn
		if target == ScreenError {
			target = ScreenMenu
		}
	}
	m.moveTo(target, false)
	return m, nil
}

func (m *Model) normalizeSelection() {
	if len(m.page.Items) == 0 {
		m.selected = 0
	} else if m.selected >= len(m.page.Items) {
		m.selected = len(m.page.Items) - 1
	}
}

func (m Model) exportPDF(assessmentID int64) tea.Cmd {
	exporter := m.reportExporter
	return func() tea.Msg {
		if exporter == nil {
			return reportMsg{assessmentID: assessmentID, err: errors.New("exportação de PDF não configurada")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		path, err := exporter.Export(ctx, assessmentID)
		return reportMsg{path: path, assessmentID: assessmentID, err: err}
	}
}

func (m Model) exportExecutivePDF(request uint64, filters business.GlobalFilters) tea.Cmd {
	exporter := m.businessPDFExporter
	return func() tea.Msg {
		if exporter == nil {
			return executivePDFMsg{request: request, err: errors.New("exportação executiva não configurada")}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 45*time.Second)
		defer cancel()
		path, err := exporter.Export(ctx, filters)
		return executivePDFMsg{path: path, request: request, err: err}
	}
}
func (m *Model) scrollKey(k string) {
	step := m.contentHeight()
	switch k {
	case "up", "k":
		m.offset--
	case "down", "j":
		m.offset++
	case "pgup":
		m.offset -= step
	case "pgdown":
		m.offset += step
	case "home", "g":
		m.offset = 0
	case "end", "G":
		m.offset = 1 << 30
	}
	m.clamp()
}
func (m *Model) clamp() {
	if m.offset < 0 {
		m.offset = 0
	}
	if m.selected < 0 {
		m.selected = 0
	}
	if len(m.page.Items) == 0 {
		m.selected = 0
	} else if m.selected >= len(m.page.Items) {
		m.selected = len(m.page.Items) - 1
	}
	lines := 0
	switch m.screen {
	case ScreenSummary:
		lines = 20 + len(m.summary.FindingsByRule)
	case ScreenFindings:
		lines = 10 + len(m.page.Items)
	case ScreenDealDetail:
		lines = 24 + 3*len(m.detail.Findings)
	case ScreenConversationAnalysis:
		lines = 27
	case ScreenRawResponse:
		lines = 6 + strings.Count(m.analysis.RawResponse, "\n")
	}
	if lines > 0 {
		max := lines - m.contentHeight()
		if max < 0 {
			max = 0
		}
		if m.offset > max {
			m.offset = max
		}
	}
	m.offsets[m.screen] = m.offset
}
func (m Model) contentHeight() int {
	h := m.height - 6
	if h < 1 {
		return 1
	}
	return h
}
