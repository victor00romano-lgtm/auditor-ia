package main

import (
	"testing"
	"time"

	platformconfig "github.com/portfolio/auditor-ia/internal/platform/config"
)

func TestDependencyClientsUseIndependentTimeouts(t *testing.T) {
	bitrix, ollama := dependencyClients(platformconfig.Config{BitrixHTTPTimeout: 17 * time.Second, OllamaHTTPTimeout: 8 * time.Minute})
	if bitrix == ollama {
		t.Fatal("Bitrix e Ollama compartilharam o mesmo cliente HTTP")
	}
	if bitrix.Timeout != 17*time.Second || ollama.Timeout != 8*time.Minute {
		t.Fatalf("timeouts incorretos: bitrix=%s ollama=%s", bitrix.Timeout, ollama.Timeout)
	}
}
