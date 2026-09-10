package tui

import (
	"fmt"
	"os"
	"sort"
	"strings"
	"time"

	"github.com/charmbracelet/lipgloss"
	business "github.com/portfolio/auditor-ia/internal/business/domain"
)

var titleStyle = func() lipgloss.Style {
	style := lipgloss.NewStyle().Bold(true)
	if os.Getenv("NO_COLOR") == "" {
		style = style.Foreground(lipgloss.Color("86"))
	}
	return style
}()

func (m Model) View() string {
	if m.width > 0 && (m.width < 70 || m.height < 20) {
		return "Terminal muito pequeno. Recomendado: pelo menos 80x24.\nRedimensione para continuar.\n\nq sair"
	}
	if m.loading && m.screen != ScreenAuditProgress {
		label := "Carregando..."
		if m.active.refresh {
			label = "Recarregando dados..."
		}
		return m.frame(m.spinner.View()+" "+label, "Esc cancelar/voltar · q sair")
	}
	switch m.screen {
	case ScreenMenu:
		return m.viewMenu()
	case ScreenAuditProgress:
		return m.viewProgress()
	case ScreenAuditResult:
		return m.viewAuditResult()
	case ScreenSummary:
		return m.viewSummary()
	case ScreenFindings:
		return m.viewFindings()
	case ScreenDealSearch:
		return m.viewSearch()
	case ScreenDealDetail:
		return m.scroll(m.viewDetail())
	case ScreenConversationAnalysis:
		return m.scroll(m.viewAnalysis(false))
	case ScreenRawResponse:
		return m.scroll(m.viewAnalysis(true))
	case ScreenBatchImport:
		return m.frame(m.viewBatchImport(), "Enter importar · Esc voltar · q sair")
	case ScreenBatchProgress:
		return m.frame(m.viewBatchProgress(), m.batchFooter())
	case ScreenExecutive, ScreenFunnel, ScreenPriorities, ScreenDivergences, ScreenCRMQuality, ScreenConversationQuality, ScreenObjections, ScreenAgents, ScreenTrends, ScreenAIQuality, ScreenHelp:
		if m.filterOpen {
			return m.frame(m.viewFilters(), "↑/↓ campos · Enter alterar/aplicar · c limpar · Esc cancelar")
		}
		return m.frame(m.viewBusiness(), m.businessFooter())
	case ScreenError:
		return m.viewError()
	}
	return ""
}
func (m Model) header() string {
	header := titleStyle.Render("AUDITOR IA — BITRIX24")
	if m.screen >= ScreenExecutive && m.screen <= ScreenAIQuality {
		header += "\n" + screenName(m.screen) + fmt.Sprintf(" · amostra %d", m.businessDashboard.Executive.Sample)
		updated := "—"
		if !m.updatedAt.IsZero() {
			updated = m.updatedAt.Format("15:04:05")
		}
		header += "\nPeríodo: " + string(m.businessFilters.Period) + " · atualizado " + updated
		if filters := m.businessFilters.ActiveSummary(); filters != "nenhum" {
			header += "\nFiltros: " + filters
		}
	} else if !m.updatedAt.IsZero() {
		header += "\nDados atualizados às " + m.updatedAt.Format("15:04:05")
	}
	return header
}

func (m Model) frame(content, footer string) string {
	header := m.header()
	height := m.height - strings.Count(header, "\n") - strings.Count(footer, "\n") - 2
	if height < 1 {
		height = 1
	}
	lines := strings.Split(content, "\n")
	for i := range lines {
		lines[i] = truncateVisual(lines[i], maxInt(1, m.width))
	}
	offset := m.offset
	if m.isBusinessList(m.screen) {
		offset = m.listState(m.screen).Offset
		if m.screen == ScreenPriorities && m.width < 100 {
			offset = 3 + offset*2
		}
	}
	offset = clampInt(offset, 0, maxInt(0, len(lines)-height))
	end := clampInt(offset+height, 0, len(lines))
	body := strings.Join(lines[offset:end], "\n")
	return header + "\n" + body + "\n" + footer
}
func (m Model) viewMenu() string {
	items := []string{"Visão executiva", "Funil comercial", "Oportunidades prioritárias", "Divergências CRM × IA", "Qualidade do CRM", "Análise das conversas", "Objeções comerciais", "Desempenho da equipe", "Tendências", "Qualidade da IA", "Auditorias e achados", "Buscar negócio", "Ajuda", "Importar lista para auditoria em lote"}
	keys := []string{"1", "2", "3", "4", "5", "6", "7", "8", "9", "0", "A", "B", "C", "L"}
	var b strings.Builder
	b.WriteString(m.header() + "\n\n")
	for i, item := range items {
		cursor := " "
		if i == m.menuIndex {
			cursor = ">"
		}
		fmt.Fprintf(&b, "%s [%s] %s\n", cursor, keys[i], item)
	}
	b.WriteString("\n↑/↓ ou j/k navegar · Enter selecionar · q sair")
	return b.String()
}

