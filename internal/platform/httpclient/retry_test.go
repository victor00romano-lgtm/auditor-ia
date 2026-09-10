package httpclient

import (
	"context"
	"io"
	"net/http"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

type retryRoundTripFunc func(*http.Request) (*http.Response, error)

func (f retryRoundTripFunc) RoundTrip(request *http.Request) (*http.Response, error) {
	return f(request)
}

func TestRetryTransportRetries429AndReplaysBody(t *testing.T) {
	var calls atomic.Int32
	transport := RetryTransport{MaxAttempts: 3, BaseDelay: time.Millisecond, Next: retryRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		calls.Add(1)
		body, _ := io.ReadAll(request.Body)
		if string(body) != `{"id":52}` {
			t.Fatalf("corpo não repetido: %q", body)
		}
		status := http.StatusOK
		if calls.Load() == 1 {
			status = http.StatusTooManyRequests
		}
		return &http.Response{StatusCode: status, Header: http.Header{"Retry-After": []string{"0"}}, Body: io.NopCloser(strings.NewReader("{}")), Request: request}, nil
	})}
	request, _ := http.NewRequestWithContext(context.Background(), http.MethodPost, "https://example.invalid", strings.NewReader(`{"id":52}`))
	response, err := transport.RoundTrip(request)
	if err != nil || response.StatusCode != http.StatusOK || calls.Load() != 2 {
		t.Fatalf("retry incorreto: calls=%d response=%v err=%v", calls.Load(), response, err)
	}
}

func TestRetryTransportCancellationStopsBackoff(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	transport := RetryTransport{MaxAttempts: 3, BaseDelay: time.Second, Next: retryRoundTripFunc(func(request *http.Request) (*http.Response, error) {
		return &http.Response{StatusCode: http.StatusInternalServerError, Header: http.Header{}, Body: io.NopCloser(strings.NewReader("")), Request: request}, nil
	})}
	request, _ := http.NewRequestWithContext(ctx, http.MethodGet, "https://example.invalid", nil)
	if _, err := transport.RoundTrip(request); err != context.Canceled {
		t.Fatalf("cancelamento=%v", err)
	}
}
