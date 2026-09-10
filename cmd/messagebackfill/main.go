package main

import (
	"context"
	"flag"
	"fmt"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	conversationbitrix "github.com/portfolio/auditor-ia/internal/conversation/infrastructure/bitrix"
	"github.com/portfolio/auditor-ia/internal/messagebackfill"
	platformconfig "github.com/portfolio/auditor-ia/internal/platform/config"
	"github.com/portfolio/auditor-ia/internal/platform/httpclient"
	postgresdb "github.com/portfolio/auditor-ia/internal/platform/postgres"
	"github.com/portfolio/auditor-ia/internal/platform/security"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "erro:", security.Error(err))
		os.Exit(1)
	}
}

func run() error {
	dealID := flag.Int64("deal", 0, "ID do negócio no Bitrix")
	limit := flag.Int("limit", 100, "quantidade máxima de negócios")
	concurrency := flag.Int("concurrency", 2, "coletas simultâneas")
	includeAll := flag.Bool("all", false, "inclui negócios que já possuem mensagens")
	dryRun := flag.Bool("dry-run", false, "somente lista e conta os negócios")
	flag.Parse()
	if *dealID < 0 || *limit < 1 || *concurrency < 1 || *concurrency > 16 {
		return fmt.Errorf("parâmetros inválidos: --deal deve ser positivo, --limit >= 1 e --concurrency entre 1 e 16")
	}
	cfg, err := platformconfig.Load(platformconfig.MessageBackfill)
	if err != nil {
		return err
	}
	root, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	ctx, cancel := context.WithTimeout(root, cfg.MessageBackfillTimeout)
	defer cancel()
	pool, err := postgresdb.Open(ctx)
	if err != nil {
		return fmt.Errorf("abrir PostgreSQL: %w", err)
	}
	defer pool.Close()
	transport := httpclient.RetryTransport{Next: httpclient.NewLimitTransport(http.DefaultTransport, *concurrency), MaxAttempts: 4, BaseDelay: 500 * time.Millisecond}
	client := &http.Client{Timeout: cfg.BitrixHTTPTimeout, Transport: transport}
	service := messagebackfill.Service{
		Deals: postgresdb.NewMessageBackfillRepository(pool), Source: conversationbitrix.Timeline{Webhook: cfg.BitrixWebhookURL, Client: client},
		Messages: postgresdb.NewMessageRepository(pool), Concurrency: *concurrency,
	}
	started := time.Now()
	summary, err := service.Run(ctx, messagebackfill.Selection{DealID: *dealID, Limit: *limit, IncludeWithMessages: *includeAll}, *dryRun, func(current messagebackfill.Summary) {
		if !*dryRun {
			fmt.Printf("\rProcessados: %d/%d | mensagens: %d | falhos: %d", current.Processed, current.TotalSelected, current.MessagesPersisted, current.Failed)
		}
	})
	if !*dryRun {
		fmt.Println()
	} else {
		for _, id := range summary.SelectedDealIDs {
			fmt.Printf("deal_id: %d\n", id)
		}
	}
	fmt.Printf("Total selecionado: %d\nProcessados: %d\nNegócios com mensagens: %d\nMensagens coletadas: %d\nMensagens persistidas: %d\nSem conversa: %d\nConversa vazia: %d\nAcesso negado: %d\nFalhos: %d\nTempo: %s\n", summary.TotalSelected, summary.Processed, summary.DealsWithMessages, summary.MessagesCollected, summary.MessagesPersisted, summary.NoConversation, summary.EmptyConversation, summary.AccessDenied, summary.Failed, time.Since(started).Round(time.Millisecond))
	return err
}
