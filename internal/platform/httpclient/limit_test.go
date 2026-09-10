package httpclient

import (
	"context"
	"errors"
	"net/http"
	"sync"
	"sync/atomic"
	"testing"
	"time"
)

type roundTripFunc func(*http.Request) (*http.Response, error)

func (f roundTripFunc) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func TestLimitTransportBoundsConcurrency(t *testing.T) {
	var active, maxActive atomic.Int32
	release := make(chan struct{})
	base := roundTripFunc(func(*http.Request) (*http.Response, error) {
		current := active.Add(1)
		defer active.Add(-1)
		for {
			old := maxActive.Load()
			if current <= old || maxActive.CompareAndSwap(old, current) {
				break
			}
		}
		<-release
		return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
	})
	transport := NewLimitTransport(base, 2)
	var group sync.WaitGroup
	for range 5 {
		group.Add(1)
		go func() {
			defer group.Done()
			request, _ := http.NewRequestWithContext(context.Background(), http.MethodGet, "http://example.test", nil)
			_, _ = transport.RoundTrip(request)
		}()
	}
	deadline := time.After(time.Second)
	for maxActive.Load() < 2 {
		select {
		case <-deadline:
			t.Fatal("requisições não iniciaram")
		default:
			time.Sleep(time.Millisecond)
		}
	}
	if maxActive.Load() != 2 {
		t.Fatalf("concorrência=%d", maxActive.Load())
	}
	close(release)
	group.Wait()
	if maxActive.Load() != 2 {
		t.Fatalf("máxima=%d", maxActive.Load())
	}
}

func TestLimitTransportHonorsContextWhileWaiting(t *testing.T) {
	release := make(chan struct{})
	transport := NewLimitTransport(roundTripFunc(func(*http.Request) (*http.Response, error) {
		<-release
		return &http.Response{StatusCode: 200, Body: http.NoBody}, nil
	}), 1)
	request, _ := http.NewRequest(http.MethodGet, "http://example.test", nil)
	go transport.RoundTrip(request)
	time.Sleep(10 * time.Millisecond)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	waiting, _ := http.NewRequestWithContext(ctx, http.MethodGet, "http://example.test", nil)
	if _, err := transport.RoundTrip(waiting); !errors.Is(err, context.Canceled) {
		t.Fatalf("err=%v", err)
	}
	close(release)
}

func TestTwoWorkersWithOllamaConcurrencyOneRunSequentially(t *testing.T) {
	firstStarted := make(chan struct{})
	firstRelease := make(chan struct{})
	secondStarted := make(chan struct{})
	var calls atomic.Int32
	base := roundTripFunc(func(*http.Request) (*http.Response, error) {
		call := calls.Add(1)
		if call == 1 {
			close(firstStarted)
			<-firstRelease
		} else {
			close(secondStarted)
		}
		return &http.Response{StatusCode: http.StatusOK, Body: http.NoBody}, nil
	})
	transport := NewLimitTransport(base, 1)
	var workers sync.WaitGroup
	for range 2 {
		workers.Add(1)
		go func() {
			defer workers.Done()
			request, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "http://ollama.test/api/generate", nil)
			_, _ = transport.RoundTrip(request)
		}()
	}
	select {
	case <-firstStarted:
	case <-time.After(time.Second):
		t.Fatal("primeiro worker não iniciou")
	}
	select {
	case <-secondStarted:
		t.Fatal("segundo worker entrou no Ollama antes de o primeiro concluir")
	case <-time.After(30 * time.Millisecond):
	}
	close(firstRelease)
	select {
	case <-secondStarted:
	case <-time.After(time.Second):
		t.Fatal("segundo worker não prosseguiu após o primeiro concluir")
	}
	workers.Wait()
	if calls.Load() != 2 {
		t.Fatalf("chamadas=%d", calls.Load())
	}
}