func (m Model) viewBatchImport() string {
	status := ""
	if m.batchLoading {
		status = "\n\n" + m.spinner.View() + " Criando lote e persistindo itens..."
	}
	if m.err != nil {
		status = "\n\nErro: " + m.err.Error()
	}
	return "IMPORTAR AUDITORIA EM LOTE\n\nCole IDs separados por vírgula, ponto e vírgula ou espaço.\nPara importar TXT/CSV, informe @ seguido do caminho completo.\nO arquivo pode conter o cabeçalho bitrix_deal_id e até 10.000 IDs.\n\n" + m.batchInput.View() + status
}
func (m Model) viewBatchProgress() string {
	b := m.batch
	percent := 0.0
	if b.Total > 0 {
		percent = float64(b.Completed+b.Failed) * 100 / float64(b.Total)
	}
	barWidth := 20
	filled := int(percent * float64(barWidth) / 100)
	if filled > barWidth {
		filled = barWidth
	}
	message := ""
	if m.err != nil {
		message = "\nErro de atualização: " + m.err.Error()
	}
	if m.batchLoading {
		message = "\n" + m.spinner.View() + " Enviando cancelamento..."
	}
	return fmt.Sprintf("PROCESSAMENTO EM LOTE #%d\n\n[%s%s] %.2f%%\n\nStatus: %s\nTotal: %d\nPendentes: %d\nProcessando: %d\nConcluídos: %d\nFalhos: %d\n\nO progresso está persistido no PostgreSQL.%s", b.ID, strings.Repeat("█", filled), strings.Repeat("░", barWidth-filled), percent, b.Status, b.Total, b.Pending, b.Processing, b.Completed, b.Failed, message)
}
func (m Model) batchFooter() string {
	parts := []string{"r atualizar"}
	if !isBatchTerminal(m.batch.Status) {
		parts = append(parts, "c cancelar")
	}
	parts = append(parts, "Esc voltar", "q sair")
	footer := strings.Join(parts, " · ")
	if m.feedback != "" {
		footer = m.feedback + "\n" + footer
	}
	if m.crmSyncing && m.screen == ScreenExecutive {
		footer = fmt.Sprintf("%s Sincronização: negócios %d · atividades %d · marcadores 3264 %d\n%s", m.spinner.View(), m.crmSyncProgress.DealsCollected, m.crmSyncProgress.ActivitiesCollected, m.crmSyncProgress.Markers3264Discovered, footer)
	}
	return footer
}

