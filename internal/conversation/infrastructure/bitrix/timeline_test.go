package bitrix

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/portfolio/auditor-ia/internal/conversation/domain"
)

func TestSanitize(t *testing.T) {
	raw := `[img]https://example.com/icon.png[/img]&nbsp; Cliente (5531999999999): fale comigo em pessoa@example.com`
	got := Sanitize(raw)
	if strings.Contains(got, "example.com/icon") || strings.Contains(got, "pessoa@example.com") {
		t.Fatalf("dado sensível não removido: %q", got)
	}
	if !strings.Contains(got, "5531999999999") || !strings.Contains(got, "[email]") {
		t.Fatalf("marcadores ausentes: %q", got)
	}
}

func TestConversationUsesAssociatedEntityAsSessionIDAndClassifiesAccessDenied(t *testing.T) {
	for _, code := range []string{"ACCESS_DENIED", "ACCESS_ERROR"} {
		t.Run(code, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/crm.activity.list.json":
					_, _ = w.Write([]byte(`{"result":[{"ID":"16","PROVIDER_ID":"IMOPENLINES_SESSION","ASSOCIATED_ENTITY_ID":"14","ORIGIN_ID":"IMOL_14"}]}`))
				case "/imopenlines.session.history.get.json":
					var body map[string]any
					_ = json.NewDecoder(r.Body).Decode(&body)
					if body["SESSION_ID"] != "14" || body["CHAT_ID"] != nil {
						t.Fatalf("sessão enviada incorretamente: %#v", body)
					}
					w.WriteHeader(http.StatusForbidden)
					_, _ = w.Write([]byte(`{"error":"` + code + `","error_description":"segredo que não deve vazar"}`))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()

			got, err := (Timeline{Webhook: server.URL, Client: server.Client()}).Conversation(context.Background(), "52")
			if err != nil || got.Status != domain.ConversationAccessDenied || got.SessionID != "14" || got.ErrorCode != code {
				t.Fatalf("resultado inesperado: %#v err=%v", got, err)
			}
		})
	}
}

func TestConversationDoesNotMaskOperationalHTTPFailures(t *testing.T) {
	for _, status := range []int{http.StatusUnauthorized, http.StatusTooManyRequests, http.StatusInternalServerError} {
		t.Run(http.StatusText(status), func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				w.WriteHeader(status)
				_, _ = w.Write([]byte(`{"error":"OPERATIONAL","error_description":"token=secret"}`))
			}))
			defer server.Close()
			_, err := (Timeline{Webhook: server.URL, Client: server.Client()}).Conversation(context.Background(), "52")
			if err == nil || strings.Contains(err.Error(), "token=secret") {
				t.Fatalf("erro operacional mascarado ou inseguro: %v", err)
			}
		})
	}
}

func TestConversationDoesNotMaskTimeout(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(http.ResponseWriter, *http.Request) { time.Sleep(100 * time.Millisecond) }))
	defer server.Close()
	_, err := (Timeline{Webhook: server.URL, Client: &http.Client{Timeout: 10 * time.Millisecond}}).Conversation(context.Background(), "52")
	if err == nil {
		t.Fatal("timeout foi mascarado como ausência de conversa")
	}
}

