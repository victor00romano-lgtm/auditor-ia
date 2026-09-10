package report

import (
	"bytes"
	"context"
	"fmt"
	"sort"
	"strings"
	"unicode/utf8"

	"github.com/phpdave11/gofpdf"
	"github.com/portfolio/auditor-ia/internal/business/domain"
)

type ExecutivePDFRenderer struct{}

type pdfDocument struct {
	pdf       *gofpdf.Fpdf
	translate func(string) string
}

func (ExecutivePDFRenderer) Render(ctx context.Context, report domain.ExecutiveReport) ([]byte, error) {
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	pdf := gofpdf.New("P", "mm", "A4", "")
	doc := pdfDocument{pdf: pdf, translate: pdf.UnicodeTranslatorFromDescriptor("")}
	pdf.SetMargins(18, 18, 18)
	pdf.SetAutoPageBreak(true, 17)
	pdf.SetTitle(doc.t("Auditor IA - Relatório executivo empresarial"), false)
	pdf.SetAuthor("Auditor IA", false)
	pdf.SetHeaderFunc(func() {
		if pdf.PageNo() == 1 {
			return
		}
		pdf.SetY(8)
		pdf.SetFont("Arial", "B", 8)
		pdf.SetTextColor(31, 78, 121)
		pdf.CellFormat(0, 5, doc.t("AUDITOR IA - RELATÓRIO EXECUTIVO EMPRESARIAL"), "", 0, "L", false, 0, "")
		pdf.SetDrawColor(210, 218, 226)
		pdf.Line(18, 14, 192, 14)
		pdf.SetTextColor(35, 43, 52)
	})
	pdf.SetFooterFunc(func() {
		pdf.SetY(-11)
		pdf.SetDrawColor(210, 218, 226)
		pdf.Line(18, pdf.GetY(), 192, pdf.GetY())
		pdf.SetY(-9)
		pdf.SetFont("Arial", "", 7.5)
		pdf.SetTextColor(90, 100, 110)
		text := fmt.Sprintf("Gerado em %s | Página %d", report.GeneratedAt.Format("02/01/2006 15:04"), pdf.PageNo())
		pdf.CellFormat(0, 5, doc.t(text), "", 0, "C", false, 0, "")
	})

	doc.cover(report)
	doc.executiveOverview(report)
	doc.crmStatistics(report)
	doc.crmQuality(report)
	doc.priorities(report)
	doc.divergences(report)
	doc.conversationsAndObjections(report)
	doc.team(report)
	doc.recommendationsAndLimits(report)

	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var buffer bytes.Buffer
	if err := pdf.Output(&buffer); err != nil {
		return nil, fmt.Errorf("renderizar PDF executivo: %w", err)
	}
	return buffer.Bytes(), nil
}

func (d pdfDocument) crmStatistics(report domain.ExecutiveReport) {
	stats := report.Dashboard.CRMStatistics
	d.newSection("Estatísticas globais do CRM")
	if !stats.Available {
		d.note("Indisponível: " + fallback(stats.UnavailableReason, "nenhuma sincronização global concluída"))
		return
	}
	d.paragraph(fmt.Sprintf("Negócios no CRM: %d\nVendas concluídas: %d\nNegócios perdidos: %d\nÚltimo snapshot: %s\nÚltima tentativa: %s\nStatus: %s", stats.TotalDeals, stats.WonDeals, stats.LostDeals, stats.LastSyncAt.Format("02/01/2006 15:04"), stats.LastAttemptAt.Format("02/01/2006 15:04"), stats.LastStatus))
	if stats.CoverageNote != "" {
		d.note(stats.CoverageNote)
	}
	rows := make([][]string, 0, len(stats.ByAssignee))
	for _, item := range stats.ByAssignee {
		rows = append(rows, []string{fallback(item.Name, "Sem responsável"), fmt.Sprint(item.Count)})
	}
	d.table([]string{"Responsável", "Negócios"}, []float64{125, 45}, rows)
	rows = rows[:0]
	for _, item := range stats.ByMonth {
		rows = append(rows, []string{item.Month, fmt.Sprint(item.Count)})
	}
	d.table([]string{"Mês de criação", "Negócios"}, []float64{125, 45}, rows)
	rows = rows[:0]
	for _, item := range stats.OutcomesByMonth {
		rows = append(rows, []string{item.Month, fmt.Sprint(item.Won), fmt.Sprint(item.Lost)})
	}
	d.table([]string{"Mês de criação", "Concluídas", "Perdidas"}, []float64{90, 40, 40}, rows)
	if !stats.Marker3264Available {
		d.note(fallback(stats.MarkerUnavailable, "Canal 3264 indisponível"))
		return
	}
	d.paragraph(fmt.Sprintf("Total de negócios no canal 3264: %d", stats.Marker3264Total))
	rows = rows[:0]
	for _, item := range stats.Marker3264ByAssignee {
		rows = append(rows, []string{fallback(item.Name, "Sem responsável"), fmt.Sprint(item.Count), fmt.Sprintf("%.2f%%", float64(item.PercentageBasis)/100)})
	}
	d.table([]string{"Responsável", "Negócios 3264", "Participação"}, []float64{100, 35, 35}, rows)
}

