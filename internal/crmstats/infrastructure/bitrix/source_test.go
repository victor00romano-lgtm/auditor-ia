package bitrix

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"reflect"
	"sync/atomic"
	"testing"
	"time"

	statsdomain "github.com/portfolio/auditor-ia/internal/crmstats/domain"
)

func TestCollectDealsPaginatesAndDeduplicates(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Start int `json:"start"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		w.Header().Set("Content-Type", "application/json")
		if payload.Start == 0 {
			fmt.Fprint(w, `{"result":[{"ID":"1","TITLE":"A","ASSIGNED_BY_ID":"7","DATE_CREATE":"2026-08-01T10:00:00-03:00"},{"ID":"2","TITLE":"B"}],"next":2}`)
			return
		}
		fmt.Fprint(w, `{"result":[{"ID":"2","TITLE":"B atual"},{"ID":"3","TITLE":"C","ASSIGNED_BY_ID":"0"}]}`)
	}))
	defer server.Close()
	source := Source{Webhook: server.URL, Client: server.Client()}
	deals, err := source.CollectDeals(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(deals) != 3 || deals[0].BitrixDealID != 1 || deals[2].BitrixDealID != 3 || deals[0].AssignedByID == nil || *deals[0].AssignedByID != 7 || deals[2].AssignedByID != nil {
		t.Fatalf("negócios=%#v", deals)
	}
}

func TestCollectActivitiesPaginatesUsesExact3264AndDeduplicatesDeals(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var payload struct {
			Start int `json:"start"`
		}
		_ = json.NewDecoder(r.Body).Decode(&payload)
		if payload.Start == 0 {
			fmt.Fprint(w, `{"result":[{"ID":"1","OWNER_ID":"10","SUBJECT":"Instagram 3264","PROVIDER_ID":"IMOPENLINES_SESSION"},{"ID":"2","OWNER_ID":"10","SUBJECT":"3264 retorno","PROVIDER_ID":"IMOPENLINES_SESSION"},{"ID":"3","OWNER_ID":"11","SUBJECT":"13264","PROVIDER_ID":"IMOPENLINES_SESSION"}],"next":"3"}`)
			return
		}
		fmt.Fprint(w, `{"result":[{"ID":"4","OWNER_ID":"12","SUBJECT":"ticket 32640","PROVIDER_ID":"IMOPENLINES_SESSION"},{"ID":"5","OWNER_ID":"13","SUBJECT":"[3264] atendimento","PROVIDER_ID":"IMOPENLINES_SESSION"}]}`)
	}))
	defer server.Close()
	ids, activities, err := (Source{Webhook: server.URL, Client: server.Client()}).CollectMarker3264(context.Background(), nil)
	if err != nil || activities != 5 || !reflect.DeepEqual(ids, []int64{10, 13}) {
		t.Fatalf("ids=%v activities=%d err=%v", ids, activities, err)
	}
	for _, value := range []struct {
		subject string
		want    bool
	}{{"3264", true}, {"x3264y", true}, {"13264", false}, {"32640", false}, {"132640", false}} {
		if got := containsExact3264(value.subject); got != value.want {
			t.Fatalf("%q=%t", value.subject, got)
		}
	}
}

func TestActivitiesAccessDeniedIsUnavailable(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"error":"ACCESS_DENIED","error_description":"denied"}`)
	}))
	defer server.Close()
	_, _, err := (Source{Webhook: server.URL, Client: server.Client()}).CollectMarker3264(context.Background(), nil)
	if err != statsdomain.ErrMarkerUnavailable {
		t.Fatalf("err=%v", err)
	}
}

func TestRetriesHTTP429AndHonorsCancellationDuringPageDelay(t *testing.T) {
	var calls atomic.Int32
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if calls.Add(1) == 1 {
			w.WriteHeader(http.StatusTooManyRequests)
			fmt.Fprint(w, `{"error":"QUERY_LIMIT_EXCEEDED"}`)
			return
		}
		fmt.Fprint(w, `{"result":[]}`)
	}))
	defer server.Close()
	if _, err := (Source{Webhook: server.URL, Client: server.Client(), MaxRetries: 2}).CollectDeals(context.Background(), nil); err != nil || calls.Load() != 2 {
		t.Fatalf("calls=%d err=%v", calls.Load(), err)
	}

	server.Config.Handler = http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, `{"result":[],"next":1}`)
	})
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	_, err := (Source{Webhook: server.URL, Client: server.Client(), PageDelay: time.Second}).CollectDeals(ctx, nil)
	if err == nil {
		t.Fatal("cancelamento ignorado")
	}
}