func TestConversationStates(t *testing.T) {
	for _, test := range []struct {
		name       string
		activity   string
		history    string
		wantStatus domain.ConversationStatus
		wantCount  int
	}{
		{name: "no conversation", activity: `{"result":[]}`, wantStatus: domain.ConversationNoConversation},
		{name: "empty conversation", activity: `{"result":[{"ID":"16","ASSOCIATED_ENTITY_ID":"14"}]}`, history: `{"result":{"users":{},"message":{}}}`, wantStatus: domain.ConversationEmpty},
		{name: "available", activity: `{"result":[{"ID":"16","ASSOCIATED_ENTITY_ID":"14"}]}`, history: `{"result":{"session":{"managerList":["7"]},"users":{"7":{"id":"7","type":"employee"}},"message":{"1":{"id":"1","senderId":"7","date":"2026-08-12T12:00:00-03:00","text":"mensagem"}}}}`, wantStatus: domain.ConversationAvailable, wantCount: 1},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
				w.Header().Set("Content-Type", "application/json")
				switch r.URL.Path {
				case "/crm.activity.list.json":
					_, _ = w.Write([]byte(test.activity))
				case "/imopenlines.session.history.get.json":
					_, _ = w.Write([]byte(test.history))
				case "/imopenlines.crm.chat.get.json":
					_, _ = w.Write([]byte(`{"result":[]}`))
				case "/crm.deal.get.json":
					_, _ = w.Write([]byte(`{"result":{"CONTACT_ID":"0","DATE_CREATE":"","CLOSEDATE":""}}`))
				default:
					http.NotFound(w, r)
				}
			}))
			defer server.Close()
			got, err := (Timeline{Webhook: server.URL, Client: server.Client()}).Conversation(context.Background(), "52")
			if err != nil || got.Status != test.wantStatus || len(got.Messages) != test.wantCount {
				t.Fatalf("estado incorreto: %#v err=%v", got, err)
			}
		})
	}
}