func (m Model) viewBusiness() string {
	d := m.businessDashboard
	var b strings.Builder
	switch m.screen {
	case ScreenExecutive:
		e := d.Executive
		fmt.Fprintf(&b, "VISÃO EXECUTIVA\n\nCRM Health: %s\nConclusão: %s\nScore médio: %s\n\nNegociações: %d\nNão vendas: %d\nIndeterminados: %d\nPossíveis comprovantes: %d\nDivergências: %d\n\nCanal 3264 — distribuição e perda:\n", metricInt(d.CRMQuality.HealthScore), metricPercentage(e.CompletionRate), metricString(e.AverageScore), e.Results["em negociação"], e.Results["não venda"], e.Results["indeterminado"], e.PossibleReceipts, len(d.Divergences))
		if len(d.Channel3264) == 0 {
			b.WriteString("Nenhum negócio localizado.\n")
		} else {
			for _, item := range d.Channel3264 {
				fmt.Fprintf(&b, "%s (%s): %d negócio(s), %d perdido(s), perda %s\n", item.Assignee, item.Area, item.Deals, item.Lost, metricPercentage(item.LossRate))
			}
		}
		fmt.Fprintf(&b, "\nQualidade dos dados:\nSem valor: %d\nSem conversa: %d\nVazias: %d\nAcesso negado: %d\n\nPipeline: %s\n\n%s", e.WithoutValue, e.WithoutConversation, e.EmptyConversation, e.AccessDenied, metricMoney(e.Pipeline), m.viewCRMStatistics())
	case ScreenFunnel:
		fmt.Fprintf(&b, "FUNIL COMERCIAL — amostra: %d\n\nETAPA | NEGÓCIOS | PARADOS | VALOR\n", d.Funnel.Sample)
		for i, stage := range d.Funnel.Stages {
			fmt.Fprintf(&b, "%s %-20s | %8d | %7d | %s\n", listCursor(m, ScreenFunnel, i), truncate(stage.Stage, 20), stage.Deals, stage.Stale, metricMoney(stage.Value))
		}
		fmt.Fprintf(&b, "\nGanhos: %d  Perdidos: %d  Taxa de perda: %s\nValor esperado: %s", d.Funnel.Won, d.Funnel.Lost, metricPercentage(d.Funnel.LossRate), metricMoney(d.Funnel.WeightedValue))
	case ScreenPriorities:
		fmt.Fprintf(&b, "OPORTUNIDADES PRIORITÁRIAS — %d item(ns)\n\nPRI | NEGÓCIO | ETAPA | RESULTADO FINAL | COMPROVANTE | DIAS\n", len(d.Priorities))
		for i, p := range d.Priorities {
			cursor := " "
			if i == m.listState(ScreenPriorities).Cursor {
				cursor = ">"
			}
			if m.width < 100 {
				fmt.Fprintf(&b, "%s [%d] #%d — %s\n  %s · comprovante %s · parado %s\n", cursor, p.PriorityScore, p.BitrixDealID, truncate(p.Title, maxInt(10, m.width-18)), truncate(p.CommercialResult(), 18), boolLabel(p.PossibleReceipt), pluralDays(p.StaleDays))
				continue
			}
			fmt.Fprintf(&b, "%s %3d | %-7d | %-14s | %-14s | %-11s | %d\n", cursor, p.PriorityScore, p.BitrixDealID, truncate(p.Stage, 14), truncate(p.CommercialResult(), 14), boolLabel(p.PossibleReceipt), p.StaleDays)
		}
	case ScreenDivergences:
		fmt.Fprintf(&b, "DIVERGÊNCIAS CRM × IA — %d caso(s)\n\nTIPO | NEGÓCIO | CRM | IA | CONFIANÇA\n", len(d.Divergences))
		for i, v := range d.Divergences {
			fmt.Fprintf(&b, "%s %-38s | %-7d | %-3s | %-14s | %s\n", listCursor(m, ScreenDivergences, i), truncate(divergenceLabel(v.Type), 38), v.BitrixDealID, v.CRMState, truncate(v.ConversationState, 14), v.Confidence)
		}
	case ScreenCRMQuality:
		q := d.CRMQuality
		fmt.Fprintf(&b, "QUALIDADE DO CRM — amostra: %d\n\nCRM Health Score: %s\nSem valor: %d\nSem responsável: indisponível quando assigned_by_id não foi sincronizado\nParados 7/15/30 dias: %d / %d / %d\nSem conversa: %d\nConversa vazia: %d\nAcesso negado: %d\nAnálise indeterminada: %d\nDiferença mensagens analisadas/persistidas: %d\n\n%s", q.Sample, metricInt(q.HealthScore), q.WithoutValue, q.Stale7, q.Stale15, q.Stale30, q.WithoutConversation, q.EmptyConversation, q.AccessDenied, q.Indeterminate, q.MessageCountMismatch, q.CoverageNote)
	case ScreenConversationQuality:
		q := d.Conversations
		fmt.Fprintf(&b, "ANÁLISE DAS CONVERSAS — amostra: %d\n\nAVAILABLE: %d  NO_CONVERSATION: %d  EMPTY: %d  ACCESS_DENIED: %d\nMensagens coletadas: %d  Analisadas: %d  Persistidas: %d\nQualidade: boa=%d regular=%d ruim=%d indeterminada=%d\nConfiança: alta=%d média=%d baixa=%d\nPossíveis comprovantes: %d\n\n⚠ %s", q.Sample, q.Statuses["AVAILABLE"], q.Statuses["NO_CONVERSATION"], q.Statuses["EMPTY_CONVERSATION"], q.Statuses["ACCESS_DENIED"], q.MessagesCollected, q.MessagesAnalyzed, q.MessagesPersisted, q.Quality["boa"], q.Quality["regular"], q.Quality["ruim"], q.Quality["indeterminada"], q.Confidence["alta"], q.Confidence["média"], q.Confidence["baixa"], q.PossibleReceipts, q.CoverageAlert)
	case ScreenObjections:
		fmt.Fprintf(&b, "OBJEÇÕES COMERCIAIS — amostra: %d\n\nCATEGORIA | QUANTIDADE | PERCENTUAL\n", d.Conversations.Sample)
		for i, o := range d.Objections {
			fmt.Fprintf(&b, "%s %-22s | %10d | %s\n", listCursor(m, ScreenObjections, i), o.Label, o.Count, metricPercentage(o.Percentage))
		}
	case ScreenAgents:
		b.WriteString("DESEMPENHO DA EQUIPE\n\nRESPONSÁVEL | ÁREA | NEGÓCIOS | ABERTOS | GANHOS | PERDIDOS | CONVERSÃO\n")
		for i, a := range d.Agents {
			fmt.Fprintf(&b, "%s %-18s | %-7s | %8d | %7d | %6d | %8d | %s\n", listCursor(m, ScreenAgents, i), truncate(a.Assignee, 18), a.Area, a.Deals, a.Open, a.Won, a.Lost, metricPercentage(a.Conversion))
		}
		b.WriteString("\nIndicadores dependem da qualidade do registro no CRM e devem ser usados para melhoria de processo, não como avaliação isolada de pessoas.")
	case ScreenTrends:
		b.WriteString("TENDÊNCIAS — últimos 12 meses com dados\n\nMÊS | NOVOS | GANHOS | PERDIDOS | CONVERSÃO | RECEITA\n")
		var values []int
		for i, trend := range d.Trends {
			fmt.Fprintf(&b, "%s %s | %5d | %6d | %8d | %s | %s\n", listCursor(m, ScreenTrends, i), trend.Month, trend.NewDeals, trend.Won, trend.Lost, metricPercentage(trend.Conversion), metricMoney(trend.Revenue))
			values = append(values, trend.NewDeals)
		}
		fmt.Fprintf(&b, "\nNovos negócios %s", business.Sparkline(values))
	case ScreenAIQuality:
		q := d.AIQuality
		fmt.Fprintf(&b, "QUALIDADE DA IA — amostra: %d\n\nConcluídas: %d  Falhas: %d  Sucesso: %s\nPossíveis comprovantes: %d  Divergências: %d\nModelos: %v\nPrompts: %v\nFalhas seguras: %v\nStatus de conversa: %v\nDuração p50/p95/máxima: indisponível — duração de inferência não persistida separadamente.\nRaw response permanece disponível apenas no detalhe autorizado.", q.Sample, q.Completed, q.Failed, metricPercentage(q.SuccessRate), q.PossibleReceipts, q.Divergences, q.Models, q.PromptVersions, q.FailureCategories, q.ConversationStatuses)
	case ScreenHelp:
		b.WriteString("AJUDA\n\n1–9, 0, A–C: abrir telas pelo menu\nL: importar lista para auditoria em lote\n↑/↓ ou j/k: navegar\nEnter: abrir item\nEsc: voltar\nq: sair\nHome/g, End/G, PgUp/PgDown: rolagem\nr: recarregar\nd: alternar período\nc: limpar filtros\n/: buscar negócio\np: PDF executivo na Visão executiva ou PDF individual no detalhe\n\nGrafana: observabilidade técnica. TUI/API: inteligência comercial.")
	}
	return b.String()
}

