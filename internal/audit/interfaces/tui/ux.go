package tui

import (
	"context"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	business "github.com/portfolio/auditor-ia/internal/business/domain"
)

func (m Model) isBusinessList(screen Screen) bool {
	switch screen {
	case ScreenFunnel, ScreenPriorities, ScreenDivergences, ScreenObjections, ScreenAgents, ScreenTrends:
		return true
	default:
		return false
	}
}

func (m Model) businessListLen(screen Screen) int {
	switch screen {
	case ScreenFunnel:
		return len(m.businessDashboard.Funnel.Stages)
	case ScreenPriorities:
		return len(m.businessDashboard.Priorities)
	case ScreenDivergences:
		return len(m.businessDashboard.Divergences)
	case ScreenObjections:
		return len(m.businessDashboard.Objections)
	case ScreenAgents:
		return len(m.businessDashboard.Agents)
	case ScreenTrends:
		return len(m.businessDashboard.Trends)
	default:
		return 0
	}
}

func (m Model) listState(screen Screen) ListState {
	state := m.listStates[screen]
	if state.PageSize <= 0 {
		state.PageSize = maxInt(1, m.contentHeight()-3)
	}
	return state
}

func (m *Model) setListState(screen Screen, state ListState) {
	length := m.businessListLen(screen)
	if length == 0 {
		state.Cursor, state.Offset = 0, 0
	} else {
		state.Cursor = clampInt(state.Cursor, 0, length-1)
		visible := m.businessVisibleItems(screen)
		if state.Cursor < state.Offset {
			state.Offset = state.Cursor
		}
		if state.Cursor >= state.Offset+visible {
			state.Offset = state.Cursor - visible + 1
		}
		state.Offset = clampInt(state.Offset, 0, maxInt(0, length-visible))
	}
	m.listStates[screen] = state
	if screen == ScreenPriorities || screen == ScreenDivergences {
		m.selected, m.offset = state.Cursor, state.Offset
	}
}

func (m Model) businessVisibleItems(screen Screen) int {
	visible := maxInt(1, m.contentHeight()-3)
	if screen == ScreenPriorities && m.width < 100 {
		visible = maxInt(1, visible/2)
	}
	return visible
}

func (m *Model) moveBusinessCursor(key string) {
	state := m.listState(m.screen)
	page := m.businessVisibleItems(m.screen)
	switch key {
	case "up", "k":
		state.Cursor--
	case "down", "j":
		state.Cursor++
	case "home", "g":
		state.Cursor = 0
	case "end", "G":
		state.Cursor = m.businessListLen(m.screen) - 1
	case "pgup":
		state.Cursor -= page
	case "pgdown":
		state.Cursor += page
	default:
		return
	}
	m.setListState(m.screen, state)
}

func (m Model) selectedBusinessID(screen Screen) string {
	state := m.listState(screen)
	switch screen {
	case ScreenPriorities:
		if state.Cursor < len(m.businessDashboard.Priorities) {
			return strconv.FormatInt(m.businessDashboard.Priorities[state.Cursor].BitrixDealID, 10)
		}
	case ScreenDivergences:
		if state.Cursor < len(m.businessDashboard.Divergences) {
			return fmt.Sprintf("%d:%s", m.businessDashboard.Divergences[state.Cursor].BitrixDealID, m.businessDashboard.Divergences[state.Cursor].Type)
		}
	case ScreenFunnel:
		if state.Cursor < len(m.businessDashboard.Funnel.Stages) {
			return m.businessDashboard.Funnel.Stages[state.Cursor].Stage
		}
	case ScreenObjections:
		if state.Cursor < len(m.businessDashboard.Objections) {
			return m.businessDashboard.Objections[state.Cursor].Category
		}
	case ScreenAgents:
		if state.Cursor < len(m.businessDashboard.Agents) {
			return m.businessDashboard.Agents[state.Cursor].Assignee
		}
	case ScreenTrends:
		if state.Cursor < len(m.businessDashboard.Trends) {
			return m.businessDashboard.Trends[state.Cursor].Month
		}
	}
	return ""
}

