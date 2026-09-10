package tui

import (
	"context"
	"fmt"
	"os"
	"sort"
	"strconv"
	"strings"
	"time"

	tea "github.com/charmbracelet/bubbletea"
	batchdomain "github.com/portfolio/auditor-ia/internal/batch/domain"
)

func (m Model) updateBatchInput(msg tea.KeyMsg) (tea.Model, tea.Cmd) {
	if m.batchLoading {
		return m, nil
	}
	if msg.String() == "enter" {
		value := strings.TrimSpace(m.batchInput.Value())
		if value == "" {
			m.err = fmt.Errorf("informe IDs ou um arquivo com @caminho")
			return m, nil
		}
		if m.batchManager == nil {
			m.err = fmt.Errorf("processamento em lote não configurado")
			return m, nil
		}
		m.requestSequence++
		m.batchRequest = m.requestSequence
		m.batchLoading = true
		m.err = nil
		return m, tea.Batch(m.createBatch(value, m.batchRequest), m.spinner.Tick)
	}
	var cmd tea.Cmd
	m.batchInput, cmd = m.batchInput.Update(msg)
	return m, cmd
}

func (m Model) createBatch(value string, request uint64) tea.Cmd {
	manager := m.batchManager
	return func() tea.Msg {
		content := value
		if strings.HasPrefix(value, "@") {
			path := strings.TrimSpace(strings.TrimPrefix(value, "@"))
			raw, err := os.ReadFile(path)
			if err != nil {
				return batchCreatedMsg{request: request, err: fmt.Errorf("ler arquivo de IDs: %w", err)}
			}
			content = string(raw)
		}
		ids, err := parseBatchIDs(content)
		if err != nil {
			return batchCreatedMsg{request: request, err: err}
		}
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		created, err := manager.Create(ctx, ids)
		return batchCreatedMsg{batch: created, request: request, err: err}
	}
}

func parseBatchIDs(content string) ([]int64, error) {
	content = strings.TrimPrefix(content, "\ufeff")
	tokens := strings.FieldsFunc(content, func(r rune) bool { return r == ',' || r == ';' || r == '\n' || r == '\r' || r == '\t' || r == ' ' })
	seen := map[int64]bool{}
	for _, token := range tokens {
		token = strings.Trim(strings.TrimSpace(token), "\"")
		if token == "" || strings.EqualFold(token, "bitrix_deal_id") || strings.EqualFold(token, "id") {
			continue
		}
		id, err := strconv.ParseInt(token, 10, 64)
		if err != nil || id <= 0 {
			return nil, fmt.Errorf("ID inválido na lista: %q", safeToken(token))
		}
		seen[id] = true
	}
	if len(seen) == 0 {
		return nil, fmt.Errorf("nenhum ID válido encontrado")
	}
	if len(seen) > 10000 {
		return nil, fmt.Errorf("a lista excede 10000 negócios")
	}
	ids := make([]int64, 0, len(seen))
	for id := range seen {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids, nil
}
func safeToken(value string) string {
	r := []rune(value)
	if len(r) > 24 {
		r = r[:24]
	}
	return string(r)
}

func batchPollCmd(request uint64) tea.Cmd {
	return tea.Tick(time.Second, func(time.Time) tea.Msg { return batchPollMsg{request: request} })
}
func (m Model) loadBatchProgress(request uint64, id int64) tea.Cmd {
	manager := m.batchManager
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		result, err := manager.Get(ctx, id)
		return batchProgressMsg{batch: result, request: request, err: err}
	}
}
func (m Model) cancelBatch(request uint64, id int64) tea.Cmd {
	manager := m.batchManager
	return func() tea.Msg {
		ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
		defer cancel()
		return batchCanceledMsg{request: request, err: manager.Cancel(ctx, id)}
	}
}
func isBatchTerminal(status batchdomain.Status) bool {
	return status == batchdomain.Completed || status == batchdomain.Partial || status == batchdomain.Failed || status == batchdomain.Canceled
}
