package tui

import (
	"context"
	"fmt"

	tea "github.com/charmbracelet/bubbletea"
	statsdomain "github.com/portfolio/auditor-ia/internal/crmstats/domain"
)

func (m Model) startCRMStatsSync() (tea.Model, tea.Cmd) {
	if m.crmSyncing {
		m.feedback = "A sincronização global já está em execução"
		return m, nil
	}
	if m.crmSyncer == nil {
		m.err = fmt.Errorf("sincronização global não configurada")
		return m, nil
	}
	m.requestSequence++
	m.crmSyncRequest = m.requestSequence
	m.crmSyncing = true
	m.err = nil
	m.feedback = "Sincronizando metadados globais do Bitrix..."
	m.crmSyncProgress = statsdomain.Progress{Phase: "starting"}
	m.crmSyncProgressCh = make(chan statsdomain.Progress, 32)
	ctx, cancel := context.WithCancel(context.Background())
	m.crmSyncCancel = cancel
	return m, tea.Batch(m.spinner.Tick, m.executeCRMStatsSync(ctx, m.crmSyncRequest), m.waitCRMStatsProgress(m.crmSyncRequest))
}

func (m Model) executeCRMStatsSync(ctx context.Context, request uint64) tea.Cmd {
	syncer := m.crmSyncer
	updates := m.crmSyncProgressCh
	return func() tea.Msg {
		err := syncer.Sync(ctx, func(progress statsdomain.Progress) {
			select {
			case updates <- progress:
			default:
			}
		})
		close(updates)
		return crmSyncDoneMsg{request: request, err: err}
	}
}

func (m Model) waitCRMStatsProgress(request uint64) tea.Cmd {
	updates := m.crmSyncProgressCh
	return func() tea.Msg {
		progress, ok := <-updates
		if !ok {
			return crmSyncProgressClosedMsg{request: request}
		}
		return crmSyncProgressMsg{progress: progress, request: request}
	}
}