func (d pdfDocument) cover(report domain.ExecutiveReport) {
	d.pdf.AddPage()
	d.pdf.SetFillColor(25, 66, 105)
	d.pdf.Rect(0, 0, 210, 297, "F")
	d.pdf.SetTextColor(255, 255, 255)
	d.pdf.SetY(45)
	d.pdf.SetFont("Arial", "B", 26)
	d.pdf.MultiCell(0, 12, d.t("RELATÓRIO EXECUTIVO\nEMPRESARIAL"), "", "L", false)
	d.pdf.Ln(8)
	d.pdf.SetFont("Arial", "", 14)
	d.pdf.MultiCell(0, 8, d.t("Auditoria comercial e qualidade operacional do Bitrix24"), "", "L", false)
	d.pdf.SetY(138)
	d.pdf.SetFillColor(255, 255, 255)
	d.pdf.SetTextColor(25, 66, 105)
	d.pdf.RoundedRect(18, 138, 174, 58, 3, "1234", "F")
	d.pdf.SetXY(26, 149)
	d.pdf.SetFont("Arial", "B", 11)
	d.pdf.CellFormat(50, 6, d.t("Período analisado"), "", 0, "L", false, 0, "")
	d.pdf.SetFont("Arial", "", 11)
	d.pdf.CellFormat(105, 6, d.t(periodLabel(report.Filters)), "", 1, "L", false, 0, "")
	d.pdf.SetX(26)
	d.pdf.SetFont("Arial", "B", 11)
	d.pdf.CellFormat(50, 6, d.t("Amostra"), "", 0, "L", false, 0, "")
	d.pdf.SetFont("Arial", "", 11)
	d.pdf.CellFormat(105, 6, d.t(fmt.Sprintf("%d negócios", report.Dashboard.Executive.Sample)), "", 1, "L", false, 0, "")
	d.pdf.SetX(26)
	d.pdf.SetFont("Arial", "B", 11)
	d.pdf.CellFormat(50, 6, d.t("Gerado em"), "", 0, "L", false, 0, "")
	d.pdf.SetFont("Arial", "", 11)
	d.pdf.CellFormat(105, 6, d.t(report.GeneratedAt.Format("02/01/2006 15:04")), "", 1, "L", false, 0, "")
	d.pdf.SetXY(18, 245)
	d.pdf.SetTextColor(220, 232, 242)
	d.pdf.SetFont("Arial", "", 9)
	d.pdf.MultiCell(174, 5, d.t("Documento gerencial baseado exclusivamente nos dados persistidos pelo Auditor IA. Indicadores indisponíveis são apresentados sem estimativas inventadas."), "", "L", false)
}

