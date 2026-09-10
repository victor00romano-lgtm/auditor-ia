package tui

import (
	"context"
	"os"
	"reflect"
	"strings"
	"testing"

	tea "github.com/charmbracelet/bubbletea"
	batchdomain "github.com/portfolio/auditor-ia/internal/batch/domain"
)

type fakeBatchManager struct {
	createdIDs []int64
	batch      batchdomain.Batch
	canceled   int64
	err        error
}

func (f *fakeBatchManager) Create(_ context.Context, ids []int64) (batchdomain.Batch, error) {
	f.createdIDs = append([]int64(nil), ids...)
	return f.batch, f.err
}

func (f *fakeBatchManager) Get(context.Context, int64) (batchdomain.Batch, error) {
	return f.batch, f.err
}

func (f *fakeBatchManager) Cancel(_ context.Context, id int64) error {
	f.canceled = id
	return f.err
}

func TestParseBatchIDsAcceptsCSVTextDeduplicatesAndSorts(t *testing.T) {
	got, err := parseBatchIDs("\ufeffbitrix_deal_id\r\n35318, 34700;35318\n34746")
	if err != nil {
		t.Fatal(err)
	}
	want := []int64{34700, 34746, 35318}
	if !reflect.DeepEqual(got, want) {
		t.Fatalf("IDs = %v; esperado %v", got, want)
	}
}

func TestParseBatchIDsRejectsInvalidInput(t *testing.T) {
	if _, err := parseBatchIDs("52, segredo"); err == nil || !strings.Contains(err.Error(), "ID inválido") {
		t.Fatalf("erro inválido: %v", err)
	}
}

func TestBatchImportCanReadAFileAndCreatePersistentBatch(t *testing.T) {
	path := t.TempDir() + string(os.PathSeparator) + "negocios.csv"
	if err := os.WriteFile(path, []byte("id\n52\n53\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	manager := &fakeBatchManager{batch: batchdomain.Batch{ID: 9, Status: batchdomain.Pending, Total: 2, Pending: 2}}
	m := New(applicationRunStub())
	m.SetBatchManager(manager)
	msg := m.createBatch("@"+path, 7)()
	created, ok := msg.(batchCreatedMsg)
	if !ok || created.err != nil || created.batch.ID != 9 {
		t.Fatalf("resultado inesperado: %#v", msg)
	}
	if !reflect.DeepEqual(manager.createdIDs, []int64{52, 53}) {
		t.Fatalf("IDs enviados = %v", manager.createdIDs)
	}
}

func TestBatchMenuAndProgressLoadingScreen(t *testing.T) {
	m := New(applicationRunStub())
	m.SetBatchManager(&fakeBatchManager{})
	m.width, m.height = 100, 32
	m, _ = updateModel(t, m, key("L"))
	if m.screen != ScreenBatchImport || !m.batchInput.Focused() {
		t.Fatalf("tela de importação não abriu: %v", m.screen)
	}
	m.batchRequest = 3
	m, _ = updateModel(t, m, batchCreatedMsg{request: 3, batch: batchdomain.Batch{ID: 12, Status: batchdomain.Running, Total: 10, Pending: 6, Processing: 1, Completed: 2, Failed: 1}})
	view := m.View()
	for _, want := range []string{"PROCESSAMENTO EM LOTE #12", "30.00%", "Pendentes: 6", "Processando: 1", "Concluídos: 2", "Falhos: 1"} {
		if !strings.Contains(view, want) {
			t.Fatalf("tela não contém %q:\n%s", want, view)
		}
	}
}

func TestLeavingBatchImportIgnoresLateCreationResult(t *testing.T) {
	m := New(applicationRunStub())
	m.screen = ScreenBatchImport
	m.batchRequest = 4
	m.requestSequence = 4
	m, _ = updateModel(t, m, tea.KeyMsg{Type: tea.KeyEsc})
	if m.screen != ScreenMenu {
		t.Fatal("Esc não voltou ao menu")
	}
	m, _ = updateModel(t, m, batchCreatedMsg{request: 4, batch: batchdomain.Batch{ID: 99}})
	if m.screen != ScreenMenu {
		t.Fatal("resultado atrasado reabriu a tela de lote")
	}
}

func TestBatchCancelIsSentAndShown(t *testing.T) {
	manager := &fakeBatchManager{batch: batchdomain.Batch{ID: 15, Status: batchdomain.Running}}
	m := New(applicationRunStub())
	m.SetBatchManager(manager)
	m.screen = ScreenBatchProgress
	m.batchRequest = 8
	m.batch = manager.batch
	m, cmd := updateModel(t, m, key("c"))
	if cmd == nil || !m.batchLoading {
		t.Fatal("cancelamento não entrou em carregamento")
	}
	msg := m.cancelBatch(8, 15)()
	canceled := msg.(batchCanceledMsg)
	if canceled.err != nil || manager.canceled != 15 {
		t.Fatalf("cancelamento incorreto: %#v", canceled)
	}
}
