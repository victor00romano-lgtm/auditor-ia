package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"time"

	bitrixdeal "github.com/portfolio/auditor-ia/internal/deal/infrastructure/bitrix"
	platformconfig "github.com/portfolio/auditor-ia/internal/platform/config"
	postgresdb "github.com/portfolio/auditor-ia/internal/platform/postgres"
	"github.com/portfolio/auditor-ia/internal/platform/security"
)

func main() {
	cfg, err := platformconfig.Load(platformconfig.AssigneeSync)
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", security.Error(err))
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	pool, err := postgresdb.Open(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", security.Error(err))
		os.Exit(1)
	}
	defer pool.Close()
	repository := postgresdb.NewAssigneeRepository(pool)
	ids, err := repository.MissingIDs(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", security.Error(err))
		os.Exit(1)
	}
	client := &http.Client{Timeout: cfg.BitrixHTTPTimeout}
	succeeded, failed := 0, 0
	for _, id := range ids {
		assignee, err := bitrixdeal.GetAssignee(ctx, cfg.BitrixWebhookURL, client, id)
		if err != nil {
			fmt.Fprintf(os.Stderr, "responsável %d: %v\n", id, security.Error(err))
			failed++
			continue
		}
		if err := repository.Upsert(ctx, assignee); err != nil {
			fmt.Fprintln(os.Stderr, "erro:", security.Error(err))
			os.Exit(1)
		}
		fmt.Printf("%d -> %s\n", id, assignee.DisplayName)
		succeeded++
	}
	fmt.Printf("Sincronizados: %d\nFalhas: %d\n", succeeded, failed)
}