func metricPercentage(v business.MetricValue[business.Percentage]) string {
	if !v.Available {
		return "indisponível: " + v.Reason
	}
	return v.Value.String()
}

func (m Model) businessFooter() string {
	parts := []string{}
	if m.isBusinessList(m.screen) {
		parts = append(parts, "↑/↓ navegar")
		if m.businessListLen(m.screen) > 0 && m.screen != ScreenTrends {
			parts = append(parts, "Enter abrir")
		}
		if m.screen == ScreenPriorities || m.screen == ScreenDivergences || m.screen == ScreenAgents {
			parts = append(parts, "s ordenar")
		}
	} else {
		parts = append(parts, "↑/↓ rolar", "PgUp/PgDown", "Home/End")
	}
	parts = append(parts, "f filtros")
	if m.csvExporter != nil && (m.screen == ScreenPriorities || m.screen == ScreenDivergences || m.screen == ScreenCRMQuality || m.screen == ScreenObjections || m.screen == ScreenAgents) {
		parts = append(parts, "e exportar")
	}
	if m.screen == ScreenExecutive && m.businessPDFExporter != nil {
		if m.businessPDFGenerating {
			parts = append(parts, "gerando PDF...")
		} else {
			parts = append(parts, "p PDF executivo")
		}
	}
	if m.screen == ScreenExecutive && m.crmSyncer != nil {
		if m.crmSyncing {
			parts = append(parts, "x cancelar sincronização")
		} else {
			parts = append(parts, "U sincronizar CRM")
		}
	}
	parts = append(parts, "Esc voltar")
	footer := strings.Join(parts, " · ")
	if m.feedback != "" {
		footer = m.feedback + "\n" + footer
	}
	return footer
}

