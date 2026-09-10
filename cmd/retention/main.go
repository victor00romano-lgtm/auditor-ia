package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/portfolio/auditor-ia/internal/governance"
	platformconfig "github.com/portfolio/auditor-ia/internal/platform/config"
	"github.com/portfolio/auditor-ia/internal/platform/postgres"
	"github.com/portfolio/auditor-ia/internal/platform/security"
	"os"
	"time"
)

func main() {
	apply := flag.Bool("apply", false, "aplicar")
	dry := flag.Bool("dry-run", false, "simular")
	confirm := flag.Bool("confirm-retention-delete", false, "confirmar exclusão")
	flag.Parse()
	if *apply && !*confirm {
		fmt.Fprintln(os.Stderr, "--apply exige --confirm-retention-delete")
		os.Exit(2)
	}
	_ = dry
	cfg, err := platformconfig.Load(platformconfig.SecurityCheck)
	if err != nil {
		fail(err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Minute)
	defer cancel()
	pool, err := postgres.Open(ctx)
	if err != nil {
		fail(err)
	}
	defer pool.Close()
	s := governance.Service{Pool: pool, ReportsDir: os.Getenv("REPORTS_DIR")}
	counts, err := s.Retention(ctx, governance.RetentionPolicy{MessagesDays: cfg.Retention.MessagesDays, RawDays: cfg.Retention.RawAnalysisDays, AssessmentsDays: cfg.Retention.AssessmentsDays, ReportsDays: cfg.Retention.ReportsDays, Batch: 500}, *apply)
	if err != nil {
		fail(err)
	}
	for _, name := range []string{"messages", "raw_analysis", "assessments", "reports", "logs"} {
		fmt.Printf("categoria=%s quantidade=%d modo=%s\n", name, counts[name], map[bool]string{true: "apply", false: "dry-run"}[*apply])
	}
}
func fail(err error) { fmt.Fprintln(os.Stderr, "erro:", security.Error(err)); os.Exit(1) }
