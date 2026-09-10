package main

import (
	"context"
	"fmt"
	"log"
	"time"

	dealdomain "github.com/portfolio/auditor-ia/internal/deal/domain"
	postgresdb "github.com/portfolio/auditor-ia/internal/platform/postgres"
)

func main() {
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()

	pool, err := postgresdb.Open(ctx)
	if err != nil {
		log.Fatalf("abrir PostgreSQL: %v", err)
	}
	defer pool.Close()

	repository := postgresdb.NewDealRepository(pool)
	now := time.Now().UTC()
	title := "Negócio de demonstração do dealcheck"
	stageID := "NEW"
	semanticID := "P"
	closed := false
	currency := "BRL"
	amount := "199.90"
	deal := dealdomain.Deal{
		BitrixDealID:    999999999,
		Title:           &title,
		StageID:         &stageID,
		StageSemanticID: &semanticID,
		Closed:          &closed,
		Amount:          &amount,
		Currency:        &currency,
		SyncedAt:        &now,
	}

	firstID, err := repository.Upsert(ctx, deal)
	if err != nil {
		log.Fatalf("primeiro UPSERT: %v", err)
	}
	secondID, err := repository.Upsert(ctx, deal)
	if err != nil {
		log.Fatalf("segundo UPSERT: %v", err)
	}
	if firstID != secondID {
		log.Fatalf("UPSERT gerou IDs diferentes: %d e %d", firstID, secondID)
	}

	fmt.Printf("UPSERT idempotente: deals.id=%d bitrix_deal_id=%d\n", firstID, deal.BitrixDealID)
}
