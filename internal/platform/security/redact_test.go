package security

import (
	"errors"
	"fmt"
	"strings"
	"testing"
)

func TestTextRedactsSecrets(t *testing.T) {
	t.Setenv("DATABASE_URL", "postgres://real:database-secret@db/auditor")
	inputs := []string{
		"https://portal.bitrix24.com/rest/958/token-secreto/crm.deal.get.json",
		"postgres://user:password@localhost/db",
		"Authorization: Bearer abc.def-123",
		"eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiIxIn0.signature",
		"token=secret api_key=secret2 password:secret3",
		"postgres://real:database-secret@db/auditor",
	}
	for _, input := range inputs {
		got := Text(input)
		for _, secret := range []string{"958", "token-secreto", "user", "password@", "abc.def-123", "eyJhbGci", "secret2", "secret3", "database-secret"} {
			if strings.Contains(got, secret) {
				t.Fatalf("segredo %q vazou em %q", secret, got)
			}
		}
	}
}

func TestErrorDoesNotChangeWrappedError(t *testing.T) {
	sentinel := errors.New("sentinel")
	err := fmt.Errorf("falha Bearer token-value: %w", sentinel)
	if strings.Contains(Error(err), "token-value") || !errors.Is(err, sentinel) {
		t.Fatal("erro encadeado vazou ou foi alterado")
	}
}
