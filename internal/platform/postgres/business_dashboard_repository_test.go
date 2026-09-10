package postgres

import (
	"bytes"
	"context"
	"os"
	"path/filepath"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	business "github.com/portfolio/auditor-ia/internal/business/domain"
)

func TestBusinessDashboardRepositoryWithRealPostgres(t *testing.T) {
	url := os.Getenv("DATABASE_URL")
	if url == "" {
		t.Skip("DATABASE_URL não configurada")
	}
	pool, err := pgxpool.New(context.Background(), url)
	if err != nil {
		t.Skipf("PostgreSQL indisponível: %v", err)
	}
	defer pool.Close()
	dashboard, err := NewBusinessDashboardRepository(pool).Load(context.Background(), business.GlobalFilters{Period: business.PeriodAll})
	if err != nil {
		t.Fatal(err)
	}
	if dashboard.Executive.DealsEvaluated > dashboard.Executive.DealsSynced {
		t.Fatalf("avaliações atuais duplicaram negócios: %#v", dashboard.Executive)
	}
	if dashboard.Executive.Sample != dashboard.Executive.DealsEvaluated {
		t.Fatalf("amostra incorreta: %#v", dashboard.Executive)
	}
}

func TestPaymentTextDetectorConflict(t *testing.T) {
	p := business.PriorityOpportunity{AnalysisStatus: "COMPLETED", Evidence: "Vou pagar agora e envio do comprovante de pagamento.", ExplicitPaymentEvidence: true}
	got := divergenceTypes(p, "P", false)
	if len(got) != 1 || got[0] != business.PaymentTextDetectorConflict {
		t.Fatalf("divergência 34700=%v", got)
	}
}

func TestExplicitPaymentWithReceiptIsNotDetectorConflict(t *testing.T) {
	p := business.PriorityOpportunity{AnalysisStatus: "COMPLETED", Evidence: "Pagamento já realizado.", ExplicitPaymentEvidence: hasPaymentEvidence("Pagamento já realizado."), PossibleReceipt: true}
	for _, got := range divergenceTypes(p, "S", true) {
		if got == business.PaymentTextDetectorConflict {
			t.Fatalf("pagamento consistente gerou conflito: %v", got)
		}
	}
}

func TestFailedPossibleReceiptDoesNotGetTextConflict(t *testing.T) {
	p := business.PriorityOpportunity{AnalysisStatus: "FAILED", Evidence: "Pagamento já realizado.", ExplicitPaymentEvidence: true, PossibleReceipt: false}
	if got := divergenceTypes(p, "P", false); len(got) != 0 {
		t.Fatalf("análise falhada gerou divergência textual: %v", got)
	}
}

func TestMigration007IsUTF8AndUsesNonPositiveValueRule(t *testing.T) {
	path := filepath.Join("..", "..", "..", "migrations", "007_business_dashboard.sql")
	content, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	corruptNegotiation := []byte{'e', 'm', ' ', 'n', 'e', 'g', 'o', 'c', 'i', 'a', 0xc3, 0x83, 0xc2, 0xa7, 0xc3, 0x83, 0xc2, 0xa3, 'o'}
	if !bytes.Contains(content, []byte("em negociação")) || bytes.Contains(content, corruptNegotiation) {
		t.Fatal("migration 007 contém texto corrompido")
	}
	if !bytes.Contains(content, []byte("d.amount IS NULL OR d.amount <= 0")) {
		t.Fatal("migration 007 não trata amount <= 0 como sem valor")
	}
}
