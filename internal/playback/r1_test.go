package playback

import (
	"context"
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
	"time"

	"github.com/TuanKietTran/loan-platform-go/internal/api"
	"github.com/TuanKietTran/loan-platform-go/internal/command"
	"github.com/TuanKietTran/loan-platform-go/internal/domain"
	"github.com/TuanKietTran/loan-platform-go/internal/repository"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type r1Allow struct{}

func (r1Allow) Screen(context.Context, string, domain.Application) (string, error) {
	return "ALLOW", nil
}

type r1Pay struct{}

func (r1Pay) Settle(context.Context, string, uuid.UUID, uuid.UUID, decimal.Decimal, string) (domain.PaymentEvidence, error) {
	return domain.PaymentEvidence{}, fmt.Errorf("unexpected payment during application replay")
}

// TestR1TenThousandLifecycleApplications exercises the real lifecycle router over TCP.
func TestR1TenThousandLifecycleApplications(t *testing.T) {
	agreements := repository.NewMemoryAgreements()
	svc := command.NewAgreementService(agreements, r1Allow{}, uuid.New)
	life := &api.Lifecycle{Agreements: svc, Cash: command.NewCashService(svc, repository.NewMemoryIntents(), r1Pay{})}
	router := chi.NewRouter()
	life.Register(router)
	var faults atomic.Uint64
	proxy := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		n := faults.Add(1)
		if n%20 == 0 {
			http.Error(w, "transient", http.StatusServiceUnavailable)
			return
		}
		if n%20 == 1 {
			http.Error(w, "busy", http.StatusTooManyRequests)
			return
		}
		if n%100 == 2 {
			time.Sleep(750 * time.Millisecond)
		}
		router.ServeHTTP(w, r)
	})
	srv := httptest.NewServer(proxy)
	defer srv.Close()
	const total = R1Requests
	calls := make([]Call, total)
	borrower, funding, account := uuid.NewString(), uuid.NewString(), uuid.NewString()
	for i := range calls {
		key := fmt.Sprintf("r1-app-%d", i)
		if i >= total*98/100 {
			key = fmt.Sprintf("r1-app-%d", i-total*98/100)
		}
		body := fmt.Sprintf(`{"borrower_id":%q,"funding_account":%q,"borrower_account":%q,"principal":"1200.00","annual_rate":"0.12","term_months":12,"monthly_income":"2000.00","monthly_debt":"0","start_date":"2024-01-01"}`, borrower, funding, account)
		calls[i] = Call{Method: http.MethodPost, Path: "/applications", Body: []byte(body), Headers: http.Header{"Idempotency-Key": []string{key}}}
	}
	ctx, cancel := context.WithTimeout(context.Background(), Window)
	defer cancel()
	start := time.Now()
	outcomes, err := Play(ctx, srv.Client(), srv.URL, calls, Policy{MaxAttempts: 6, BaseDelay: 50 * time.Millisecond, MaxDelay: 2 * time.Second, AttemptTimeout: 500 * time.Millisecond, Concurrency: 128})
	elapsed := time.Since(start)
	if err != nil {
		t.Fatalf("play: %v", err)
	}
	if elapsed >= Window {
		t.Fatalf("10k lifecycle requests took %s", elapsed)
	}
	ids := make([]string, total)
	for i, o := range outcomes {
		if o.Err != nil || o.Status != http.StatusOK {
			t.Fatalf("call %d terminal: status=%d attempts=%d err=%v body=%s", i, o.Status, o.Attempts, o.Err, o.Body)
		}
		var v api.AgreementView
		if err := json.Unmarshal(o.Body, &v); err != nil {
			t.Fatal(err)
		}
		if v.ID == "" || v.Principal != "1200.00" {
			t.Fatalf("call %d invalid agreement: %+v", i, v)
		}
		ids[i] = v.ID
	}
	for i := total * 98 / 100; i < total; i++ {
		if ids[i] != ids[i-total*98/100] {
			t.Fatalf("duplicate key at %d returned %s; original %s", i, ids[i], ids[i-total*98/100])
		}
	}
	queries := make([]Call, 0, total*98/100)
	for i := 0; i < total*98/100; i++ {
		queries = append(queries, Call{Method: http.MethodGet, Path: "/loans/" + ids[i]})
	}
	queried, e := Play(ctx, srv.Client(), srv.URL, queries, DefaultPolicy())
	if e != nil {
		t.Fatal(e)
	}
	for i, o := range queried {
		if o.Status != http.StatusOK {
			t.Fatalf("query %d status=%d err=%v", i, o.Status, o.Err)
		}
		var v api.AgreementView
		e = json.Unmarshal(o.Body, &v)
		if e != nil {
			t.Fatal(e)
		}
		sum := decimal.Zero
		for _, item := range v.Schedule {
			sum = sum.Add(decimal.RequireFromString(item.Principal))
		}
		if !sum.Equal(decimal.RequireFromString(v.Principal)) {
			t.Fatalf("loan %s schedule principal %s != principal %s", v.ID, sum, v.Principal)
		}
	}
	t.Logf("10,000 application submissions completed in %s; fault attempts=%d; unique agreements=%d", elapsed, faults.Load(), total*98/100)
}