func TestMessagesReadsAllOpenLinesChats(t *testing.T) {
	var calls []string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		calls = append(calls, r.URL.Path)
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/imopenlines.crm.chat.get.json":
			var body map[string]any
			if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
				t.Fatal(err)
			}
			if body["CRM_ENTITY_TYPE"] != "deal" || body["ACTIVE_ONLY"] != "N" || body["CRM_ENTITY"] != float64(42) {
				t.Fatalf("parâmetros inesperados: %#v", body)
			}
			_, _ = w.Write([]byte(`{"result":[{"CHAT_ID":"20"},{"CHAT_ID":10},{"CHAT_ID":"20"}]}`))
		case "/crm.deal.get.json":
			_, _ = w.Write([]byte(`{"result":{"CONTACT_ID":"0","DATE_CREATE":"2025-01-01T00:00:00-03:00","CLOSEDATE":"2025-12-31T23:59:59-03:00"}}`))
		case "/imopenlines.session.history.get.json":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["CHAT_ID"] == "20" {
				_, _ = w.Write([]byte(`{"result":{"session":{"managerList":["7"]},"users":{"7":{"id":"7","name":"Ana Vendedora","type":"employee"}},"message":{"2":{"id":"2","senderid":"7","date":"2025-01-02T12:00:00-03:00","text":"Segunda, Ana Vendedora"},"99":{"id":"99","senderid":"0","date":"2025-01-02T12:01:00-03:00","text":"mensagem de sistema"}}}}`))
				return
			}
			_, _ = w.Write([]byte(`{"result":{"session":{"managerList":["7"]},"users":{"55":{"id":"55","name":"João Cliente","type":"connector","phone":"5531999999999"},"7":{"id":"7","name":"Ana Vendedora","type":"employee"}},"files":{"900":{"id":900,"name":"comprovante.png","type":"image"}},"message":{"1":{"id":1,"senderId":"55","date":"2025-01-01T12:00:00-03:00","text":"Primeira de João Cliente, 5531999999999, pessoa@example.com"},"2":{"id":"2","senderId":"7","date":"2025-01-02T12:00:00-03:00","text":"duplicada"},"3":{"id":"3","senderId":"55","date":"2025-01-03T12:00:00-03:00","text":"","params":{"fileId":[900]}}}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	got, err := (Timeline{Webhook: server.URL + "/", Client: server.Client()}).Messages(context.Background(), "42")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 3 {
		t.Fatalf("esperava 3 mensagens úteis, incluindo imagem, obteve %#v", got)
	}
	if got[0].ID != "1" || got[0].SessionID != "10" || got[0].SenderID != "55" || got[0].SenderName != "João Cliente" || !strings.Contains(got[0].Text, "5531999999999") || !strings.Contains(got[0].Text, "Cliente") || got[0].Role != "CLIENTE" || got[0].ChatOrigin != "deal" || got[1].Text != "Segunda, Ana Vendedora" || got[1].ID != "2" || got[1].SessionID != "20" || got[1].SenderID != "7" || got[1].SenderName != "Ana Vendedora" || got[1].Role != "ATENDENTE" || got[2].ID != "3" || got[2].Role != "CLIENTE" || got[2].CreatedAt.IsZero() || len(got[2].Attachments) != 1 || got[2].Attachments[0].ID != "900" || !got[2].Attachments[0].IsImage {
		t.Fatalf("mensagens não normalizadas/ordenadas: %#v", got)
	}
	wantCalls := []string{"/imopenlines.crm.chat.get.json", "/crm.deal.get.json", "/imopenlines.session.history.get.json", "/imopenlines.session.history.get.json"}
	if !reflect.DeepEqual(calls, wantCalls) {
		t.Fatalf("chamadas = %#v; esperado %#v", calls, wantCalls)
	}
}

func TestReferencedAttachmentsUsesResultFilesAndParamsFileID(t *testing.T) {
	var files historyFiles
	if err := json.Unmarshal([]byte(`{"10":{"id":10,"name":"foto.bin","type":"image"},"20":{"id":"20","name":"recibo.webp","type":"file"},"30":{"id":30,"name":"contrato.pdf","type":"file"}}`), &files); err != nil {
		t.Fatal(err)
	}
	tests := []struct {
		name   string
		params string
		wantID string
	}{
		{name: "ID numérico", params: `{"fileId":10}`, wantID: "10"},
		{name: "ID textual", params: `{"fileId":"20"}`, wantID: "20"},
		{name: "IDs em array", params: `{"fileId":[30,"20"]}`, wantID: "20"},
		{name: "IDs em objeto", params: `{"fileId":{"item":"10"}}`, wantID: "10"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			attachments := referencedAttachments(json.RawMessage(tt.params), files)
			var found *domain.Attachment
			for index := range attachments {
				if attachments[index].ID == tt.wantID {
					found = &attachments[index]
				}
			}
			if found == nil || !found.IsImage {
				t.Fatalf("imagem %q não vinculada: %#v", tt.wantID, attachments)
			}
		})
	}
}

func TestHistoryFilesAcceptsEmptyArray(t *testing.T) {
	var files historyFiles
	if err := json.Unmarshal([]byte(`[]`), &files); err != nil {
		t.Fatal(err)
	}
	if len(files) != 0 {
		t.Fatalf("esperava mapa vazio, obteve %#v", files)
	}
}

func TestClassifySenderUsesAtendenteForInternalUsers(t *testing.T) {
	tests := []struct {
		name     string
		id       string
		user     historyUser
		managers map[string]struct{}
	}{
		{name: "gerente da sessão", id: "7", managers: map[string]struct{}{"7": {}}},
		{name: "funcionário interno", id: "8", user: historyUser{Type: "employee"}, managers: map[string]struct{}{}},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := classifySender(tt.id, tt.user, true, tt.managers); got != "ATENDENTE" {
				t.Fatalf("classifySender() = %q; esperado ATENDENTE", got)
			}
		})
	}
}

func TestHistoryUsersUnmarshalJSON(t *testing.T) {
	tests := []struct {
		name string
		json string
	}{
		{name: "objeto indexado por ID", json: `{"7":{"id":"7","name":"Ana","type":"employee"}}`},
		{name: "array de usu\u00e1rios", json: `[{"id":"7","name":"Ana","type":"employee"}]`},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			var users historyUsers
			if err := json.Unmarshal([]byte(tt.json), &users); err != nil {
				t.Fatal(err)
			}
			if len(users) != 1 || users["7"].Name != "Ana" || users["7"].Type != "employee" {
				t.Fatalf("usu\u00e1rios n\u00e3o normalizados: %#v", users)
			}
		})
	}
}

func TestHistoryUsersUnmarshalJSONPropagatesDecodeErrors(t *testing.T) {
	var users historyUsers
	if err := json.Unmarshal([]byte(`[{"id":{"inv\u00e1lido":true}}]`), &users); err == nil {
		t.Fatal("esperava erro ao decodificar ID inv\u00e1lido")
	}
}

func TestMessagesDiscardsSenderIDZero(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		if strings.Contains(r.URL.Path, "crm.chat.get") {
			_, _ = w.Write([]byte(`{"result":[{"CHAT_ID":"10"}]}`))
			return
		}
		_, _ = w.Write([]byte(`{"result":{"users":{"5":{"id":"5","type":"extranet"}},"message":{"10":{"id":"10","senderid":"0","date":"2025-01-01T12:00:00-03:00","text":"sistema"},"11":{"id":"11","senderid":"5","date":"2025-01-01T12:01:00-03:00","text":"humana"}}}}`))
	}))
	defer server.Close()

	got, err := (Timeline{Webhook: server.URL, Client: server.Client()}).Messages(context.Background(), "4")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "11" || got[0].Text != "humana" {
		t.Fatalf("senderid 0 não foi descartado: %#v", got)
	}
}

func TestMessagesPropagatesBitrixError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"ACCESS_DENIED","error_description":"denied"}`))
	}))
	defer server.Close()

	_, err := (Timeline{Webhook: server.URL, Client: server.Client()}).Messages(context.Background(), "42")
	if err == nil || !strings.Contains(err.Error(), "ACCESS_DENIED") {
		t.Fatalf("erro inesperado: %v", err)
	}
}

