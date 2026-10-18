package adapter

import (
	"context"
	"errors"
	"math/rand"
	"net/http"
	"sync"
	"time"
)

// ErrCircuitOpen is returned without contacting the downstream service while
// the breaker is open.
var ErrCircuitOpen = errors.New("downstream circuit open")

// Resilient is an http.RoundTripper that retries transport errors and
// 502/503/504 responses with capped exponential backoff and jitter, and opens a
// circuit after consecutive failed calls. Every downstream write used by the
// adapters is keyed or replay-safe, so retrying an ambiguous failure is safe;
// requests whose body cannot be rewound are attempted once.
type Resilient struct {
	Base      http.RoundTripper // default http.DefaultTransport
	Attempts  int               // total tries per call, default 3
	Backoff   time.Duration     // first delay, default 50ms
	MaxDelay  time.Duration     // delay cap, default 1s
	Threshold int               // consecutive failed calls before opening, default 5
	Cooldown  time.Duration     // open duration before one probe, default 5s

	Sleep  func(context.Context, time.Duration) error // test seam
	Jitter func(n int64) int64                        // returns [0,n), test seam
	Now    func() time.Time                           // test seam

	mu        sync.Mutex
	failures  int
	openUntil time.Time
	probing   bool
}

func (r *Resilient) now() time.Time {
	if r.Now != nil {
		return r.Now()
	}
	return time.Now()
}

func orDefault[T int | time.Duration](v, d T) T {
	if v <= 0 {
		return d
	}
	return v
}

func (r *Resilient) sleep(ctx context.Context, d time.Duration) error {
	if r.Sleep != nil {
		return r.Sleep(ctx, d)
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}

func (r *Resilient) jitter(n int64) int64 {
	if n <= 0 {
		return 0
	}
	if r.Jitter != nil {
		return r.Jitter(n)
	}
	return rand.Int63n(n)
}

func (r *Resilient) admit() error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if r.failures < orDefault(r.Threshold, 5) {
		return nil
	}
	if r.now().Before(r.openUntil) || r.probing {
		return ErrCircuitOpen
	}
	r.probing = true
	return nil
}

func (r *Resilient) record(ok bool) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.probing = false
	if ok {
		r.failures = 0
		return
	}
	r.failures++
	if r.failures >= orDefault(r.Threshold, 5) {
		r.openUntil = r.now().Add(orDefault(r.Cooldown, 5*time.Second))
	}
}

// release frees a half-open probe slot without scoring the call.
func (r *Resilient) release() {
	r.mu.Lock()
	r.probing = false
	r.mu.Unlock()
}

func retryable(resp *http.Response, err error) bool {
	if err != nil {
		return true
	}
	switch resp.StatusCode {
	case http.StatusBadGateway, http.StatusServiceUnavailable, http.StatusGatewayTimeout:
		return true
	}
	return false
}

func (r *Resilient) RoundTrip(req *http.Request) (*http.Response, error) {
	if err := r.admit(); err != nil {
		return nil, err
	}
	base := r.Base
	if base == nil {
		base = http.DefaultTransport
	}
	attempts := orDefault(r.Attempts, 3)
	if req.Body != nil && req.Body != http.NoBody && req.GetBody == nil {
		attempts = 1
	}
	delay := orDefault(r.Backoff, 50*time.Millisecond)
	limit := orDefault(r.MaxDelay, time.Second)
	ctx := req.Context()
	for i := 0; ; i++ {
		attempt := req.Clone(ctx)
		if i > 0 && req.GetBody != nil {
			body, err := req.GetBody()
			if err != nil {
				r.record(false)
				return nil, err
			}
			attempt.Body = body
		}
		resp, err := base.RoundTrip(attempt)
		if ctx.Err() != nil {
			// Caller cancellation says nothing about downstream health.
			r.release()
			if resp != nil {
				resp.Body.Close()
			}
			return nil, ctx.Err()
		}
		if !retryable(resp, err) {
			r.record(true)
			return resp, nil
		}
		if i+1 >= attempts {
			r.record(false)
			return resp, err
		}
		if resp != nil {
			resp.Body.Close()
		}
		d := delay << i
		if d > limit || d <= 0 {
			d = limit
		}
		if err := r.sleep(ctx, d/2+time.Duration(r.jitter(int64(d/2)))); err != nil {
			r.release()
			return nil, err
		}
	}
}
