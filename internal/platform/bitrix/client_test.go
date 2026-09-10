package bitrix

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func TestClientReturnsTypedSafeAccessErrors(t *testing.T) {
	for _, code := range []string{"ACCESS_DENIED", "ACCESS_ERROR"} {
		t.Run(code, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				if r.URL.Path != "/imopenlines.dialog.get.json" {
					http.NotFound(w, r)
					return
				}
				w.WriteHeader(http.StatusForbidden)
				_, _ = w.Write([]byte(`{"error":"` + code + `","error_description":"webhook https://secret.example/rest/1/token"}`))
			}))
			defer server.Close()
			err := (Client{Webhook: server.URL, HTTP: server.Client()}).Call(context.Background(), "imopenlines.dialog.get.json", map[string]any{"DIALOG_ID": "chat14"}, &struct{}{})
			var apiErr *APIError
			if !errors.Is(err, ErrAccessDenied) || !errors.As(err, &apiErr) || apiErr.Code != code {
				t.Fatalf("erro não tipado: %T %v", err, err)
			}
			if strings.Contains(err.Error(), "secret.example") || strings.Contains(err.Error(), "token") {
				t.Fatalf("credencial vazou no erro: %v", err)
			}
		})
	}
}

func TestClientKeepsOperationalHTTPFailuresDistinct(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusTooManyRequests, http.StatusBadGateway} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":"OPERATIONAL","error_description":"credential=secret"}`))
			}))
			defer server.Close()
			err := (Client{Webhook: server.URL, HTTP: server.Client()}).Call(context.Background(), "crm.activity.list.json", map[string]any{}, &struct{}{})
			if err == nil || errors.Is(err, ErrAccessDenied) || strings.Contains(err.Error(), "credential=secret") {
				t.Fatalf("erro operacional incorreto: %v", err)
			}
		})
	}
}
