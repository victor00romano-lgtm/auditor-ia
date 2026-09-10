package main

import (
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	bitrixdeal "github.com/portfolio/auditor-ia/internal/deal/infrastructure/bitrix"
	platformconfig "github.com/portfolio/auditor-ia/internal/platform/config"
	postgresdb "github.com/portfolio/auditor-ia/internal/platform/postgres"
	"github.com/portfolio/auditor-ia/internal/platform/security"
)

const defaultTimeout = 30 * time.Second

func main() {
	if len(os.Args) != 2 || strings.TrimSpace(os.Args[1]) == "" {
		fmt.Fprintln(os.Stderr, "uso: go run ./cmd/dealsync <deal_id>")
		os.Exit(2)
	}
	cfg, configErr := platformconfig.Load(platformconfig.DealSync)
	if configErr != nil {
		fmt.Fprintln(os.Stderr, "erro:", security.Error(configErr))
		os.Exit(1)
	}
	webhook := cfg.BitrixWebhookURL
	timeout, err := configuredTimeout()
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()

	deal, err := (bitrixdeal.Source{Webhook: webhook}).Get(ctx, os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}
	pool, err := postgresdb.Open(ctx)
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro: abrir PostgreSQL:", err)
		os.Exit(1)
	}
	defer pool.Close()

	internalID, err := postgresdb.NewDealRepository(pool).Upsert(ctx, deal)
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}
	title := ""
	if deal.Title != nil {
		title = *deal.Title
	}
	fmt.Printf("Salvo com sucesso: bitrix_deal_id=%d postgres_id=%d title=%q\n", deal.BitrixDealID, internalID, title)
}

func configuredTimeout() (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv("DEALSYNC_TIMEOUT"))
	if value == "" {
		return defaultTimeout, nil
	}
	timeout, err := time.ParseDuration(value)
	if err != nil || timeout <= 0 {
		return 0, fmt.Errorf("DEALSYNC_TIMEOUT inválido: use uma duração positiva, como 30s")
	}
	return timeout, nil
}
