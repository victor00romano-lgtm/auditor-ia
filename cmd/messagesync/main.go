package main

import (
	"context"
	"fmt"
	"net/http"
	"os"
	"strings"
	"time"

	conversationbitrix "github.com/portfolio/auditor-ia/internal/conversation/infrastructure/bitrix"
	dealbitrix "github.com/portfolio/auditor-ia/internal/deal/infrastructure/bitrix"
	platformconfig "github.com/portfolio/auditor-ia/internal/platform/config"
	postgresdb "github.com/portfolio/auditor-ia/internal/platform/postgres"
	"github.com/portfolio/auditor-ia/internal/platform/security"
)

const defaultTimeout = 2 * time.Minute

func main() {
	if len(os.Args) != 2 || strings.TrimSpace(os.Args[1]) == "" {
		fmt.Fprintln(os.Stderr, "uso: go run ./cmd/messagesync <deal_id>")
		os.Exit(2)
	}
	cfg, configErr := platformconfig.Load(platformconfig.MessageSync)
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
	httpClient := &http.Client{Timeout: timeout}

	deal, err := (dealbitrix.Source{Webhook: webhook, Client: httpClient}).Get(ctx, os.Args[1])
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
	internalDealID, err := postgresdb.NewDealRepository(pool).Upsert(ctx, deal)
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}

	messages, err := (conversationbitrix.Timeline{Webhook: webhook, Client: httpClient}).Messages(ctx, os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro: coletar mensagens:", err)
		os.Exit(1)
	}
	persisted, err := postgresdb.NewMessageRepository(pool).UpsertBatch(ctx, internalDealID, messages)
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}

	fmt.Printf("ID do negócio: %d\n", deal.BitrixDealID)
	fmt.Printf("ID interno no PostgreSQL: %d\n", internalDealID)
	fmt.Printf("Quantidade coletada: %d\n", len(messages))
	fmt.Printf("Quantidade persistida: %d\n", persisted.Processed)
	fmt.Println("Mensagens sincronizadas com sucesso.")
}

func configuredTimeout() (time.Duration, error) {
	value := strings.TrimSpace(os.Getenv("MESSAGESYNC_TIMEOUT"))
	if value == "" {
		return defaultTimeout, nil
	}
	timeout, err := time.ParseDuration(value)
	if err != nil || timeout <= 0 {
		return 0, fmt.Errorf("MESSAGESYNC_TIMEOUT inválido: use uma duração positiva, como 2m")
	}
	return timeout, nil
}
