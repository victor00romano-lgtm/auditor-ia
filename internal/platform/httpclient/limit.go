package httpclient

import "net/http"

// LimitTransport bounds in-flight requests for one dependency. A future queue
// implementation (for example SQS) does not need to know about this policy.
type LimitTransport struct {
	Base  http.RoundTripper
	slots chan struct{}
}

func NewLimitTransport(base http.RoundTripper, limit int) *LimitTransport {
	if base == nil {
		base = http.DefaultTransport
	}
	if limit < 1 {
		limit = 1
	}
	return &LimitTransport{Base: base, slots: make(chan struct{}, limit)}
}

func (t *LimitTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	select {
	case t.slots <- struct{}{}:
		defer func() { <-t.slots }()
	case <-req.Context().Done():
		return nil, req.Context().Err()
	}
	return t.Base.RoundTrip(req)
}