func (m *Model) restoreBusinessSelection(screen Screen, id string) {
	state := m.listState(screen)
	if id != "" {
		for i := 0; i < m.businessListLen(screen); i++ {
			state.Cursor = i
			m.setListState(screen, state)
			if m.selectedBusinessID(screen) == id {
				return
			}
		}
	}
	m.setListState(screen, state)
}

func (m *Model) pushNavigation() {
	if len(m.navigation) > 0 && m.navigation[len(m.navigation)-1].Screen == m.screen {
		return
	}
	m.navigation = append(m.navigation, navigationEntry{Screen: m.screen, Filters: m.businessFilters})
}

func (m Model) openBusinessSelection() (tea.Model, tea.Cmd) {
	state := m.listState(m.screen)
	if m.businessListLen(m.screen) == 0 {
		return m, nil
	}
	switch m.screen {
	case ScreenPriorities:
		m.pushNavigation()
		return m.beginRead(retryDetail, m.businessDashboard.Priorities[state.Cursor].BitrixDealID, 0, false)
	case ScreenDivergences:
		m.pushNavigation()
		return m.beginRead(retryDetail, m.businessDashboard.Divergences[state.Cursor].BitrixDealID, 0, false)
	case ScreenFunnel:
		m.businessFilters.Stage = m.businessDashboard.Funnel.Stages[state.Cursor].Stage
		m.feedback = "Filtro de etapa aplicado"
		return m.loadBusiness(ScreenPriorities)
	case ScreenObjections:
		item := m.businessDashboard.Objections[state.Cursor]
		if len(item.ExampleDealIDs) > 0 {
			m.pushNavigation()
			return m.beginRead(retryDetail, item.ExampleDealIDs[0], 0, false)
		}
	case ScreenAgents:
		m.businessFilters.Assignee = m.businessDashboard.Agents[state.Cursor].Assignee
		m.feedback = "Filtro de responsável aplicado"
		return m.loadBusiness(ScreenPriorities)
	}
	return m, nil
}

func (m *Model) cycleSort(screen Screen) {
	state := m.listState(screen)
	fields := map[Screen][]string{
		ScreenPriorities:  {"prioridade", "valor", "atualização", "dias parado", "score", "confiança"},
		ScreenDivergences: {"tipo", "data", "confiança", "negócio"},
		ScreenAgents:      {"negócios", "conversão", "ganhos", "perdidos", "pipeline"},
	}[screen]
	if len(fields) == 0 {
		return
	}
	index := 0
	for i, field := range fields {
		if field == state.SortField {
			index = i
			break
		}
	}
	if state.SortField == fields[index] {
		if !state.SortDesc {
			state.SortDesc = true
		} else {
			index = (index + 1) % len(fields)
			state.SortField, state.SortDesc = fields[index], false
		}
	} else {
		state.SortField = fields[0]
	}
	m.listStates[screen] = state
	m.sortBusiness(screen, state)
}

func (m *Model) sortBusiness(screen Screen, state ListState) {
	selected := m.selectedBusinessID(screen)
	lessDirection := func(less bool) bool {
		if state.SortDesc {
			return !less
		}
		return less
	}
	switch screen {
	case ScreenPriorities:
		sort.SliceStable(m.businessDashboard.Priorities, func(i, j int) bool {
			a, b := m.businessDashboard.Priorities[i], m.businessDashboard.Priorities[j]
			var less bool
			switch state.SortField {
			case "valor":
				less = a.Value.Value.MinorUnits < b.Value.Value.MinorUnits
			case "atualização":
				less = a.UpdatedAt.Before(b.UpdatedAt)
			case "dias parado":
				less = a.StaleDays < b.StaleDays
			case "score":
				less = a.AuditScore < b.AuditScore
			case "confiança":
				less = a.Confidence < b.Confidence
			default:
				less = a.PriorityScore < b.PriorityScore
			}
			if a.BitrixDealID == b.BitrixDealID {
				return false
			}
			return lessDirection(less)
		})
	case ScreenDivergences:
		sort.SliceStable(m.businessDashboard.Divergences, func(i, j int) bool {
			a, b := m.businessDashboard.Divergences[i], m.businessDashboard.Divergences[j]
			var less bool
			switch state.SortField {
			case "data":
				less = a.Date.Before(b.Date)
			case "confiança":
				less = a.Confidence < b.Confidence
			case "negócio":
				less = a.BitrixDealID < b.BitrixDealID
			default:
				less = a.Type < b.Type
			}
			return lessDirection(less)
		})
	case ScreenAgents:
		sort.SliceStable(m.businessDashboard.Agents, func(i, j int) bool {
			a, b := m.businessDashboard.Agents[i], m.businessDashboard.Agents[j]
			less := a.Deals < b.Deals
			switch state.SortField {
			case "ganhos":
				less = a.Won < b.Won
			case "perdidos":
				less = a.Lost < b.Lost
			case "conversão":
				less = a.Conversion.Value.BasisPoints < b.Conversion.Value.BasisPoints
			case "pipeline":
				less = a.Pipeline.Value.MinorUnits < b.Pipeline.Value.MinorUnits
			}
			return lessDirection(less)
		})
	}
	m.restoreBusinessSelection(screen, selected)
}

