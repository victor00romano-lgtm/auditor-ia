package main

import (
	"context"
	"fmt"
	"github.com/portfolio/auditor-ia/internal/conversation/application"
	bitrixconversation "github.com/portfolio/auditor-ia/internal/conversation/infrastructure/bitrix"
	ollamaconversation "github.com/portfolio/auditor-ia/internal/conversation/infrastructure/ollama"
	"os"
	"time"
)

func main() {
	if len(os.Args) != 2 {
		fmt.Fprintln(os.Stderr, "uso: go run ./cmd/conversation <deal_id>")
		os.Exit(2)
	}
	webhook := os.Getenv("BITRIX_WEBHOOK_URL")
	if webhook == "" {
		fmt.Fprintln(os.Stderr, "BITRIX_WEBHOOK_URL não configurado")
		os.Exit(2)
	}
	bitrixTimeline := bitrixconversation.Timeline{Webhook: webhook}
	useCase := application.Analyze{Source: bitrixTimeline, FormSource: bitrixTimeline, StatusSource: bitrixTimeline, Analyzer: ollamaconversation.Analyzer{URL: getenv("OLLAMA_URL", "http://localhost:11434"), Model: getenv("OLLAMA_MODEL", "gemma3:4b")}}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	result, err := useCase.Execute(ctx, os.Args[1])
	if err != nil {
		fmt.Fprintln(os.Stderr, "erro:", err)
		os.Exit(1)
	}
	fmt.Fprintf(os.Stderr, "Negócio %s: formulários=%d campos=%d mensagens=%d\n", result.DealID, result.FormCount, result.FormFieldCount, result.MessageCount)
	fmt.Printf("Fonte analisada: %s\n", result.AnalysisSource)
	fmt.Printf("Resultado final: %s\n", result.FinalResult)
	fmt.Printf("Fonte do resultado: %s\n", result.ResultSource)
	fmt.Printf("Estado da conversa: %s\n", result.ConversationState)
	fmt.Printf("Observação: %s\n", result.Observation)
	fmt.Printf("Ação: %s\n\n", result.RecommendedAction)
	fmt.Printf("Análise da conversa\n%s\n", result.Analysis)
}
func getenv(key, fallback string) string {
	if value := os.Getenv(key); value != "" {
		return value
	}
	return fallback
}
