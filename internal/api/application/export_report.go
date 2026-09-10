package application

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"time"

	apidomain "github.com/portfolio/auditor-ia/internal/api/domain"
	"github.com/portfolio/auditor-ia/internal/observability"
	"go.opentelemetry.io/otel/attribute"
)

type ReportDataSource interface {
	GetReportData(context.Context, int64) (apidomain.ReportData, error)
}

type ReportExporter struct {
	Source   ReportDataSource
	Renderer apidomain.ReportRenderer
	BaseDir  string
	Now      func() time.Time
}

func (exporter ReportExporter) Export(ctx context.Context, assessmentID int64) (string, error) {
	ctx, span := observability.Tracer().Start(ctx, "report.pdf.generate")
	span.SetAttributes(attribute.Int64("assessment.id", assessmentID), attribute.String("operation", "report.pdf.generate"))
	defer span.End()
	if assessmentID <= 0 {
		return "", fmt.Errorf("assessment_id inválido")
	}
	if exporter.Source == nil || exporter.Renderer == nil {
		return "", fmt.Errorf("exportação de relatório não configurada")
	}
	data, err := exporter.Source.GetReportData(ctx, assessmentID)
	if err != nil {
		return "", fmt.Errorf("consultar dados do relatório: %w", err)
	}
	summary := ExecutiveSummary(data)
	content, err := exporter.Renderer.Render(ctx, data, summary)
	if err != nil {
		return "", fmt.Errorf("gerar relatório: %w", err)
	}
	baseDir := exporter.BaseDir
	if baseDir == "" {
		baseDir = "./reports"
	}
	directory, err := filepath.Abs(filepath.Clean(baseDir))
	if err != nil {
		return "", fmt.Errorf("resolver pasta de relatórios: %w", err)
	}
	if err := os.MkdirAll(directory, 0o755); err != nil {
		return "", fmt.Errorf("criar pasta de relatórios: %w", err)
	}
	resolvedDirectory := directory
	now := exporter.Now
	if now == nil {
		now = time.Now
	}
	baseName := fmt.Sprintf("auditoria-%d.pdf", assessmentID)
	path := filepath.Join(resolvedDirectory, baseName)
	valid, err := isInsideDirectory(resolvedDirectory, path)
	if err != nil || !valid {
		return "", fmt.Errorf("caminho de relatório inválido")
	}
	file, err := os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if os.IsExist(err) {
		baseName = fmt.Sprintf("auditoria-%d-%s.pdf", assessmentID, now().Format("20060102-150405"))
		path = filepath.Join(resolvedDirectory, baseName)
		valid, validationErr := isInsideDirectory(resolvedDirectory, path)
		if validationErr != nil || !valid {
			return "", fmt.Errorf("caminho de relatório inválido")
		}
		file, err = os.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	}
	if err != nil {
		return "", fmt.Errorf("criar arquivo de relatório: %w", err)
	}
	if _, err := file.Write(content); err != nil {
		_ = file.Close()
		_ = os.Remove(path)
		return "", fmt.Errorf("salvar relatório: %w", err)
	}
	if err := file.Close(); err != nil {
		_ = os.Remove(path)
		return "", fmt.Errorf("finalizar relatório: %w", err)
	}
	return path, nil
}

func isInsideDirectory(directory, path string) (bool, error) {
	relative, err := filepath.Rel(directory, path)
	if err != nil {
		return false, err
	}
	return relative != ".." && relative != "." && !filepath.IsAbs(relative) && len(relative) > 0 && relative[:1] != ".", nil
}
