package httpclient

import (
	"context"
	"io"
	"net/http"
	"strconv"
	"strings"
	"time"
)

type RetryTransport struct {
	Next        http.RoundTripper
	MaxAttempts int
	BaseDelay   time.Duration
}

func (t RetryTransport) RoundTrip(request *http.Request) (*http.Response, error) {
	next := t.Next
	if next == nil {
		next = http.DefaultTransport
	}
	attempts := t.MaxAttempts
	if attempts < 1 {
		attempts = 4
	}
	base := t.BaseDelay
	if base <= 0 {
		base = 500 * time.Millisecond
	}
	for attempt := 1; ; attempt++ {
		current := request
		if attempt > 1 && request.Body != nil {
			if request.GetBody == nil {
				return nil, context.Canceled
			}
			body, err := request.GetBody()
			if err != nil {
				return nil, err
			}
			current = request.Clone(request.Context())
			current.Body = body
		}
		response, err := next.RoundTrip(current)
		if err != nil || response == nil || !retryableStatus(response.StatusCode) || attempt >= attempts {
			return response, err
		}
		_, _ = io.Copy(io.Discard, response.Body)
		_ = response.Body.Close()
		delay := retryAfter(response.Header.Get("Retry-After"), time.Now())
		if delay <= 0 {
			delay = base * time.Duration(1<<(attempt-1))
		}
		timer := time.NewTimer(delay)
		select {
		case <-timer.C:
		case <-request.Context().Done():
			timer.Stop()
			return nil, request.Context().Err()
		}
	}
}

func retryableStatus(status int) bool { return status == http.StatusTooManyRequests || status >= 500 }

func retryAfter(value string, now time.Time) time.Duration {
	value = strings.TrimSpace(value)
	if seconds, err := strconv.Atoi(value); err == nil && seconds >= 0 {
		return time.Duration(seconds) * time.Second
	}
	if at, err := http.ParseTime(value); err == nil && at.After(now) {
		return at.Sub(now)
	}
	return 0
}