func (m Model) handleFilterKey(key string) (tea.Model, tea.Cmd) {
	fields := 3
	switch key {
	case "up", "k", "shift+tab":
		m.filterField--
	case "down", "j", "tab":
		m.filterField++
	case "c":
		m.filterDraft = business.GlobalFilters{Period: business.PeriodAll}
		m.feedback = "Filtros limpos"
	case " ", "enter":
		switch m.filterField {
		case 0:
			m.filterDraft.Period = nextPeriod(m.filterDraft.Period)
		case 1:
			m.filterDraft.OnlyWithoutValue = !m.filterDraft.OnlyWithoutValue
		default:
			m.businessFilters = m.filterDraft
			m.filterOpen = false
			m.feedback = "Filtros aplicados"
			return m.loadBusiness(m.screen)
		}
	}
	m.filterField = clampInt(m.filterField, 0, fields-1)
	return m, nil
}

func (m Model) beginCSVExport() (tea.Model, tea.Cmd) {
	if m.csvExporter == nil || m.exporting {
		return m, nil
	}
	supported := m.screen == ScreenPriorities || m.screen == ScreenDivergences || m.screen == ScreenCRMQuality || m.screen == ScreenObjections || m.screen == ScreenAgents
	if !supported {
		return m, nil
	}
	m.requestSequence++
	request, target, exporter, data := m.requestSequence, m.screen, m.csvExporter, m.businessDashboard
	m.exporting = true
	m.feedback = "Exportando..."
	return m, func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		var path string
		var err error
		switch target {
		case ScreenPriorities:
			path, err = exporter.Priorities(ctx, data.Priorities)
		case ScreenDivergences:
			path, err = exporter.Divergences(ctx, data.Divergences)
		case ScreenCRMQuality:
			path, err = exporter.CRMQuality(ctx, data.CRMQuality)
		case ScreenObjections:
			path, err = exporter.Objections(ctx, data.Objections)
		case ScreenAgents:
			path, err = exporter.Agents(ctx, data.Agents)
		}
		return csvMsg{path: path, target: target, request: request, err: err}
	}
}

func boolLabel(value bool) string {
	if value {
		return "Sim"
	}
	return "Não"
}
func divergenceLabel(value business.DivergenceType) string {
	labels := map[business.DivergenceType]string{business.AISaleCRMOpen: "IA indica venda, CRM aberto", business.CRMLostAINegotiating: "CRM perdido, conversa em negociação", business.PossibleReceiptNotWon: "Possível comprovante sem fechamento", business.PaymentTextDetectorConflict: "Texto indica pagamento, detector não confirmou anexo", business.PaymentEvidenceWithoutValue: "Evidência de pagamento sem valor"}
	if label := labels[value]; label != "" {
		return label
	}
	return strings.ReplaceAll(string(value), "_", " ")
}
func clampInt(v, low, high int) int {
	if high < low {
		return low
	}
	if v < low {
		return low
	}
	if v > high {
		return high
	}
	return v
}
func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}
