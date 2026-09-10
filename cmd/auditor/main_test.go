package main

import "testing"

func TestReportsDirUsesEnvironmentWithSafeDefault(t *testing.T) {
	t.Setenv("REPORTS_DIR", "")
	if got := reportsDir(); got != "./reports" {
		t.Fatalf("diretório padrão: %q", got)
	}
	t.Setenv("REPORTS_DIR", "/reports")
	if got := reportsDir(); got != "/reports" {
		t.Fatalf("REPORTS_DIR ignorado: %q", got)
	}
}
