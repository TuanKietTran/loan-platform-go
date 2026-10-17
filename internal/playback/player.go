package playback

import (
	"bytes"
	"context"
	"errors"
	"io"
	"net/http"
	"time"
)

// RequestsPerMinute is the throughput the playback client must finish inside Window.
const (
	RequestsPerMinute = 1000
	Window            = time.Minute
)

// Call is one recorded HTTP request. Playback sends the same value again.
type Call struct {
	Method string
	Path   string
	Body   []byte
}

// Outcome is the last attempt for a call.
type Outcome struct {
	Status   int
	Body     []byte
	Attempts int
	Err      error
}

// Policy controls retries. Only transport errors and HTTP 408, 429, and 5xx are retried.
type Policy struct {
	MaxAttempts int
	BaseDelay   time.Duration
}

func DefaultPolicy() Policy {
	return Policy{MaxAttempts: 3}
}

func Retryable(status int) bool {
	return status == http.StatusRequestTimeout || status == http.StatusTooManyRequests || status >= 500
}

func IsIdempotentReplay(status int, body []byte) bool {
	return status == http.StatusBadRequest && bytes.Contains(body, []byte("idempotency key already used"))
}

// Play sends calls in order. A later Play of the same slice is a playback.
func Play(ctx context.Context, client *http.Client, baseURL string, calls []Call, policy Policy) ([]Outcome, error) {
	if client == nil {
		client = http.DefaultClient
	}
	if policy.MaxAttempts < 1 {
		policy.MaxAttempts = 1
	}
	out := make([]Outcome, len(calls))
	for i, call := range calls {
		outcome, err := playOne(ctx, client, baseURL, call, policy)
		out[i] = outcome
		if err != nil {
			return out, err
		}
	}
	return out, nil
}

func playOne(ctx context.Context, client *http.Client, baseURL string, call Call, policy Policy) (Outcome, error) {
	var last Outcome
	for attempt := 1; attempt <= policy.MaxAttempts; attempt++ {
		if err := ctx.Err(); err != nil {
			return last, err
		}
		if attempt > 1 && policy.BaseDelay > 0 {
			delay := policy.BaseDelay << (attempt - 2)
			timer := time.NewTimer(delay)
			select {
			case <-ctx.Done():
				timer.Stop()
				return last, ctx.Err()
			case <-timer.C:
			}
		}
		req, err := http.NewRequestWithContext(ctx, call.Method, baseURL+call.Path, bytes.NewReader(call.Body))
		if err != nil {
			return last, err
		}
		if len(call.Body) > 0 {
			req.Header.Set("Content-Type", "application/json")
		}
		resp, err := client.Do(req)
		last.Attempts = attempt
		if err != nil {
			last.Err = err
			last.Status = 0
			if attempt == policy.MaxAttempts || !retryableErr(err) {
				return last, nil
			}
			continue
		}
		body, readErr := io.ReadAll(resp.Body)
		resp.Body.Close()
		if readErr != nil {
			last.Err = readErr
			last.Status = resp.StatusCode
			if attempt == policy.MaxAttempts || !retryableErr(readErr) {
				return last, nil
			}
			continue
		}
		last.Status = resp.StatusCode
		last.Body = body
		last.Err = nil
		if !Retryable(resp.StatusCode) || attempt == policy.MaxAttempts {
			return last, nil
		}
	}
	return last, nil
}

func retryableErr(err error) bool {
	return err != nil && !errors.Is(err, context.Canceled) && !errors.Is(err, context.DeadlineExceeded)
}
