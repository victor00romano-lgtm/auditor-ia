package bitrix

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestMapDealMapsClosedYN(t *testing.T) {
	for _, tt := range []struct {
		value string
		want  bool
	}{
		{value: "Y", want: true},
		{value: "N", want: false},
	} {
		t.Run(tt.value, func(t *testing.T) {
			deal, err := mapDeal(json.RawMessage(`{"ID":"8620","CLOSED":"` + tt.value + `"}`))
			if err != nil {
				t.Fatal(err)
			}
			if deal.Closed == nil || *deal.Closed != tt.want {
				t.Fatalf("CLOSED %q = %#v; esperado %v", tt.value, deal.Closed, tt.want)
			}
		})
	}
}

func TestMapDealPreservesDecimalAndMapsFields(t *testing.T) {
	deal, err := mapDeal(json.RawMessage(`{
		"ID":"8620",
		"TITLE":"Negócio real",
		"STAGE_ID":"C1:NEW",
		"STAGE_SEMANTIC_ID":"P",
		"CLOSED":"N",
		"OPPORTUNITY":"1234567890.1200",
		"CURRENCY_ID":"BRL",
		"ASSIGNED_BY_ID":"77",
		"DATE_CREATE":"2026-08-01T10:20:30-03:00",
		"DATE_MODIFY":"2026-08-07T11:22:33-03:00"
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if deal.BitrixDealID != 8620 || deal.Title == nil || *deal.Title != "Negócio real" || deal.Amount == nil || *deal.Amount != "1234567890.1200" || deal.AssignedByID == nil || *deal.AssignedByID != 77 {
		t.Fatalf("mapeamento inesperado: %#v", deal)
	}
	if deal.CreatedAtBitrix == nil || deal.UpdatedAtBitrix == nil {
		t.Fatalf("datas ausentes: %#v", deal)
	}

	numericDeal, err := mapDeal(json.RawMessage(`{"ID":8621,"OPPORTUNITY":100.2500}`))
	if err != nil {
		t.Fatal(err)
	}
	if numericDeal.Amount == nil || *numericDeal.Amount != "100.2500" {
		t.Fatalf("decimal numérico perdeu precisão textual: %#v", numericDeal.Amount)
	}
}

func TestMapDealTreatsEmptyAndNullFieldsAsAbsent(t *testing.T) {
	deal, err := mapDeal(json.RawMessage(`{
		"ID":"8620", "TITLE":"", "STAGE_ID":null, "STAGE_SEMANTIC_ID":"",
		"CLOSED":null, "OPPORTUNITY":"", "CURRENCY_ID":null,
		"ASSIGNED_BY_ID":"0", "DATE_CREATE":"", "DATE_MODIFY":null
	}`))
	if err != nil {
		t.Fatal(err)
	}
	if deal.Title != nil || deal.StageID != nil || deal.StageSemanticID != nil || deal.Closed != nil || deal.Amount != nil || deal.Currency != nil || deal.AssignedByID != nil || deal.CreatedAtBitrix != nil || deal.UpdatedAtBitrix != nil {
		t.Fatalf("campos opcionais deveriam estar ausentes: %#v", deal)
	}
}

func TestMapDealRejectsInvalidDates(t *testing.T) {
	_, err := mapDeal(json.RawMessage(`{"ID":"8620","DATE_CREATE":"07/08/2026"}`))
	if err == nil || !strings.Contains(err.Error(), "DATE_CREATE inválido") {
		t.Fatalf("erro inesperado: %v", err)
	}
}

func TestSourceGetUsesCRMDealGet(t *testing.T) {
	fixedNow := time.Date(2026, 8, 7, 15, 0, 0, 0, time.UTC)
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/crm.deal.get.json" {
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["ID"] != float64(8620) {
			t.Fatalf("payload inesperado: %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":{"ID":"8620","TITLE":"Cliente Ágil","CLOSED":"Y","OPPORTUNITY":"99.90"}}`))
	}))
	defer server.Close()

	deal, err := (Source{Webhook: server.URL, Client: server.Client(), Now: func() time.Time { return fixedNow }}).Get(context.Background(), "8620")
	if err != nil {
		t.Fatal(err)
	}
	if deal.BitrixDealID != 8620 || deal.Title == nil || *deal.Title != "Cliente Ágil" || deal.SyncedAt == nil || !deal.SyncedAt.Equal(fixedNow) {
		t.Fatalf("negócio inesperado: %#v", deal)
	}
}

func TestSourceGetReportsMissingAndInvalidBitrixResponses(t *testing.T) {
	for _, tt := range []struct {
		name string
		body string
		want string
	}{
		{name: "inexistente", body: `{"result":false}`, want: "não encontrado"},
		{name: "resposta inválida", body: `{"result":{"TITLE":"sem ID"}}`, want: "ID ausente"},
	} {
		t.Run(tt.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				_, _ = w.Write([]byte(tt.body))
			}))
			defer server.Close()
			_, err := (Source{Webhook: server.URL, Client: server.Client()}).Get(context.Background(), "8620")
			if err == nil || !strings.Contains(err.Error(), tt.want) {
				t.Fatalf("erro = %v; esperado conter %q", err, tt.want)
			}
		})
	}
}