func (m Model) viewCRMStatistics() string {
	stats := m.businessDashboard.CRMStatistics
	if !stats.Available {
		return "ESTATÍSTICAS GLOBAIS DO CRM\n\nIndisponível: " + fallbackText(stats.UnavailableReason, "nenhuma sincronização concluída")
	}
	var b strings.Builder
	b.WriteString("ESTATÍSTICAS GLOBAIS DO CRM\n\n")
	fmt.Fprintf(&b, "Negócios no CRM: %d\nVendas concluídas: %d\nNegócios perdidos: %d\nÚltimo snapshot: %s\nÚltima tentativa: %s (%s)\n", stats.TotalDeals, stats.WonDeals, stats.LostDeals, stats.LastSyncAt.In(saoPauloLocation()).Format("02/01/2006 15:04"), stats.LastAttemptAt.In(saoPauloLocation()).Format("02/01/2006 15:04"), stats.LastStatus)
	if stats.Stale || stats.CoverageNote != "" {
		fmt.Fprintf(&b, "Aviso de cobertura: %s\n", fallbackText(stats.CoverageNote, "dados desatualizados"))
	}
	b.WriteString("\nNEGÓCIOS POR RESPONSÁVEL\n\n")
	max := 0
	for _, item := range stats.ByAssignee {
		if item.Count > max {
			max = item.Count
		}
	}
	for _, item := range stats.ByAssignee {
		fmt.Fprintf(&b, "%-24s %s %d\n", truncate(item.Name, 24), proportionalBar(item.Count, max, maxInt(4, min(24, m.width-42))), item.Count)
	}
	b.WriteString("\nNEGÓCIOS CRIADOS — ÚLTIMOS 12 MESES\n\n")
	max = 0
	for _, item := range stats.ByMonth {
		if item.Count > max {
			max = item.Count
		}
	}
	for _, item := range stats.ByMonth {
		fmt.Fprintf(&b, "%-9s %s %d\n", monthLabel(item.Month), proportionalBar(item.Count, max, maxInt(4, min(28, m.width-28))), item.Count)
	}
	b.WriteString("\nRESULTADOS POR MÊS DE CRIAÇÃO\n\n")
	b.WriteString("Mês       Concluídas  Perdidas\n")
	for _, item := range stats.OutcomesByMonth {
		fmt.Fprintf(&b, "%-9s %10d %9d\n", monthLabel(item.Month), item.Won, item.Lost)
	}
	b.WriteString("\nDISTRIBUIÇÃO DO CANAL 3264\n\n")
	if !stats.Marker3264Available {
		b.WriteString(fallbackText(stats.MarkerUnavailable, "Indisponível") + "\n")
		return strings.TrimRight(b.String(), "\n")
	}
	fmt.Fprintf(&b, "Total: %d\n\n", stats.Marker3264Total)
	for _, item := range stats.Marker3264ByAssignee {
		fmt.Fprintf(&b, "%-24s %8d %6.2f%%\n", truncate(item.Name, 24), item.Count, float64(item.PercentageBasis)/100)
	}
	return strings.TrimRight(b.String(), "\n")
}

func proportionalBar(value, maximum, width int) string {
	if width < 1 {
		width = 1
	}
	filled := 0
	if maximum > 0 && value > 0 {
		filled = (value*width + maximum - 1) / maximum
	}
	return strings.Repeat("█", filled) + strings.Repeat("░", width-filled)
}

func monthLabel(value string) string {
	parsed, err := time.Parse("2006-01", value)
	if err != nil {
		return value
	}
	months := [...]string{"Jan", "Fev", "Mar", "Abr", "Mai", "Jun", "Jul", "Ago", "Set", "Out", "Nov", "Dez"}
	return fmt.Sprintf("%s/%d", months[parsed.Month()-1], parsed.Year())
}

func saoPauloLocation() *time.Location {
	location, err := time.LoadLocation("America/Sao_Paulo")
	if err != nil {
		return time.FixedZone("America/Sao_Paulo", -3*60*60)
	}
	return location
}

func fallbackText(value, fallback string) string {
	if strings.TrimSpace(value) == "" {
		return fallback
	}
	return value
}

func (m Model) viewFilters() string {
	items := []string{
		"Período: " + string(m.filterDraft.Period),
		"Somente sem valor: " + boolLabel(m.filterDraft.OnlyWithoutValue),
		"Aplicar filtros",
	}
	var b strings.Builder
	b.WriteString("FILTROS\n\n")
	for i, item := range items {
		cursor := " "
		if i == m.filterField {
			cursor = ">"
		}
		fmt.Fprintf(&b, "%s %s\n", cursor, item)
	}
	return b.String()
}

