package postgres

import (
	"context"
	"strings"
	"testing"
)

func TestOpenRequiresDatabaseURL(t *testing.T) {
	t.Setenv("DATABASE_URL", "")

	pool, err := Open(context.Background())
	if err == nil || pool != nil || !strings.Contains(err.Error(), "DATABASE_URL não configurada") {
		t.Fatalf("Open() = (%v, %v); esperava erro de configuração", pool, err)
	}
}

func TestOpenDoesNotExposeInvalidDatabaseURL(t *testing.T) {
	const databaseURL = "postgres://usuario:senha-super-secreta@%zz/banco"
	t.Setenv("DATABASE_URL", databaseURL)

	pool, err := Open(context.Background())
	if err == nil || pool != nil {
		t.Fatalf("Open() = (%v, %v); esperava erro", pool, err)
	}
	if strings.Contains(err.Error(), databaseURL) || strings.Contains(err.Error(), "senha-super-secreta") {
		t.Fatalf("erro expôs credenciais: %q", err)
	}
	if !strings.Contains(err.Error(), "configurar pool PostgreSQL") {
		t.Fatalf("erro sem contexto: %q", err)
	}
}

func TestOpenHonorsCanceledContextWithoutRealDatabase(t *testing.T) {
	const databaseURL = "postgres://usuario:outra-senha@127.0.0.1:5432/banco"
	t.Setenv("DATABASE_URL", databaseURL)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	pool, err := Open(ctx)
	if err == nil || pool != nil {
		t.Fatalf("Open() = (%v, %v); esperava erro", pool, err)
	}
	if !strings.Contains(err.Error(), "validar conexão PostgreSQL") {
		t.Fatalf("erro sem contexto de Ping: %q", err)
	}
	if strings.Contains(err.Error(), databaseURL) || strings.Contains(err.Error(), "outra-senha") {
		t.Fatalf("erro expôs credenciais: %q", err)
	}
}
