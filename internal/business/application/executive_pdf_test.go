package application

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/portfolio/auditor-ia/internal/business/domain"
)

type executiveLoaderStub struct {
	dashboard domain.Dashboard
	filters   domain.GlobalFilters
	err       error
}

func (s *executiveLoaderStub) Load(_ context.Context, filters domain.GlobalFilters) (domain.Dashboard, error) {
	s.filters = filters
	return s.dashboard, s.err
}

type executiveRendererStub struct {
	report  domain.ExecutiveReport
	content []byte
	err     error
}

func (s *executiveRendererStub) Render(_ context.Context, report domain.ExecutiveReport) ([]byte, error) {
	s.report = report
	return s.content, s.err
}

func TestExecutivePDFExporterUsesFiltersAndDoesNotOverwrite(t *testing.T) {
	dir := t.TempDir()
	loader := &executiveLoaderStub{dashboard: domain.Dashboard{Executive: domain.ExecutiveSummary{WithoutValue: 2}}}
	renderer := &executiveRendererStub{content: []byte("%PDF-test")}
	now := time.Date(2026, 8, 13, 16, 30, 45, 0, time.UTC)
	exporter := ExecutivePDFExporter{Loader: loader, Renderer: renderer, BaseDir: dir, Now: func() time.Time { return now }}
	filters := domain.GlobalFilters{Period: domain.Period30Days, OnlyWithoutValue: true}

	first, err := exporter.Export(context.Background(), filters)
	if err != nil {
		t.Fatal(err)
	}
	second, err := exporter.Export(context.Background(), filters)
	if err != nil {
		t.Fatal(err)
	}
	if first == second || filepath.Base(first) != "relatorio-executivo-2026-08-13.pdf" || filepath.Base(second) != "relatorio-executivo-2026-08-13-163045.pdf" {
		t.Fatalf("nomes incorretos: %q %q", first, second)
	}
	if loader.filters != filters || renderer.report.Filters != filters {
		t.Fatalf("filtros não propagados: %#v %#v", loader.filters, renderer.report.Filters)
	}
	if len(renderer.report.Recommendations) == 0 || !strings.Contains(renderer.report.Recommendations[0], "2 negócio") {
		t.Fatalf("recomendações ausentes: %#v", renderer.report.Recommendations)
	}
	if content, err := os.ReadFile(first); err != nil || string(content) != "%PDF-test" {
		t.Fatalf("arquivo inválido: content=%q err=%v", content, err)
	}
}

func TestExecutivePDFExporterRejectsInvalidRendererAndPropagatesLoadError(t *testing.T) {
	loader := &executiveLoaderStub{}
	renderer := &executiveRendererStub{content: []byte("not-pdf")}
	exporter := ExecutivePDFExporter{Loader: loader, Renderer: renderer, BaseDir: t.TempDir()}
	if _, err := exporter.Export(context.Background(), domain.GlobalFilters{}); err == nil || !strings.Contains(err.Error(), "PDF inválido") {
		t.Fatalf("PDF inválido aceito: %v", err)
	}
	loader.err = errors.New("banco indisponível")
	if _, err := exporter.Export(context.Background(), domain.GlobalFilters{}); err == nil || !strings.Contains(err.Error(), "banco indisponível") {
		t.Fatalf("erro de carga perdido: %v", err)
	}
}

func TestExecutiveRecommendationsAreDeterministicAndLimited(t *testing.T) {
	dashboard := domain.Dashboard{
		Executive:   domain.ExecutiveSummary{WithoutValue: 10, PossibleReceipts: 3, WithoutConversation: 2, EmptyConversation: 1, AccessDenied: 1, Failed: 2},
		CRMQuality:  domain.CRMQualitySummary{Stale7: 5, Indeterminate: 4},
		Divergences: []domain.CRMAIDivergence{{BitrixDealID: 1}},
	}
	first, second := ExecutiveRecommendations(dashboard), ExecutiveRecommendations(dashboard)
	if strings.Join(first, "|") != strings.Join(second, "|") || len(first) != 7 {
		t.Fatalf("recomendações inconsistentes: %#v %#v", first, second)
	}
}
