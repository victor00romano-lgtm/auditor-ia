package bootstrap

import (
	"crypto/rand"
	"encoding/hex"
	"os"
	"time"

	"github.com/portfolio/auditor-ia/internal/audit/application"
	"github.com/portfolio/auditor-ia/internal/audit/infrastructure/bitrix"
	"github.com/portfolio/auditor-ia/internal/audit/infrastructure/memory"
	"github.com/portfolio/auditor-ia/internal/audit/infrastructure/ollama"
	"github.com/portfolio/auditor-ia/internal/audit/infrastructure/rules"
)

type App struct {
	Run  application.RunAudit
	Repo *memory.Repository
}

func New() (*App, error) {
	path := getenv("RULES_FILE", "configs/rules.yaml")
	engine, err := rules.Load(path)
	if err != nil {
		return nil, err
	}
	repo := memory.New()
	run := application.RunAudit{Repo: repo, Source: bitrix.Source{Webhook: os.Getenv("BITRIX_WEBHOOK_URL")}, Rules: engine, AI: ollama.Analyzer{URL: getenv("OLLAMA_URL", "http://localhost:11434"), Model: getenv("OLLAMA_MODEL", "gemma3:4b")}, Now: time.Now, NewID: newID}
	return &App{Run: run, Repo: repo}, nil
}
func getenv(key, fallback string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return fallback
}
func newID() string { b := make([]byte, 8); _, _ = rand.Read(b); return hex.EncodeToString(b) }