func TestMessagesRejectsInvalidDealID(t *testing.T) {
	_, err := (Timeline{Webhook: "https://example.invalid/rest/"}).Messages(context.Background(), "D-42")
	if err == nil || !strings.Contains(err.Error(), "inválido") {
		t.Fatalf("erro inesperado: %v", err)
	}
}

func TestStatusReadsDealStage(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/crm.deal.get.json" {
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		if body["ID"] != float64(42) {
			t.Fatalf("ID inesperado: %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":{"STAGE_ID":"WON","STAGE_SEMANTIC_ID":"S","CLOSED":"Y","ASSIGNED_BY_ID":"3066"}}`))
	}))
	defer server.Close()

	got, err := (Timeline{Webhook: server.URL, Client: server.Client()}).Status(context.Background(), "42")
	if err != nil {
		t.Fatal(err)
	}
	want := domain.DealStatus{StageID: "WON", SemanticID: "S", Closed: true, AssignedByID: 3066}
	if got != want {
		t.Fatalf("Status() = %#v; esperado %#v", got, want)
	}
}

func TestStatusPropagatesBitrixAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"ACCESS_DENIED","error_description":"denied"}`))
	}))
	defer server.Close()

	_, err := (Timeline{Webhook: server.URL, Client: server.Client()}).Status(context.Background(), "42")
	if err == nil || !strings.Contains(err.Error(), "ACCESS_DENIED") {
		t.Fatalf("erro inesperado: %v", err)
	}
}

func TestStatusPropagatesInvalidJSON(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		_, _ = w.Write([]byte(`{"result":`))
	}))
	defer server.Close()

	_, err := (Timeline{Webhook: server.URL, Client: server.Client()}).Status(context.Background(), "42")
	if err == nil || !strings.Contains(err.Error(), "decodificar resposta") {
		t.Fatalf("erro inesperado: %v", err)
	}
}

func TestStatusRejectsInvalidSemanticID(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":{"STAGE_ID":"CUSTOM","STAGE_SEMANTIC_ID":"X","CLOSED":"N"}}`))
	}))
	defer server.Close()

	_, err := (Timeline{Webhook: server.URL, Client: server.Client()}).Status(context.Background(), "42")
	if err == nil || !strings.Contains(err.Error(), `STAGE_SEMANTIC_ID inválido: "X"`) {
		t.Fatalf("erro inesperado: %v", err)
	}
}

