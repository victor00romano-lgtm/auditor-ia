package main

import (
	"context"
	"flag"
	"fmt"
	"github.com/portfolio/auditor-ia/internal/governance"
	"github.com/portfolio/auditor-ia/internal/platform/postgres"
	"github.com/portfolio/auditor-ia/internal/platform/security"
	"os"
	"time"
)

func main() {
	deal := flag.Int64("deal", 0, "ID Bitrix")
	apply := flag.Bool("apply", false, "aplicar")
	dry := flag.Bool("dry-run", false, "simular")
	confirm := flag.String("confirm-deal-id", "", "confirmação")
	flag.Parse()
	if *deal <= 0 || (*apply && *confirm != fmt.Sprint(*deal)) {
		fmt.Fprintln(os.Stderr, "uso seguro: --deal ID --dry-run ou --apply --confirm-deal-id ID")
		os.Exit(2)
	}
	_ = dry
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := postgres.Open(ctx)
	if err != nil {
		fail(err)
	}
	defer pool.Close()
	service := governance.Service{Pool: pool, ReportsDir: os.Getenv("REPORTS_DIR")}
	var counts governance.Counts
	if *apply {
		counts, err = service.DeleteDeal(ctx, *deal, *confirm)
	} else {
		counts, err = service.PreviewDeal(ctx, *deal)
	}
	if err != nil {
		fail(err)
	}
	fmt.Printf("negócio=%d mensagens=%d análises=%d avaliações=%d achados=%d jobs=%d negócio=%d PDFs=%d modo=%s\n", *deal, counts.Messages, counts.Analyses, counts.Assessments, counts.Findings, counts.Jobs, counts.Deals, counts.Reports, map[bool]string{true: "apply", false: "dry-run"}[*apply])
}
func fail(err error) { fmt.Fprintln(os.Stderr, "erro:", security.Error(err)); os.Exit(1) }
