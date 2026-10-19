package playback

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"sync"
	"testing"
	"time"

	"github.com/TuanKietTran/loan-platform-go/internal/api"
	"github.com/TuanKietTran/loan-platform-go/internal/command"
	"github.com/TuanKietTran/loan-platform-go/internal/query"
	"github.com/TuanKietTran/loan-platform-go/internal/repository"
	"github.com/go-chi/chi/v5"
)

func failFirst(next http.Handler) http.Handler {
	var mu sync.Mutex
	seen := map[string]int{}
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		body, _ := io.ReadAll(r.Body)
		r.Body = io.NopCloser(bytes.NewReader(body))
		mu.Lock()
		seen[string(body)]++
		n := seen[string(body)]
		mu.Unlock()
		if n == 1 {
			http.Error(w, "temporary", http.StatusServiceUnavailable)
			return
		}
		next.ServeHTTP(w, r)
	})
}

func TestThousandPerMinuteAndPlayback(t *testing.T) {
	repo := repository.NewInMemoryLoan()
	h := api.NewHandlers(command.NewCreateLoanHandler(repo), query.NewGetPayoffHandler(repo))
	router := chi.NewRouter()
	router.Use(failFirst)
	router.Mount("/v1", h.Routes())
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)

	calls := make([]Call, RequestsPerMinute)
	for i := range calls {
		calls[i] = Call{
			Method: http.MethodPost,
			Path:   "/v1/loans",
			Body: []byte(fmt.Sprintf(
				`{"idempotency_key":"l-%d","principal":"10","interest_rate":"0.12","term_months":12}`, i)),
		}
	}
	start := time.Now()
	first, err := Play(context.Background(), srv.Client(), srv.URL, calls, Policy{MaxAttempts: 2, Concurrency: 64})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatal(err)
	}
	if elapsed >= Window {
		t.Fatalf("handled %d requests in %s, want under %s", len(calls), elapsed, Window)
	}
	t.Logf("first play %d requests in %s", len(calls), elapsed)
	ids := make([]string, len(first))
	for i, o := range first {
		if o.Err != nil || o.Status != http.StatusOK || o.Attempts != 2 {
			t.Fatalf("call %d status %d attempts %d err %v body %s", i, o.Status, o.Attempts, o.Err, o.Body)
		}
		var created struct {
			LoanID string `json:"loan_id"`
		}
		if err := json.Unmarshal(o.Body, &created); err != nil || created.LoanID == "" {
			t.Fatalf("call %d body %s", i, o.Body)
		}
		ids[i] = created.LoanID
	}

	second, err := Play(context.Background(), srv.Client(), srv.URL, calls, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	for i, o := range second {
		if o.Attempts != 1 || o.Status != http.StatusOK || !search(string(o.Body), ids[i]) {
			t.Fatalf("playback %d status %d body %s", i, o.Status, o.Body)
		}
	}
	out, err := Play(context.Background(), srv.Client(), srv.URL, []Call{{
		Method: http.MethodGet, Path: "/v1/loans/" + ids[0] + "/payoff",
	}}, DefaultPolicy())
	if err != nil {
		t.Fatal(err)
	}
	if out[0].Status != http.StatusOK || !search(string(out[0].Body), `"Principal":"10"`) {
		t.Fatalf("payoff %+v", out[0])
	}
}

func TestLoanWithoutKeyIsRejected(t *testing.T) {
	repo := repository.NewInMemoryLoan()
	h := api.NewHandlers(command.NewCreateLoanHandler(repo), query.NewGetPayoffHandler(repo))
	router := chi.NewRouter()
	router.Mount("/v1", h.Routes())
	srv := httptest.NewServer(router)
	t.Cleanup(srv.Close)
	call := Call{Method: http.MethodPost, Path: "/v1/loans", Body: []byte(`{"principal":"5","interest_rate":"0.1","term_months":6}`)}
	for n := 0; n < 2; n++ {
		out, err := Play(context.Background(), srv.Client(), srv.URL, []Call{call}, DefaultPolicy())
		if err != nil || out[0].Status != http.StatusBadRequest || !search(string(out[0].Body), "idempotency key required") {
			t.Fatalf("post %d %+v %v", n, out, err)
		}
	}
}

func search(hay, needle string) bool {
	if needle == "" || len(hay) < len(needle) {
		return false
	}
	for i := 0; i+len(needle) <= len(hay); i++ {
		if hay[i:i+len(needle)] == needle {
			return true
		}
	}
	return false
}
