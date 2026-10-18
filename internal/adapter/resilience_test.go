package adapter

import (
	"context"
	"errors"
	"io"
	"net/http"
	"strings"
	"testing"
	"time"
)

type rt func(*http.Request) (*http.Response, error)

func (f rt) RoundTrip(r *http.Request) (*http.Response, error) { return f(r) }

func status(code int) *http.Response {
	return &http.Response{StatusCode: code, Body: io.NopCloser(strings.NewReader(""))}
}

func newReq(t *testing.T, ctx context.Context, body string) *http.Request {
	t.Helper()
	req, err := http.NewRequestWithContext(ctx, "POST", "http://x/", strings.NewReader(body))
	if err != nil {
		t.Fatal(err)
	}
	return req
}

func TestResilientRetriesWithJitterAndRewindsBody(t *testing.T) {
	var bodies []string
	codes := []int{503, 502, 200}
	var slept []time.Duration
	r := &Resilient{
		Base: rt(func(q *http.Request) (*http.Response, error) {
			b, _ := io.ReadAll(q.Body)
			bodies = append(bodies, string(b))
			c := codes[0]
			codes = codes[1:]
			return status(c), nil
		}),
		Backoff: 100 * time.Millisecond, MaxDelay: 150 * time.Millisecond,
		Sleep:  func(_ context.Context, d time.Duration) error { slept = append(slept, d); return nil },
		Jitter: func(n int64) int64 { return n - 1 },
	}
	resp, err := r.RoundTrip(newReq(t, context.Background(), "payload"))
	if err != nil || resp.StatusCode != 200 {
		t.Fatal(resp, err)
	}
	if len(bodies) != 3 || bodies[0] != "payload" || bodies[1] != "payload" || bodies[2] != "payload" {
		t.Fatal(bodies)
	}
	if len(slept) != 2 || slept[0] != 100*time.Millisecond-1 || slept[1] != 150*time.Millisecond-1 {
		t.Fatal(slept)
	}
}

func TestResilientTransportErrorExhaustsAndOpensCircuit(t *testing.T) {
	boom := errors.New("boom")
	calls := 0
	now := time.Unix(1000, 0)
	r := &Resilient{
		Base:     rt(func(*http.Request) (*http.Response, error) { calls++; return nil, boom }),
		Attempts: 2, Threshold: 2, Cooldown: time.Minute,
		Sleep: func(context.Context, time.Duration) error { return nil },
		Now:   func() time.Time { return now },
	}
	for i := 0; i < 2; i++ {
		if _, err := r.RoundTrip(newReq(t, context.Background(), "x")); !errors.Is(err, boom) {
			t.Fatal(err)
		}
	}
	if calls != 4 {
		t.Fatal(calls)
	}
	if _, err := r.RoundTrip(newReq(t, context.Background(), "x")); !errors.Is(err, ErrCircuitOpen) || calls != 4 {
		t.Fatal("breaker did not short-circuit", err, calls)
	}
	now = now.Add(time.Minute)
	if _, err := r.RoundTrip(newReq(t, context.Background(), "x")); !errors.Is(err, boom) {
		t.Fatal(err)
	}
	if _, err := r.RoundTrip(newReq(t, context.Background(), "x")); !errors.Is(err, ErrCircuitOpen) {
		t.Fatal(err)
	}
	now = now.Add(time.Minute)
	r.Base = rt(func(*http.Request) (*http.Response, error) { return status(200), nil })
	if resp, err := r.RoundTrip(newReq(t, context.Background(), "x")); err != nil || resp.StatusCode != 200 {
		t.Fatal(resp, err)
	}
	if _, err := r.RoundTrip(newReq(t, context.Background(), "x")); err != nil {
		t.Fatal("circuit stayed open", err)
	}
}

func TestResilientHalfOpenAllowsSingleProbe(t *testing.T) {
	now := time.Unix(1000, 0)
	r := &Resilient{Attempts: 1, Threshold: 1, Cooldown: time.Second, Now: func() time.Time { return now }}
	r.Base = rt(func(*http.Request) (*http.Response, error) { return status(503), nil })
	if resp, _ := r.RoundTrip(newReq(t, context.Background(), "x")); resp.StatusCode != 503 {
		t.Fatal(resp)
	}
	now = now.Add(time.Second)
	var nested error
	r.Base = rt(func(*http.Request) (*http.Response, error) {
		_, nested = r.RoundTrip(newReq(t, context.Background(), "x"))
		return status(200), nil
	})
	if _, err := r.RoundTrip(newReq(t, context.Background(), "x")); err != nil {
		t.Fatal(err)
	}
	if !errors.Is(nested, ErrCircuitOpen) {
		t.Fatal("concurrent probe admitted", nested)
	}
}

func TestResilientDoesNotRetryClientErrorsOrCancellation(t *testing.T) {
	calls := 0
	r := &Resilient{
		Base:      rt(func(*http.Request) (*http.Response, error) { calls++; return status(409), nil }),
		Threshold: 1,
	}
	for i := 0; i < 3; i++ {
		if resp, err := r.RoundTrip(newReq(t, context.Background(), "x")); err != nil || resp.StatusCode != 409 {
			t.Fatal(resp, err)
		}
	}
	if calls != 3 {
		t.Fatal(calls)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	r = &Resilient{Base: rt(func(*http.Request) (*http.Response, error) { return status(503), nil }), Threshold: 1}
	if _, err := r.RoundTrip(newReq(t, ctx, "x")); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if err := r.admit(); err != nil {
		t.Fatal("cancellation scored as downstream failure", err)
	}
}

func TestResilientSleepCancelUnrewindableBodyAndDefaults(t *testing.T) {
	calls := 0
	stop := errors.New("stop")
	r := &Resilient{
		Base:  rt(func(*http.Request) (*http.Response, error) { calls++; return status(503), nil }),
		Sleep: func(context.Context, time.Duration) error { return stop },
	}
	if _, err := r.RoundTrip(newReq(t, context.Background(), "x")); !errors.Is(err, stop) || calls != 1 {
		t.Fatal(err, calls)
	}

	calls = 0
	r = &Resilient{Base: rt(func(*http.Request) (*http.Response, error) { calls++; return nil, errors.New("down") })}
	req := newReq(t, context.Background(), "x")
	req.GetBody = nil
	if _, err := r.RoundTrip(req); err == nil || calls != 1 {
		t.Fatal(err, calls)
	}

	req = newReq(t, context.Background(), "x")
	bad := errors.New("rewind")
	req.GetBody = func() (io.ReadCloser, error) { return nil, bad }
	r.Backoff, r.MaxDelay = time.Nanosecond, time.Nanosecond
	if _, err := r.RoundTrip(req); !errors.Is(err, bad) {
		t.Fatal(err)
	}

	r = &Resilient{Base: rt(func(*http.Request) (*http.Response, error) { return status(504), nil }), Backoff: time.Millisecond, MaxDelay: time.Millisecond}
	if resp, err := r.RoundTrip(newReq(t, context.Background(), "x")); err != nil || resp.StatusCode != 504 {
		t.Fatal(resp, err)
	}
	if r.jitter(0) != 0 {
		t.Fatal("jitter(0)")
	}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if err := r.sleep(ctx, time.Hour); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if (&Resilient{}).now().IsZero() {
		t.Fatal("now")
	}
}
