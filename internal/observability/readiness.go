package observability

import (
	"context"
	"sync"
	"time"
)

type CheckFunc func(context.Context) error

type Readiness struct {
	Postgres, Bitrix, Ollama CheckFunc
	Timeout, TTL             time.Duration
	Metrics                  *Metrics
	mu                       sync.Mutex
	checking                 bool
	last                     ReadinessSnapshot
	wait                     chan struct{}
}

type ReadinessSnapshot struct {
	Status string            `json:"status"`
	Checks map[string]string `json:"checks"`
	at     time.Time
}

func (r *Readiness) Check(ctx context.Context) (string, map[string]string) {
	r.mu.Lock()
	if r.TTL > 0 && time.Since(r.last.at) < r.TTL {
		out := r.last
		r.mu.Unlock()
		return out.Status, out.Checks
	}
	if r.checking {
		wait := r.wait
		r.mu.Unlock()
		select {
		case <-wait:
			r.mu.Lock()
			out := r.last
			r.mu.Unlock()
			return out.Status, out.Checks
		case <-ctx.Done():
			return "not_ready", map[string]string{"postgres": "timeout"}
		}
	}
	r.checking, r.wait = true, make(chan struct{})
	r.mu.Unlock()

	timeout := r.Timeout
	if timeout <= 0 {
		timeout = 2 * time.Second
	}
	checkCtx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	type result struct {
		name string
		err  error
	}
	results := make(chan result, 3)
	for name, check := range map[string]CheckFunc{"postgres": r.Postgres, "bitrix": r.Bitrix, "ollama": r.Ollama} {
		go func(name string, check CheckFunc) {
			if check == nil {
				results <- result{name, context.Canceled}
				return
			}
			results <- result{name, check(checkCtx)}
		}(name, check)
	}
	checks := map[string]string{}
	for range 3 {
		item := <-results
		checks[item.name] = map[bool]string{true: "down", false: "up"}[item.err != nil]
		if r.Metrics != nil {
			r.Metrics.DependencyUp.WithLabelValues(item.name).Set(map[bool]float64{true: 0, false: 1}[item.err != nil])
		}
	}
	status := "ready"
	if checks["postgres"] != "up" {
		status = "not_ready"
	} else if checks["bitrix"] != "up" || checks["ollama"] != "up" {
		status = "degraded"
	}
	out := ReadinessSnapshot{Status: status, Checks: checks, at: time.Now()}
	r.mu.Lock()
	r.last, r.checking = out, false
	close(r.wait)
	r.mu.Unlock()
	return out.Status, out.Checks
}