func (d pdfDocument) executiveOverview(report domain.ExecutiveReport) {
	d.newSection("1. Resumo executivo")
	e, q := report.Dashboard.Executive, report.Dashboard.CRMQuality
	d.cards([]card{
		{"CRM Health", metricInt(q.HealthScore), 31, 120, 76},
		{"Conclusão", metricPercentage(e.CompletionRate), 31, 120, 76},
		{"Score médio", metricString(e.AverageScore), 31, 120, 76},
		{"Amostra", fmt.Sprintf("%d", e.Sample), 31, 120, 76},
	})
	d.paragraph(fmt.Sprintf("Foram avaliados %d negócios: %d concluídos e %d com falha. A análise encontrou %d negócio(s) em negociação, %d não venda(s), %d resultado(s) indeterminado(s) e %d possível(is) comprovante(s).", e.DealsEvaluated, e.Completed, e.Failed, e.Results["em negociação"], e.Results["não venda"], e.Results["indeterminado"], e.PossibleReceipts))
	d.subheading("Indicadores financeiros")
	d.keyValue("Pipeline", metricMoney(e.Pipeline))
	d.keyValue("Receita ganha", metricMoney(e.WonRevenue))
	d.keyValue("Receita perdida", metricMoney(e.LostRevenue))
	d.keyValue("Ticket médio", metricMoney(e.AverageTicket))
	d.keyValue("Valor ponderado", metricMoney(e.WeightedValue))
	d.subheading("Distribuição dos resultados")
	results := []string{"venda", "em negociação", "não venda", "pós-venda/suporte", "indeterminado"}
	for _, name := range results {
		d.bar(name, e.Results[name], maxInt(1, e.DealsEvaluated))
	}
	d.subheading("Canal 3264 - distribuição e perda")
	if len(report.Dashboard.Channel3264) == 0 {
		d.note("Nenhum negócio com 3264 no título foi localizado para os filtros ativos.")
	} else {
		rows := make([][]string, 0, len(report.Dashboard.Channel3264))
		for _, item := range report.Dashboard.Channel3264 {
			rows = append(rows, []string{item.Assignee, item.Area, fmt.Sprint(item.Deals), fmt.Sprint(item.Lost), metricPercentage(item.LossRate)})
		}
		d.table([]string{"Responsável", "Área", "Negócios", "Perdidos", "Taxa de perda"}, []float64{62, 28, 27, 27, 30}, rows)
	}
}

func (d pdfDocument) crmQuality(report domain.ExecutiveReport) {
	d.newSection("2. Qualidade e governança do CRM")
	q := report.Dashboard.CRMQuality
	d.paragraph("A qualidade dos indicadores depende do preenchimento consistente do CRM. Os números abaixo mostram os principais pontos que reduzem a confiabilidade gerencial da base.")
	rows := [][]string{
		{"Sem valor informado", fmt.Sprint(q.WithoutValue)},
		{"Parados há mais de 7 dias", fmt.Sprint(q.Stale7)},
		{"Parados há mais de 15 dias", fmt.Sprint(q.Stale15)},
		{"Parados há mais de 30 dias", fmt.Sprint(q.Stale30)},
		{"Sem conversa localizada", fmt.Sprint(q.WithoutConversation)},
		{"Conversa vazia", fmt.Sprint(q.EmptyConversation)},
		{"Acesso negado pelo Bitrix", fmt.Sprint(q.AccessDenied)},
		{"Análise indeterminada", fmt.Sprint(q.Indeterminate)},
		{"Divergência na contagem de mensagens", fmt.Sprint(q.MessageCountMismatch)},
	}
	d.table([]string{"Indicador", "Negócios"}, []float64{139, 35}, rows)
	if strings.TrimSpace(q.CoverageNote) != "" {
		d.note(q.CoverageNote)
	}
}