func TestMessagesUsesContactChatWhenDealHasNone(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/imopenlines.crm.chat.get.json":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["CRM_ENTITY_TYPE"] == "contact" {
				_, _ = w.Write([]byte(`{"result":[{"CHAT_ID":"30"}]}`))
			} else {
				_, _ = w.Write([]byte(`{"result":[]}`))
			}
		case "/crm.deal.get.json":
			_, _ = w.Write([]byte(`{"result":{"CONTACT_ID":55,"DATE_CREATE":"2025-01-01T00:00:00-03:00","CLOSEDATE":"2025-01-31T23:59:59-03:00"}}`))
		case "/imopenlines.session.history.get.json":
			_, _ = w.Write([]byte(`{"result":{"session":{"managerList":[]},"users":{"55":{"id":55,"type":"connector"}},"files":[],"message":{"10":{"id":10,"senderId":55,"date":"2025-01-15T12:00:00-03:00","text":"chat do contato"}}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	got, err := (Timeline{Webhook: server.URL, Client: server.Client()}).Messages(context.Background(), "42")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "10" || got[0].ChatOrigin != "contact" {
		t.Fatalf("mensagem do contato não recuperada: %#v", got)
	}
}

func TestMessagesIgnoresNullEmptyZeroAndInvalidChatIDs(t *testing.T) {
	historyCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/imopenlines.crm.chat.get.json":
			_, _ = w.Write([]byte(`{"result":[{"CHAT_ID":null},{"CHAT_ID":""},{"CHAT_ID":0},{"CHAT_ID":{"invalid":true}}]}`))
		case "/crm.deal.get.json":
			_, _ = w.Write([]byte(`{"result":{"CONTACT_ID":null,"DATE_CREATE":"","CLOSEDATE":""}}`))
		case "/imopenlines.session.history.get.json":
			historyCalls++
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	got, err := (Timeline{Webhook: server.URL, Client: server.Client()}).Messages(context.Background(), "42")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 0 || historyCalls != 0 {
		t.Fatalf("CHAT_ID inválido não foi ignorado: mensagens=%#v chamadas=%d", got, historyCalls)
	}
}

func TestMessagesDeduplicatesDealAndContactChats(t *testing.T) {
	historyCalls := 0
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/imopenlines.crm.chat.get.json":
			_, _ = w.Write([]byte(`{"result":[{"CHAT_ID":"30"},{"CHAT_ID":30}]}`))
		case "/crm.deal.get.json":
			_, _ = w.Write([]byte(`{"result":{"CONTACT_ID":"55","DATE_CREATE":"2025-01-01T00:00:00-03:00","CLOSEDATE":"2025-01-31T23:59:59-03:00"}}`))
		case "/imopenlines.session.history.get.json":
			historyCalls++
			_, _ = w.Write([]byte(`{"result":{"session":{"managerList":[]},"users":{"55":{"id":55,"type":"connector"}},"files":[],"message":{"10":{"id":10,"senderId":55,"date":"2025-01-15T12:00:00-03:00","text":"uma vez"}}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	got, err := (Timeline{Webhook: server.URL, Client: server.Client()}).Messages(context.Background(), "42")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || historyCalls != 1 || got[0].ChatOrigin != "deal" {
		t.Fatalf("chats não deduplicados: mensagens=%#v chamadas=%d", got, historyCalls)
	}
}

func TestMessagesLimitsContactMessagesToDealPeriod(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		switch r.URL.Path {
		case "/imopenlines.crm.chat.get.json":
			var body map[string]any
			_ = json.NewDecoder(r.Body).Decode(&body)
			if body["CRM_ENTITY_TYPE"] == "contact" {
				_, _ = w.Write([]byte(`{"result":[{"CHAT_ID":"30"}]}`))
			} else {
				_, _ = w.Write([]byte(`{"result":[]}`))
			}
		case "/crm.deal.get.json":
			_, _ = w.Write([]byte(`{"result":{"CONTACT_ID":"55","DATE_CREATE":"2025-02-01T00:00:00-03:00","CLOSEDATE":"2025-02-28T23:59:59-03:00"}}`))
		case "/imopenlines.session.history.get.json":
			_, _ = w.Write([]byte(`{"result":{"session":{"managerList":[]},"users":{"55":{"id":55,"type":"connector"}},"files":[],"message":{"1":{"id":1,"senderId":55,"date":"2025-01-31T23:59:59-03:00","text":"antes"},"2":{"id":2,"senderId":55,"date":"2025-02-15T12:00:00-03:00","text":"durante"},"3":{"id":3,"senderId":55,"date":"2025-03-01T00:00:00-03:00","text":"depois"}}}}`))
		default:
			http.NotFound(w, r)
		}
	}))
	defer server.Close()

	got, err := (Timeline{Webhook: server.URL, Client: server.Client()}).Messages(context.Background(), "42")
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].ID != "2" || got[0].Text != "durante" || got[0].ChatOrigin != "contact" {
		t.Fatalf("recorte temporal incorreto: %#v", got)
	}
}