func screenName(screen Screen) string {
	names := map[Screen]string{ScreenExecutive: "Visão executiva", ScreenFunnel: "Funil comercial", ScreenPriorities: "Oportunidades prioritárias", ScreenDivergences: "Divergências CRM × IA", ScreenCRMQuality: "Qualidade do CRM", ScreenConversationQuality: "Análise das conversas", ScreenObjections: "Objeções", ScreenAgents: "Equipe", ScreenTrends: "Tendências", ScreenAIQuality: "Qualidade da IA", ScreenHelp: "Ajuda"}
	return names[screen]
}
func listCursor(m Model, screen Screen, i int) string {
	if m.listState(screen).Cursor == i {
		return ">"
	}
	return " "
}
func pluralDays(days int) string {
	if days == 1 {
		return "1 dia"
	}
	return fmt.Sprintf("%d dias", days)
}
func fallbackUI(value string) string {
	if strings.TrimSpace(value) == "" {
		return "Todos"
	}
	return value
}
func metricMoney(v business.MetricValue[business.Money]) string {
	if !v.Available {
		return "indisponível: " + v.Reason
	}
	return v.Value.String()
}
func metricString(v business.MetricValue[string]) string {
	if !v.Available {
		return "indisponível: " + v.Reason
	}
	return v.Value
}
func metricInt(v business.MetricValue[int]) string {
	if !v.Available {
		return "indisponível: " + v.Reason
	}
	return fmt.Sprintf("%d/100", v.Value)
}
func resultBars(results map[string]int, width int) string {
	keys := []string{"em negociação", "indeterminado", "não venda", "venda", "pós-venda/suporte"}
	max := 1
	for _, v := range results {
		if v > max {
			max = v
		}
	}
	barWidth := 20
	if width < 100 {
		barWidth = 10
	}
	var lines []string
	for _, k := range keys {
		n := results[k]
		filled := n * barWidth / max
		lines = append(lines, fmt.Sprintf("%-19s %s %d", k, strings.Repeat("█", filled), n))
	}
	return strings.Join(lines, "\n")
}
func (m Model) viewProgress() string {
	progress := m.spinner.View() + " Total ainda desconhecido"
	if m.progress.Total > 0 {
		percent := m.progress.Processed * 100 / m.progress.Total
		filled := percent * 16 / 100
		progress = fmt.Sprintf("[%s%s] %d/%d — %d%%", strings.Repeat("█", filled), strings.Repeat("░", 16-filled), m.progress.Processed, m.progress.Total, percent)
	}
	return fmt.Sprintf("%s\n\nAUDITORIA EM EXECUÇÃO\n\nFase: %s\n%s\nColetados: %d\nAchados: %d\nErros: %d\nTempo: %s\n\nEsc voltar · q sair", m.header(), m.progress.Phase, progress, m.progress.Collected, m.progress.Findings, m.progress.Errors, formatDuration(m.elapsed))
}
func (m Model) viewAuditResult() string {
	if m.audit == nil {
		return m.header()
	}
	counts := map[string]int{}
	rules := map[string]int{}
	for _, f := range m.audit.Findings {
		counts[string(f.Severity)]++
		rules[f.Rule]++
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\nAUDITORIA CONCLUÍDA\n\nNegócios analisados: %d\nScore: %d\nAchados: %d\n", m.header(), m.audit.ProcessedRecords, m.audit.Score.Value(), len(m.audit.Findings))
	for _, s := range []string{"CRITICAL", "HIGH", "MEDIUM", "LOW"} {
		if counts[s] > 0 {
			fmt.Fprintf(&b, "%s: %d\n", s, counts[s])
		}
	}
	b.WriteString("\nPrincipais regras:\n")
	for _, pair := range sortedCounts(rules, 5) {
		fmt.Fprintf(&b, "%s: %d\n", pair.key, pair.value)
	}
	fmt.Fprintf(&b, "\nDuração: %s\nStatus: %s\nErros: 0\n\n[Enter] Ver achados · [Esc] Voltar", formatDuration(m.elapsed), m.audit.Status)
	return b.String()
}
func (m Model) viewSummary() string {
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\nVISÃO GERAL\n\nNegócios sincronizados: %d\nNegócios analisados: %d\nAvaliações concluídas: %d\nAvaliações com erro: %d\nScore médio: %.1f\nPossíveis comprovantes: %d\n\nResultados:\n", m.header(), m.summary.DealsSynced, m.summary.DealsAnalyzed, m.summary.Completed, m.summary.Failed, m.summary.AverageScore, m.summary.PossibleReceipts)
	for _, key := range []string{"venda", "não venda", "em negociação", "pós-venda/suporte", "indeterminado"} {
		fmt.Fprintf(&b, "%s: %d\n", strings.Title(key), m.summary.Results[key])
	}
	b.WriteString("\nAchados por severidade:\n")
	for _, key := range []string{"CRITICAL", "HIGH", "MEDIUM", "LOW"} {
		fmt.Fprintf(&b, "%s: %d\n", key, m.summary.FindingsBySeverity[key])
	}
	b.WriteString("\nAchados por regra:\n")
	for _, pair := range sortedCounts(m.summary.FindingsByRule, 10) {
		fmt.Fprintf(&b, "%s: %d\n", pair.key, pair.value)
	}
	b.WriteString("\n[r] recarregar · Esc voltar · q sair")
	return m.scroll(b.String())
}
func (m Model) viewFindings() string {
	rule := m.filter.Rule
	if rule == "" {
		rule = "todas"
	}
	severity := m.filter.Severity
	if severity == "" {
		severity = "todas"
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\nACHADOS — %d registro(s)\nFiltros: severidade=%s · regra=%s", m.header(), m.page.Total, severity, rule)
	if m.filter.BitrixDealID > 0 {
		fmt.Fprintf(&b, " · negócio=%d", m.filter.BitrixDealID)
	}
	b.WriteString("\n\nSEVERIDADE | REGRA | NEGÓCIO | DESCRIÇÃO\n")
	if len(m.page.Items) == 0 {
		b.WriteString("Nenhum achado encontrado.\n")
	}
	width := m.width
	if width < 80 {
		width = 80
	}
	for i, f := range m.page.Items {
		cursor := " "
		if i == m.selected {
			cursor = ">"
		}
		line := fmt.Sprintf("%s %-8s | %-24s | %-10d | %s", cursor, f.Severity, truncate(f.Rule, 24), f.BitrixDealID, f.Description)
		b.WriteString(truncate(line, width-1) + "\n")
	}
	fmt.Fprintf(&b, "\nPágina %d/%d · ↑/↓ selecionar · PgUp/PgDown · Home/g · End/G · Enter detalhes\nf severidade · F regra · c limpar · / buscar ID · r recarregar · Esc voltar · q sair", pageNumber(m.page.Offset, m.page.Limit), pageCount(m.page.Total, m.page.Limit))
	return b.String()
}
func (m Model) viewSearch() string {
	kind := "negócio"
	if m.searchAnalysis {
		kind = "análise de conversa"
	}
	message := ""
	if m.err != nil {
		message = "\n\n" + m.err.Error()
	}
	return fmt.Sprintf("%s\n\nBUSCAR %s\n\n%s%s\n\nEnter buscar · Esc cancelar", m.header(), strings.ToUpper(kind), m.search.View(), message)
}
func (m Model) viewDetail() string {
	score := "indisponível"
	if m.detail.Score != nil {
		score = fmt.Sprintf("%.1f", *m.detail.Score)
	}
	amount := "indisponível"
	if m.detail.Amount != nil {
		amount = *m.detail.Amount + " " + m.detail.Currency
	}
	var b strings.Builder
	fmt.Fprintf(&b, "%s\n\nDETALHES DO NEGÓCIO\n", m.header())
	if m.reportGenerating {
		b.WriteString("\nGerando relatório...\n")
	}
	if m.reportPath != "" {
		fmt.Fprintf(&b, "\nRelatório salvo em: %s\n", m.reportPath)
	}
	if m.detail.AnalysisStatusMessage != "" {
		fmt.Fprintf(&b, "\nAnálise de conversa indisponível: %s\n", m.detail.AnalysisStatusMessage)
	}
	if m.detail.ConversationStatus != "" {
		fmt.Fprintf(&b, "Conversa encontrada: %s\nHistórico: %s\n", conversationFound(m.detail.ConversationStatus), conversationHistory(m.detail.ConversationStatus))
	}
	assignee := "indisponível"
	if m.detail.AssignedByID != nil {
		assignee = fmt.Sprintf("ID %d (nome indisponível)", *m.detail.AssignedByID)
	}
	updated := "indisponível"
	stale := "indisponível"
	if m.detail.UpdatedAtBitrix != nil {
		updated = m.detail.UpdatedAtBitrix.Format("02/01/2006 15:04")
		stale = fmt.Sprintf("%d", int(time.Since(*m.detail.UpdatedAtBitrix).Hours()/24))
	}
	fmt.Fprintf(&b, "\nID Bitrix: %d\nTítulo: %s\nEtapa: %s\nSituação semântica/CRM: %s\nFechado: %s\nResponsável: %s\nValor: %s\nÚltima atualização: %s\nDias parado: %s\nScore: %s\nStatus da avaliação: %s\nTrace ID: %s\nSincronizado em: %s\nStatus da conversa: %s\nMensagens: %d\nPossível comprovante: %s\nResultado provável da IA: %s\nResultado final: %s\nFonte: %s\nMotivo principal: %s\nObjeções: %s\nQualidade: %s\nConfiança: %s\nObservação: %s\nPróxima ação: %s\n\nAchados (%d):\n", m.detail.BitrixDealID, m.detail.Title, m.detail.StageID, m.detail.StageSemanticID, boolLabel(m.detail.Closed), assignee, amount, updated, stale, score, m.detail.AssessmentStatus, m.detail.TraceID, m.detail.SyncedAt.Format("02/01/2006 15:04"), m.detail.ConversationStatus, m.detail.MessageCount, boolLabel(m.detail.PossibleReceipt), m.detail.ProbableResult, m.detail.FinalResult, m.detail.ResultSource, m.detail.MainReason, m.detail.CustomerObjections, m.detail.ServiceQuality, m.detail.Confidence, m.detail.Observation, m.detail.RecommendedAction, len(m.detail.Findings))
	for _, f := range m.detail.Findings {
		fmt.Fprintf(&b, "\n[%s] %s\nRegra: %s\n", f.Severity, f.Description, f.Rule)
	}
	actions := []string{"[r] recarregar"}
	if m.detail.HasAnalysis {
		actions = append(actions, "[a] análise de conversa")
	}
	if m.detail.AssessmentID > 0 && m.reportExporter != nil {
		actions = append(actions, "[p] gerar PDF")
	}
	actions = append(actions, "↑/↓ PgUp/PgDown Home/End", "Esc voltar")
	b.WriteString("\n" + strings.Join(actions, " · "))
	return b.String()
}
func (m Model) viewAnalysis(raw bool) string {
	if raw {
		return m.header() + "\n\nRESPOSTA ORIGINAL\n\n" + m.analysis.RawResponse + "\n\nEsc voltar"
	}
	a := m.analysis
	if a.StatusMessage != "" {
		return fmt.Sprintf("%s\n\nANÁLISE DE CONVERSA INDISPONÍVEL\n\nNegócio: %d — %s\nConversa encontrada: %s\nMensagens analisadas: %d\nHistórico: %s\nResultado final: %s\nFonte: %s\n\n%s\n\n[r] recarregar · ↑/↓ PgUp/PgDown Home/End · Esc voltar", m.header(), a.BitrixDealID, a.Title, conversationFound(a.ConversationStatus), a.MessageCount, conversationHistory(a.ConversationStatus), a.FinalResult, a.ResultSource, a.StatusMessage)
	}
	help := "[r] recarregar"
	if a.RawResponse != "" {
		help += " · [v] resposta original"
	}
	return fmt.Sprintf("%s\n\nANÁLISE DE CONVERSA\n\nNegócio: %d — %s\nModelo: %s\nVersão do prompt: %s\nMensagens analisadas: %d\nPossível comprovante: %s\n\nResultado provável: %s\nResultado final: %s\nFonte: %s\nMotivo principal: %s\nObjeções: %s\nQualidade: %s\nPróxima ação: %s\nConfiança: %s\nObservação: %s\n\n%s · ↑/↓ PgUp/PgDown Home/End · Esc voltar", m.header(), a.BitrixDealID, a.Title, a.Model, a.PromptVersion, a.MessageCount, boolLabel(a.PossibleReceipt), a.ProbableResult, a.FinalResult, a.ResultSource, a.MainReason, a.CustomerObjections, a.ServiceQuality, a.RecommendedAction, a.Confidence, a.Observation, help)
}

func conversationFound(status string) string {
	if status == "NO_CONVERSATION" || status == "" {
		return "não"
	}
	return "sim"
}

func conversationHistory(status string) string {
	switch status {
	case "ACCESS_DENIED":
		return "acesso negado pelo Bitrix"
	case "EMPTY_CONVERSATION":
		return "acessível, sem mensagens humanas analisáveis"
	case "AVAILABLE":
		return "disponível"
	default:
		return "não localizado"
	}
}
func (m Model) viewError() string {
	help := "[Esc] voltar · [q] sair"
	if m.retry.kind != retryNone {
		help = "[r] tentar novamente · " + help
	}
	return fmt.Sprintf("%s\n\nERRO\n%s\n\n%s", m.header(), m.err, help)
}
func (m Model) scroll(content string) string {
	lines := strings.Split(content, "\n")
	height := m.contentHeight()
	max := len(lines) - height
	if max < 0 {
		max = 0
	}
	offset := m.offset
	if offset > max {
		offset = max
	}
	end := offset + height
	if end > len(lines) {
		end = len(lines)
	}
	return strings.Join(lines[offset:end], "\n")
}

type countPair struct {
	key   string
	value int
}

func sortedCounts(values map[string]int, limit int) []countPair {
	out := make([]countPair, 0, len(values))
	for k, v := range values {
		out = append(out, countPair{k, v})
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].value == out[j].value {
			return out[i].key < out[j].key
		}
		return out[i].value > out[j].value
	})
	if len(out) > limit {
		out = out[:limit]
	}
	return out
}
func truncate(value string, width int) string {
	if width < 2 {
		return ""
	}
	r := []rune(value)
	if len(r) <= width {
		return value
	}
	return string(r[:width-1]) + "…"
}
func truncateVisual(value string, width int) string {
	if width < 2 {
		return ""
	}
	if lipgloss.Width(value) <= width {
		return value
	}
	var b strings.Builder
	for _, r := range value {
		candidate := b.String() + string(r) + "…"
		if lipgloss.Width(candidate) > width {
			break
		}
		b.WriteRune(r)
	}
	return b.String() + "…"
}
func pageNumber(offset, limit int) int {
	if limit <= 0 {
		return 1
	}
	return offset/limit + 1
}
func pageCount(total, limit int) int {
	if total == 0 || limit <= 0 {
		return 1
	}
	return (total + limit - 1) / limit
}
func formatDuration(value time.Duration) string {
	return fmt.Sprintf("%02dm%02ds", int(value.Minutes()), int(value.Seconds())%60)
}