func (d pdfDocument) priorities(report domain.ExecutiveReport) {
	d.newSection("3. Oportunidades prioritárias")
	items := append([]domain.PriorityOpportunity(nil), report.Dashboard.Priorities...)
	sort.SliceStable(items, func(i, j int) bool { return items[i].PriorityScore > items[j].PriorityScore })
	if len(items) > 10 {
		items = items[:10]
	}
	if len(items) == 0 {
		d.note("Nenhuma oportunidade prioritária foi identificada para os filtros ativos.")
		return
	}
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{fmt.Sprint(item.PriorityScore), fmt.Sprint(item.BitrixDealID), fallback(item.Stage, "-"), fallback(item.CommercialResult(), "indeterminado"), yesNo(item.PossibleReceipt), fallback(item.NextAction, "Revisar negócio")})
	}
	d.table([]string{"Pri", "Negócio", "Etapa", "Resultado final", "Comp.", "Próxima ação"}, []float64{12, 19, 27, 29, 17, 70}, rows)
	d.note("A prioridade combina sinais persistidos de pagamento, possível comprovante, tempo sem atualização e confiança da análise. Não representa confirmação automática de venda.")
}

func (d pdfDocument) divergences(report domain.ExecutiveReport) {
	d.newSection("4. Divergências entre CRM e IA")
	items := report.Dashboard.Divergences
	unique := map[int64]struct{}{}
	for _, item := range items {
		unique[item.BitrixDealID] = struct{}{}
	}
	d.paragraph(fmt.Sprintf("Foram encontradas %d divergências distribuídas em %d negócio(s) único(s). Um mesmo negócio pode possuir mais de um motivo de divergência.", len(items), len(unique)))
	if len(items) == 0 {
		d.note("Nenhuma divergência foi encontrada para os filtros ativos.")
		return
	}
	limit := len(items)
	if limit > 15 {
		limit = 15
	}
	rows := make([][]string, 0, limit)
	for _, item := range items[:limit] {
		rows = append(rows, []string{divergenceLabel(item.Type), fmt.Sprint(item.BitrixDealID), fallback(item.CRMState, "-"), fallback(item.ConversationState, "-"), fallback(item.Confidence, "-")})
	}
	d.table([]string{"Tipo", "Negócio", "CRM", "Conversa", "Confiança"}, []float64{69, 20, 18, 42, 25}, rows)
	if len(items) > limit {
		d.note(fmt.Sprintf("Exibidas as primeiras %d de %d divergências. Consulte a TUI ou a exportação CSV para a lista completa.", limit, len(items)))
	}
}

func (d pdfDocument) conversationsAndObjections(report domain.ExecutiveReport) {
	d.newSection("5. Conversas e objeções comerciais")
	q := report.Dashboard.Conversations
	d.subheading("Cobertura e qualidade das conversas")
	d.keyValue("Conversas disponíveis", fmt.Sprint(q.Statuses["AVAILABLE"]))
	d.keyValue("Sem conversa", fmt.Sprint(q.Statuses["NO_CONVERSATION"]))
	d.keyValue("Conversas vazias", fmt.Sprint(q.Statuses["EMPTY_CONVERSATION"]))
	d.keyValue("Acesso negado", fmt.Sprint(q.Statuses["ACCESS_DENIED"]))
	d.keyValue("Mensagens coletadas / analisadas / persistidas", fmt.Sprintf("%d / %d / %d", q.MessagesCollected, q.MessagesAnalyzed, q.MessagesPersisted))
	d.keyValue("Qualidade boa / regular / ruim", fmt.Sprintf("%d / %d / %d", q.Quality["boa"], q.Quality["regular"], q.Quality["ruim"]))
	d.keyValue("Confiança alta / média / baixa", fmt.Sprintf("%d / %d / %d", q.Confidence["alta"], q.Confidence["média"], q.Confidence["baixa"]))
	if strings.TrimSpace(q.CoverageAlert) != "" {
		d.note(q.CoverageAlert)
	}
	d.subheading("Principais objeções")
	items := report.Dashboard.Objections
	if len(items) > 10 {
		items = items[:10]
	}
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		rows = append(rows, []string{fallback(item.Label, item.Category), fmt.Sprint(item.Count), metricPercentage(item.Percentage), metricMoney(item.PotentialValue)})
	}
	if len(rows) == 0 {
		d.note("Nenhuma objeção categorizada está disponível para os filtros ativos.")
	} else {
		d.table([]string{"Categoria", "Qtd.", "%", "Valor potencial"}, []float64{77, 22, 30, 45}, rows)
	}
}

