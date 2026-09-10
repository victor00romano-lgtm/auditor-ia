package application

import (
	"context"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	apidomain "github.com/portfolio/auditor-ia/internal/api/domain"
)

type reportSourceStub struct {
	data apidomain.ReportData
	err  error
}

func (s reportSourceStub) GetReportData(context.Context, int64) (apidomain.ReportData, error) {
	return s.data, s.err
}

type rendererStub struct {
	content []byte
	err     error
}

func (r rendererStub) Render(context.Context, apidomain.ReportData, string) ([]byte, error) {
	return r.content, r.err
}

func TestReportExporterCreatesDirectoryAndDoesNotOverwrite(t *testing.T) {
	base := filepath.Join(t.TempDir(), "reports")
	exporter := ReportExporter{Source: reportSourceStub{data: apidomain.ReportData{Audit: apidomain.Audit{AssessmentID: 10}}}, Renderer: rendererStub{content: []byte("%PDF-new")}, BaseDir: base, Now: func() time.Time { return time.Date(2026, 8, 10, 15, 7, 8, 0, time.UTC) }}
	first, err := exporter.Export(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if !filepath.IsAbs(first) || filepath.Base(first) != "auditoria-10.pdf" {
		t.Fatalf("caminho inesperado: %q", first)
	}
	if content, err := os.ReadFile(first); err != nil || string(content) != "%PDF-new" {
		t.Fatalf("arquivo inválido: %q err=%v", content, err)
	}
	if err := os.WriteFile(first, []byte("original"), 0o644); err != nil {
		t.Fatal(err)
	}
	second, err := exporter.Export(context.Background(), 10)
	if err != nil {
		t.Fatal(err)
	}
	if filepath.Base(second) != "auditoria-10-20260810-150708.pdf" {
		t.Fatalf("nome alternativo inesperado: %q", second)
	}
	original, err := os.ReadFile(first)
	if err != nil || string(original) != "original" {
		t.Fatal("arquivo existente foi sobrescrito")
	}
	relative, err := filepath.Rel(base, second)
	if err != nil || strings.HasPrefix(relative, "..") {
		t.Fatalf("path traversal: %q", second)
	}
}
