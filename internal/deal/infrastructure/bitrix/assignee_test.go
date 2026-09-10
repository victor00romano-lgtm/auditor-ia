package bitrix

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestGetAssigneeBuildsDisplayName(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/user.get.json" {
			t.Fatalf("método inesperado: %s", r.URL.Path)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":[{"ID":"954","NAME":"Flávia","LAST_NAME":"Rita","ACTIVE":"Y"}]}`))
	}))
	defer server.Close()

	got, err := GetAssignee(context.Background(), server.URL, server.Client(), 954)
	if err != nil {
		t.Fatal(err)
	}
	if got.BitrixUserID != 954 || got.DisplayName != "Flávia Rita" || got.Active == nil || !*got.Active {
		t.Fatalf("responsável incorreto: %#v", got)
	}
}

func TestGetAssigneeAcceptsBooleanActive(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":[{"ID":"952","NAME":"Maria","LAST_NAME":"Silva","ACTIVE":true}]}`))
	}))
	defer server.Close()
	got, err := GetAssignee(context.Background(), server.URL, server.Client(), 952)
	if err != nil || got.Active == nil || !*got.Active {
		t.Fatalf("ACTIVE booleano não aceito: %#v err=%v", got, err)
	}
}

func TestGetAssigneeFallsBackWithoutPersonalData(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":[{"ID":"3056"}]}`))
	}))
	defer server.Close()
	got, err := GetAssignee(context.Background(), server.URL, server.Client(), 3056)
	if err != nil || got.DisplayName != "Usuário Bitrix #3056" {
		t.Fatalf("fallback incorreto: %#v err=%v", got, err)
	}
}