func (d pdfDocument) team(report domain.ExecutiveReport) {
	d.newSection("6. Desempenho agregado da equipe")
	items := report.Dashboard.Agents
	if len(items) == 0 {
		d.note("Não há dados de responsáveis suficientes para compor esta seção.")
		return
	}
	if len(items) > 15 {
		items = items[:15]
	}
	rows := make([][]string, 0, len(items))
	for _, item := range items {
		area := item.Area
		if area == "" {
			area = domain.AgentArea(item.Assignee)
		}
		rows = append(rows, []string{fallback(item.Assignee, "Não informado"), area, fmt.Sprint(item.Deals), fmt.Sprint(item.Open), fmt.Sprint(item.Won), fmt.Sprint(item.Lost), metricPercentage(item.Conversion)})
	}
	d.table([]string{"Responsável", "Área", "Neg.", "Abertos", "Ganhos", "Perdidos", "Conversão"}, []float64{45, 20, 17, 22, 21, 23, 26}, rows)
	d.note("Indicadores de equipe devem orientar melhorias de processo e qualidade de registro. Não devem ser usados isoladamente para avaliação individual.")
}

func (d pdfDocument) recommendationsAndLimits(report domain.ExecutiveReport) {
	d.newSection("7. Plano de ação recomendado")
	for i, item := range report.Recommendations {
		d.bullet(fmt.Sprintf("%d. %s", i+1, item))
	}
	d.subheading("Limitações e cobertura")
	limits := []string{
		"A análise considera somente os negócios sincronizados e a avaliação persistida mais recente de cada negócio.",
		"Valores, receita, pipeline e ticket médio permanecem indisponíveis quando o CRM não possui valor válido.",
		"Conversas ausentes, vazias ou com acesso negado reduzem a cobertura da IA e não são tratadas como ausência de interesse do cliente.",
		"Possível comprovante é um sinal para validação humana, não confirmação automática de pagamento ou venda.",
		"O relatório não contém conversas completas, raw_response, tokens, webhooks ou credenciais.",
	}
	for _, item := range limits {
		d.bullet(item)
	}
}

type card struct {
	label, value string
	r, g, b      int
}

func (d pdfDocument) cards(cards []card) {
	startX, y, width, gap := 18.0, d.pdf.GetY(), 41.0, 3.3
	for i, item := range cards {
		x := startX + float64(i)*(width+gap)
		d.pdf.SetFillColor(item.r, item.g, item.b)
		d.pdf.RoundedRect(x, y, width, 25, 2, "1234", "F")
		d.pdf.SetXY(x+3, y+4)
		d.pdf.SetTextColor(255, 255, 255)
		d.pdf.SetFont("Arial", "", 8)
		d.pdf.CellFormat(width-6, 5, d.t(item.label), "", 1, "L", false, 0, "")
		d.pdf.SetXY(x+3, y+11)
		d.pdf.SetFont("Arial", "B", 12)
		d.pdf.CellFormat(width-6, 7, d.t(fit(item.value, 21)), "", 0, "L", false, 0, "")
	}
	d.pdf.SetY(y + 31)
	d.pdf.SetTextColor(35, 43, 52)
}

func (d pdfDocument) newSection(title string) {
	d.pdf.AddPage()
	d.pdf.SetY(20)
	d.pdf.SetTextColor(25, 66, 105)
	d.pdf.SetFont("Arial", "B", 17)
	d.pdf.MultiCell(0, 9, d.t(title), "", "L", false)
	d.pdf.SetDrawColor(54, 139, 171)
	d.pdf.SetLineWidth(0.8)
	d.pdf.Line(18, d.pdf.GetY(), 72, d.pdf.GetY())
	d.pdf.Ln(6)
	d.pdf.SetTextColor(35, 43, 52)
}

