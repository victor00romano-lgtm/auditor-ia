package privacy

import (
	"strings"
	"testing"
)

func TestAnonymizeIsDeterministicPerSession(t *testing.T) {
	s := NewSession("anonymize")
	s.RegisterName("Maria Silva", "CLIENTE")
	got := s.Transform("Maria Silva maria@example.com +55 11 99999-1111 e maria@example.com IP 10.0.0.1 CPF 123.456.789-00 https://example.com/a")
	for _, v := range []string{"CLIENTE_1", "EMAIL_1", "TELEFONE_1", "IP_1", "DOCUMENTO_1", "URL_1"} {
		if !strings.Contains(got, v) {
			t.Fatalf("alias %s ausente em %q", v, got)
		}
	}
	if strings.Count(got, "EMAIL_1") != 2 {
		t.Fatal("alias inconsistente")
	}
}
func TestPreserveKeepsPIIButAlwaysRemovesSecrets(t *testing.T) {
	s := NewSession("preserve")
	got := s.Transform("maria@example.com Bearer abc123 https://example.com/page token=secret")
	if !strings.Contains(got, "maria@example.com") || !strings.Contains(got, "https://example.com/page") || strings.Contains(got, "abc123") || strings.Contains(got, "secret") {
		t.Fatalf("preserve inseguro: %q", got)
	}
}
