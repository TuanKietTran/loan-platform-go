package playback

import (
	"bytes"
	"context"
	"errors"
	"io"
	"math/rand/v2"
	"net/http"
	"sync"
	"time"
)

const (
	RequestsPerMinute = 1000
	R1Requests        = 10000
	Window            = time.Minute
)

type Call struct {
	Method, Path string
	Body         []byte
	Headers      http.Header
}
type Outcome struct {
	Status   int
	Body     []byte
	Attempts int
	Err      error
}
type Policy struct {
	MaxAttempts    int
	BaseDelay      time.Duration
	MaxDelay       time.Duration
	AttemptTimeout time.Duration
	Concurrency    int
}

func DefaultPolicy() Policy {
	return Policy{MaxAttempts: 6, BaseDelay: 50 * time.Millisecond, MaxDelay: 2 * time.Second, AttemptTimeout: 2 * time.Second, Concurrency: 64}
}
func Retryable(s int) bool {
	return s == http.StatusRequestTimeout || s == http.StatusTooManyRequests || s >= 500
}
func IsIdempotentReplay(status int, body []byte) bool {
	return status == http.StatusBadRequest && bytes.Contains(body, []byte("idempotency key already used"))
}

// Play submits a bounded number of calls concurrently and preserves input order in outcomes.
func Play(ctx context.Context, client *http.Client, baseURL string, calls []Call, p Policy) ([]Outcome, error) {
	if client == nil {
		client = http.DefaultClient
	}
	if p.MaxAttempts < 1 {
		p.MaxAttempts = 1
	}
	workers := p.Concurrency
	if workers < 1 {
		workers = 1
	}
	if workers > len(calls) {
		workers = len(calls)
	}
	out := make([]Outcome, len(calls))
	jobs := make(chan int)
	var wg sync.WaitGroup
	for n := 0; n < workers; n++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := range jobs {
				out[i], _ = playOne(ctx, client, baseURL, calls[i], p)
			}
		}()
	}
	for i := range calls {
		select {
		case <-ctx.Done():
			close(jobs)
			wg.Wait()
			return out, ctx.Err()
		case jobs <- i:
		}
	}
	close(jobs)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return out, err
	}
	return out, nil
}
func playOne(ctx context.Context, client *http.Client, baseURL string, call Call, p Policy) (Outcome, error) {
	var last Outcome
	for attempt := 1; attempt <= p.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return last, err
		}
		if attempt > 1 && p.BaseDelay > 0 {
			capDelay := p.BaseDelay << (attempt - 2)
			if p.MaxDelay > 0 && capDelay > p.MaxDelay {
				capDelay = p.MaxDelay
			}
			delay := time.Duration(rand.Int64N(int64(capDelay) + 1))
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return last, ctx.Err()
			case <-timer.C:
			}
		}
		attemptCtx := ctx
		cancel := func() {}
		if p.AttemptTimeout > 0 {
			attemptCtx, cancel = context.WithTimeout(ctx, p.AttemptTimeout)
		}
		req, err := http.NewRequestWithContext(attemptCtx, call.Method, baseURL+call.Path, bytes.NewReader(call.Body))
		if err != nil {
			cancel()
			return last, err
		}
		if len(call.Body) > 0 {
			req.Header.Set("Content-Type", "application/json")
		}
		req.Header = mergeHeaders(req.Header, call.Headers)
		resp, err := client.Do(req)
		last.Attempts = attempt
		if err != nil {
			cancel()
			last.Err = err
			last.Status = 0
			if attempt == p.MaxAttempts || !retryableErr(err) {
				return last, nil
			}
			continue
		}
		body, readErr := io.ReadAll(io.LimitReader(resp.Body, 1<<20+1))
		_ = resp.Body.Close()
		cancel()
		if len(body) > 1<<20 {
			last.Status = resp.StatusCode
			last.Err = errors.New("response body exceeds 1 MiB")
			return last, nil
		}
		if readErr != nil {
			last.Err = readErr
			last.Status = resp.StatusCode
			if attempt == p.MaxAttempts || !retryableErr(readErr) {
				return last, nil
			}
			continue
		}
		last.Status = resp.StatusCode
		last.Body = body
		last.Err = nil
		if !Retryable(resp.StatusCode) || attempt == p.MaxAttempts {
			return last, nil
		}
	}
	return last, nil
}
func mergeHeaders(dst, src http.Header) http.Header {
	for k, vs := range src {
		for _, v := range vs {
			dst.Add(k, v)
		}
	}
	return dst
}
func retryableErr(err error) bool { return err != nil && !errors.Is(err, context.Canceled) }