func (d pdfDocument) subheading(text string) {
	d.ensureSpace(14)
	d.pdf.Ln(3)
	d.pdf.SetFont("Arial", "B", 11)
	d.pdf.SetTextColor(25, 66, 105)
	d.pdf.MultiCell(0, 6, d.t(text), "", "L", false)
	d.pdf.SetTextColor(35, 43, 52)
}

func (d pdfDocument) paragraph(text string) {
	d.ensureSpace(12)
	d.pdf.SetFont("Arial", "", 9.5)
	d.pdf.SetTextColor(35, 43, 52)
	d.pdf.MultiCell(0, 5, d.t(text), "", "L", false)
	d.pdf.Ln(2)
}

func (d pdfDocument) note(text string) {
	d.ensureSpace(14)
	d.pdf.SetFillColor(235, 242, 247)
	d.pdf.SetTextColor(55, 72, 88)
	d.pdf.SetFont("Arial", "I", 8.5)
	d.pdf.MultiCell(0, 5, d.t(text), "", "L", true)
	d.pdf.SetTextColor(35, 43, 52)
	d.pdf.Ln(2)
}

func (d pdfDocument) bullet(text string) {
	d.ensureSpace(10)
	d.pdf.SetFont("Arial", "", 9.5)
	d.pdf.SetX(21)
	d.pdf.MultiCell(168, 5.5, d.t("- "+text), "", "L", false)
	d.pdf.Ln(1)
}

func (d pdfDocument) keyValue(key, value string) {
	d.ensureSpace(7)
	d.pdf.SetFont("Arial", "B", 9)
	d.pdf.CellFormat(57, 5.5, d.t(key), "", 0, "L", false, 0, "")
	d.pdf.SetFont("Arial", "", 9)
	d.pdf.MultiCell(117, 5.5, d.t(value), "", "L", false)
}

func (d pdfDocument) bar(label string, value, total int) {
	d.ensureSpace(8)
	y := d.pdf.GetY()
	d.pdf.SetFont("Arial", "", 8)
	d.pdf.CellFormat(43, 5, d.t(label), "", 0, "L", false, 0, "")
	d.pdf.SetFillColor(231, 236, 241)
	d.pdf.Rect(61, y+1, 105, 3.5, "F")
	width := 105 * float64(value) / float64(total)
	if width > 105 {
		width = 105
	}
	d.pdf.SetFillColor(54, 139, 171)
	d.pdf.Rect(61, y+1, width, 3.5, "F")
	d.pdf.SetXY(169, y)
	d.pdf.CellFormat(23, 5, fmt.Sprintf("%d", value), "", 1, "R", false, 0, "")
}

func (d pdfDocument) table(headers []string, widths []float64, rows [][]string) {
	rowHeight := 6.5
	d.ensureSpace(rowHeight * 2)
	d.drawTableRow(headers, widths, rowHeight, true)
	for i, row := range rows {
		if d.pdf.GetY()+rowHeight > 276 {
			d.pdf.AddPage()
			d.pdf.SetY(20)
			d.drawTableRow(headers, widths, rowHeight, true)
		}
		d.drawTableRow(row, widths, rowHeight, i%2 == 1)
	}
	d.pdf.Ln(2)
}

func (d pdfDocument) drawTableRow(values []string, widths []float64, height float64, styled bool) {
	header := false
	if styled && len(values) > 0 {
		header = values[0] == "Indicador" || values[0] == "Pri" || values[0] == "Tipo" || values[0] == "Categoria" || values[0] == "Responsável"
	}
	if header {
		d.pdf.SetFillColor(25, 66, 105)
		d.pdf.SetTextColor(255, 255, 255)
		d.pdf.SetFont("Arial", "B", 7.5)
	} else {
		if styled {
			d.pdf.SetFillColor(244, 247, 250)
		} else {
			d.pdf.SetFillColor(255, 255, 255)
		}
		d.pdf.SetTextColor(35, 43, 52)
		d.pdf.SetFont("Arial", "", 7.3)
	}
	for i, width := range widths {
		value := ""
		if i < len(values) {
			value = fit(values[i], maxInt(5, int(width/2.1)))
		}
		d.pdf.CellFormat(width, height, d.t(value), "1", 0, "L", true, 0, "")
	}
	d.pdf.Ln(height)
	d.pdf.SetTextColor(35, 43, 52)
}