func TestFormsReadsWebformFieldsAndDeduplicatesActivities(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/crm.activity.list.json" {
			http.NotFound(w, r)
			return
		}
		var body map[string]any
		if err := json.NewDecoder(r.Body).Decode(&body); err != nil {
			t.Fatal(err)
		}
		filter, _ := body["filter"].(map[string]any)
		selectFields, _ := body["select"].([]any)
		if filter["OWNER_TYPE_ID"] != float64(2) || filter["OWNER_ID"] != float64(42) || filter["PROVIDER_ID"] != "CRM_WEBFORM" || len(selectFields) != 1 || selectFields[0] != "*" {
			t.Fatalf("parâmetros inesperados: %#v", body)
		}
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":[
			{"ID":10,"PROVIDER_PARAMS":{"FORM":{"name":"Captação"},"VISITED_PAGES":["https://exemplo.test/página?a=1&b=2"],"FIELDS":[
				{"type":"text","code":"NAME","caption":"Nome","value":"João Ávila"},
				{"type":"list","code":"INTEREST","caption":"Interesses","value":["Crédito","PIX"]},
				{"type":"number","code":"VALUE","caption":"Valor","value":123.40},
				{"type":"boolean","code":"ACCEPT","caption":"Aceite","value":true},
				{"type":"text","code":"EMPTY","caption":"Vazio","value":null}
			]}},
			{"ID":"10","PROVIDER_PARAMS":{"FIELDS":[{"caption":"Duplicado","value":"ignorar"}]}},
			{"ID":"11","PROVIDER_PARAMS":{"FORM":"Formulário sem campos"}}
		]}`))
	}))
	defer server.Close()

	forms, err := (Timeline{Webhook: server.URL, Client: server.Client()}).Forms(context.Background(), "42")
	if err != nil {
		t.Fatal(err)
	}
	if len(forms) != 2 || forms[0].ActivityID != "10" || len(forms[0].Fields) != 5 || forms[1].ActivityID != "11" || len(forms[1].Fields) != 0 {
		t.Fatalf("formulários inesperados: %#v", forms)
	}
	if forms[0].Fields[0].Value != "João Ávila" {
		t.Fatalf("UTF-8 não preservado: %#v", forms[0].Fields[0].Value)
	}
	list, ok := forms[0].Fields[1].Value.([]any)
	if !ok || !reflect.DeepEqual(list, []any{"Crédito", "PIX"}) {
		t.Fatalf("lista não preservada: %#v", forms[0].Fields[1].Value)
	}
	number, ok := forms[0].Fields[2].Value.(json.Number)
	if !ok || number.String() != "123.40" || forms[0].Fields[3].Value != true || forms[0].Fields[4].Value != nil {
		t.Fatalf("valores simples não preservados: %#v", forms[0].Fields)
	}
}

func TestFormsAcceptsMissingProviderParams(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"result":[{"ID":"20"}]}`))
	}))
	defer server.Close()

	forms, err := (Timeline{Webhook: server.URL, Client: server.Client()}).Forms(context.Background(), "42")
	if err != nil {
		t.Fatal(err)
	}
	if len(forms) != 1 || forms[0].ActivityID != "20" || len(forms[0].Fields) != 0 {
		t.Fatalf("formulário ausente não tratado: %#v", forms)
	}
}

func TestFormsPropagatesBitrixAPIError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		w.WriteHeader(http.StatusForbidden)
		_, _ = w.Write([]byte(`{"error":"ACCESS_DENIED","error_description":"denied"}`))
	}))
	defer server.Close()

	_, err := (Timeline{Webhook: server.URL, Client: server.Client()}).Forms(context.Background(), "42")
	if err == nil || !strings.Contains(err.Error(), "ACCESS_DENIED") {
		t.Fatalf("erro inesperado: %v", err)
	}
}
