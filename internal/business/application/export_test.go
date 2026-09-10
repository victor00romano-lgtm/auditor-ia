package application

import (
	"bytes"
	"context"
	"encoding/csv"
	"os"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"github.com/portfolio/auditor-ia/internal/business/domain"
)

func TestSafeCSVPreventsFormulaInjection(t *testing.T) {
	for _, v := range []string{"=CMD()", "+1", "-2", "@SUM(A1)"} {
		if got := SafeCSV(v); got[0] != '\'' {
			t.Fatalf("não escapou %q: %q", v, got)
		}
	}
	if got := SafeCSV("normal"); got != "normal" {
		t.Fatalf("alterou texto: %q", got)
	}
}

func TestAllCSVExportsUseUTF8BOMAndExcludeSensitiveFields(t *testing.T) {
	dir := t.TempDir()
	baseTime := time.Date(2026, 8, 13, 12, 0, 0, 0, time.UTC)
	newExporter := func(second int) CSVExporter {
		return CSVExporter{BaseDir: dir, Now: func() time.Time { return baseTime.Add(time.Duration(second) * time.Second) }}
	}
	tests := []struct {
		name string
		run  func(CSVExporter) (string, error)
		want []string
	}{
		{"prioridades", func(e CSVExporter) (string, error) {
			return e.Priorities(context.Background(), []domain.PriorityOpportunity{{BitrixDealID: 35318, Title: "Negócio possível", Assignee: "Média", Stage: "em negociação", ProbableResult: "não venda", PossibleReceipt: true}})
		}, []string{"Negócio possível", "Média", "não venda"}},
		{"divergências", func(e CSVExporter) (string, error) {
			return e.Divergences(context.Background(), []domain.CRMAIDivergence{{BitrixDealID: 34700, Evidence: "raw_response=segredo", Recommendation: "Recomendação possível", Confidence: "média"}})
		}, []string{"Recomendação possível", "média"}},
		{"qualidade CRM", func(e CSVExporter) (string, error) {
			return e.CRMQuality(context.Background(), domain.CRMQualitySummary{WithoutValue: 61})
		}, []string{"sem_valor"}},
		{"objeções", func(e CSVExporter) (string, error) {
			return e.Objections(context.Background(), []domain.ObjectionSummary{{Label: "Preço médio", Count: 2}})
		}, []string{"Preço médio"}},
		{"equipe", func(e CSVExporter) (string, error) {
			return e.Agents(context.Background(), []domain.AgentPerformance{{Assignee: "João; negócio", Deals: 1}})
		}, []string{"João; negócio"}},
	}
	for i, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			path, err := tc.run(newExporter(i))
			if err != nil {
				t.Fatal(err)
			}
			content, err := os.ReadFile(path)
			if err != nil {
				t.Fatal(err)
			}
			if !bytes.HasPrefix(content, []byte{0xEF, 0xBB, 0xBF}) {
				t.Fatalf("BOM ausente: % x", content[:min(3, len(content))])
			}
			payload := content[3:]
			if !utf8.Valid(payload) {
				t.Fatal("conteúdo após BOM não é UTF-8 válido")
			}
			text := string(payload)
			for _, want := range tc.want {
				if !strings.Contains(text, want) {
					t.Fatalf("texto %q ausente em %q", want, text)
				}
			}
			for _, forbidden := range []string{"raw_response", "segredo", "token", "credential"} {
				if strings.Contains(strings.ToLower(text), forbidden) {
					t.Fatalf("campo sensível exportado: %q", forbidden)
				}
			}
		})
	}
}

func TestCSVPreservesRFC4180FieldsAndInjectionProtection(t *testing.T) {
	e := CSVExporter{BaseDir: t.TempDir(), Now: func() time.Time { return time.Date(2026, 8, 13, 13, 0, 0, 0, time.UTC) }}
	path, err := e.Priorities(context.Background(), []domain.PriorityOpportunity{
		{BitrixDealID: 1, Title: "Negócio; com \"aspas\"\ne quebra", Assignee: "=CMD()", Stage: "+SUM(A1)", ProbableResult: "-1", Confidence: "@média"},
	})
	if err != nil {
		t.Fatal(err)
	}
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	reader := csv.NewReader(bytes.NewReader(content[3:]))
	reader.Comma = ';'
	records, err := reader.ReadAll()
	if err != nil {
		t.Fatalf("CSV inválido: %v", err)
	}
	if len(records) != 2 || records[1][2] != "Negócio; com \"aspas\"\ne quebra" {
		t.Fatalf("escaping RFC 4180 incorreto: %#v", records)
	}
	for _, index := range []int{3, 4, 5, 6, 9} {
		if !strings.HasPrefix(records[1][index], "'") {
			t.Fatalf("injeção não protegida na coluna %d: %q", index, records[1][index])
		}
	}
}