func (d pdfDocument) ensureSpace(height float64) {
	if d.pdf.GetY()+height > 276 {
		d.pdf.AddPage()
		d.pdf.SetY(20)
	}
}

func (d pdfDocument) t(text string) string { return d.translate(text) }

func periodLabel(filters domain.GlobalFilters) string {
	labels := map[domain.DashboardPeriod]string{
		domain.PeriodAll: "Todo o histórico", domain.PeriodToday: "Hoje", domain.PeriodYesterday: "Ontem",
		domain.Period7Days: "Últimos 7 dias", domain.Period30Days: "Últimos 30 dias",
		domain.PeriodCurrentMonth: "Mês atual", domain.PeriodPreviousMonth: "Mês anterior", domain.PeriodCustom: "Período personalizado",
	}
	label := labels[filters.Period]
	if label == "" {
		label = "Todo o histórico"
	}
	if summary := filters.ActiveSummary(); summary != "nenhum" {
		return label + " | filtros: " + summary
	}
	return label
}

func divergenceLabel(value domain.DivergenceType) string {
	labels := map[domain.DivergenceType]string{
		domain.AISaleCRMOpen:                "IA indica venda, CRM aberto",
		domain.AISaleCRMLost:                "IA indica venda, CRM perdido",
		domain.CRMWonWithoutEvidence:        "CRM ganho sem evidência na conversa",
		domain.CRMLostAINegotiating:         "CRM perdido, conversa em negociação",
		domain.PossibleReceiptNotWon:        "Possível comprovante sem fechamento",
		domain.ConversationWithoutCRMUpdate: "Conversa sem atualização do CRM",
		domain.FinalResultConflict:          "Conflito de resultado final",
		domain.PaymentEvidenceWithoutValue:  "Evidência de pagamento sem valor",
		domain.OutsideChannelSuspected:      "Fechamento fora do canal suspeito",
		domain.PaymentTextDetectorConflict:  "Texto indica pagamento, detector não confirmou",
	}
	if label := labels[value]; label != "" {
		return label
	}
	return string(value)
}

func metricMoney(v domain.MetricValue[domain.Money]) string {
	if !v.Available {
		return "Indisponível: " + fallback(v.Reason, "dados insuficientes")
	}
	return v.Value.String()
}

func metricPercentage(v domain.MetricValue[domain.Percentage]) string {
	if !v.Available {
		return "Indisponível: " + fallback(v.Reason, "dados insuficientes")
	}
	return v.Value.String()
}

func metricString(v domain.MetricValue[string]) string {
	if !v.Available {
		return "Indisponível: " + fallback(v.Reason, "dados insuficientes")
	}
	return v.Value
}

func metricInt(v domain.MetricValue[int]) string {
	if !v.Available {
		return "Indisponível: " + fallback(v.Reason, "dados insuficientes")
	}
	return fmt.Sprintf("%d/100", v.Value)
}

func fit(value string, limit int) string {
	value = strings.Join(strings.Fields(value), " ")
	if utf8.RuneCountInString(value) <= limit {
		return value
	}
	runes := []rune(value)
	if limit <= 3 {
		return string(runes[:limit])
	}
	return string(runes[:limit-3]) + "..."
}

func fallback(value, other string) string {
	if strings.TrimSpace(value) == "" {
		return other
	}
	return value
}

func yesNo(value bool) string {
	if value {
		return "Sim"
	}
	return "Não"
}

func maxInt(a, b int) int {
	if a > b {
		return a
	}
	return b
}

var _ interface {
	Render(context.Context, domain.ExecutiveReport) ([]byte, error)
} = ExecutivePDFRenderer{}
