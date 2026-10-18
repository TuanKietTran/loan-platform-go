package api

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/TuanKietTran/loan-platform-go/internal/adapter"
	"github.com/TuanKietTran/loan-platform-go/internal/command"
	"github.com/TuanKietTran/loan-platform-go/internal/domain"
	"github.com/TuanKietTran/loan-platform-go/internal/repository"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type allowAll struct{}

func (allowAll) Screen(context.Context, string, domain.Application) (string, error) {
	return "ALLOW", nil
}

// fakePayer stands in for the payment adapter: it returns SETTLED evidence
// that mirrors the request unless err is set.
type fakePayer struct {
	err   error
	calls int
	seen  map[string]domain.PaymentEvidence
}

func (p *fakePayer) Settle(_ context.Context, key string, src, dst uuid.UUID, amt decimal.Decimal, cur string) (domain.PaymentEvidence, error) {
	p.calls++
	if p.err != nil {
		return domain.PaymentEvidence{}, p.err
	}
	if ev, ok := p.seen[key]; ok {
		return ev, nil
	}
	ev := domain.PaymentEvidence{PaymentID: uuid.New(), LedgerID: uuid.New(), Status: "SETTLED", Source: src, Destination: dst, Currency: cur, Amount: amt}
	p.seen[key] = ev
	return ev, nil
}

type lrig struct {
	t      *testing.T
	h      http.Handler
	payer  *fakePayer
	borrow uuid.UUID
	fund   uuid.UUID
	acct   uuid.UUID
}

func newLRig(t *testing.T) *lrig {
	ag := command.NewAgreementService(repository.NewMemoryAgreements(), allowAll{}, uuid.New)
	payer := &fakePayer{seen: map[string]domain.PaymentEvidence{}}
	l := &Lifecycle{Agreements: ag, Cash: command.NewCashService(ag, repository.NewMemoryIntents(), payer)}
	r := chi.NewRouter()
	l.Register(r)
	return &lrig{t: t, h: r, payer: payer, borrow: uuid.New(), fund: uuid.New(), acct: uuid.New()}
}

func (g *lrig) do(method, path, key, body string) (int, []byte) {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	rec := httptest.NewRecorder()
	g.h.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

func (g *lrig) want(method, path, key, body string, status int, code string) []byte {
	g.t.Helper()
	got, b := g.do(method, path, key, body)
	if got != status {
		g.t.Fatalf("%s %s = %d want %d: %s", method, path, got, status, b)
	}
	if code != "" {
		var env ErrorEnvelope
		if err := json.Unmarshal(b, &env); err != nil || env.Error.Code != code || env.Error.Message == "" {
			g.t.Fatalf("%s %s error = %s want code %s", method, path, b, code)
		}
	}
	return b
}

func (g *lrig) apply(key string) AgreementView {
	g.t.Helper()
	body := fmt.Sprintf(`{"borrower_id":%q,"funding_account":%q,"borrower_account":%q,"principal":"1200.00","annual_rate":"0.12","term_months":12,"monthly_income":"2000.00","monthly_debt":"0","start_date":"2024-01-01"}`, g.borrow, g.fund, g.acct)
	var v AgreementView
	if err := json.Unmarshal(g.want("POST", "/applications", key, body, 200, ""), &v); err != nil {
		g.t.Fatal(err)
	}
	return v
}

func TestLifecycleHandlersHappyPathShapes(t *testing.T) {
	g := newLRig(t)
	loan := g.apply("a1")
	if loan.Status != "PENDING" || loan.Principal != "1200.00" || loan.AnnualRate != "0.12" || len(loan.Schedule) != 12 || loan.StartDate != "2024-01-01" || loan.LastAccrualDate != "" || loan.Currency != "USD" {
		t.Fatalf("%+v", loan)
	}
	p := "/loans/" + loan.ID

	var active AgreementView
	if err := json.Unmarshal(g.want("POST", p+"/disburse", "d1", "", 200, ""), &active); err != nil {
		t.Fatal(err)
	}
	if active.Status != "ACTIVE" || active.OutstandingPrincipal != "1200.00" || len(active.PaymentIDs) != 1 {
		t.Fatalf("%+v", active)
	}
	// Replay returns the same payment identity.
	var replay AgreementView
	if err := json.Unmarshal(g.want("POST", p+"/disburse", "d1", "", 200, ""), &replay); err != nil {
		t.Fatal(err)
	}
	if len(replay.PaymentIDs) != 1 || replay.PaymentIDs[0] != active.PaymentIDs[0] {
		t.Fatalf("replay changed result: %+v", replay)
	}

	var accrued AgreementView
	if err := json.Unmarshal(g.want("POST", p+"/accruals", "ac1", `{"date":"2024-02-01"}`, 200, ""), &accrued); err != nil {
		t.Fatal(err)
	}
	if accrued.LastAccrualDate != "2024-02-01" || decimal.RequireFromString(accrued.AccruedInterest).Sign() <= 0 {
		t.Fatalf("%+v", accrued)
	}

	var fee AgreementView
	if err := json.Unmarshal(g.want("POST", p+"/fees", "f1", `{"date":"2024-02-15","fee":"10.00"}`, 200, ""), &fee); err != nil {
		t.Fatal(err)
	}
	if fee.Status != "DELINQUENT" || fee.Fees != "10.00" {
		t.Fatalf("%+v", fee)
	}

	var q QuoteView
	if err := json.Unmarshal(g.want("GET", p+"/payoff-quote?date=2024-02-15", "", "", 200, ""), &q); err != nil {
		t.Fatal(err)
	}
	total := decimal.RequireFromString(q.Principal).Add(decimal.RequireFromString(q.Interest)).Add(decimal.RequireFromString(q.Fees))
	if q.LoanID != loan.ID || q.Date != "2024-02-15" || q.Principal != "1200.00" || q.Fees != "10.00" || !total.Equal(decimal.RequireFromString(q.Total)) {
		t.Fatalf("%+v", q)
	}

	calls := g.payer.calls
	var rep RepaymentView
	body := fmt.Sprintf(`{"date":"2024-02-15","amount":%q,"prepayment":true}`, q.Total)
	if err := json.Unmarshal(g.want("POST", p+"/repayments", "r1", body, 200, ""), &rep); err != nil {
		t.Fatal(err)
	}
	if rep.Agreement.Status != "PAID_OFF" || rep.Allocation.Fees != "10.00" || rep.Agreement.OutstandingPrincipal != "0.00" || len(rep.Agreement.PaymentIDs) != 2 || g.payer.calls != calls+1 {
		t.Fatalf("%+v", rep)
	}
}

func TestLifecycleHandlersRejectBadInput(t *testing.T) {
	g := newLRig(t)
	loan := g.apply("a1")
	p := "/loans/" + loan.ID
	missing := "/loans/" + uuid.NewString()

	g.want("POST", "/applications", "", "{}", 400, "invalid_request")
	g.want("POST", "/applications", "a2", `{"unknown":1}`, 400, "invalid_request")
	g.want("POST", "/applications", "a2", `{"start_date":"01/02/2024"}`, 400, "invalid_request")
	g.want("GET", "/loans/not-a-uuid", "", "", 400, "invalid_request")
	g.want("GET", missing, "", "", 404, "not_found")

	bodies := map[string]string{
		"/disburse":   "",
		"/repayments": `{"date":"2024-02-01","amount":"1.00","prepayment":false}`,
		"/accruals":   `{"date":"2024-02-01"}`,
		"/fees":       `{"date":"2024-02-01","fee":"1.00"}`,
	}
	for route, body := range bodies {
		g.want("POST", "/loans/bad"+route, "k", body, 400, "invalid_request")
		g.want("POST", p+route, "", body, 400, "invalid_request")
		g.want("POST", missing+route, "k", body, 404, "not_found")
		if route != "/disburse" {
			g.want("POST", p+route, "k", `{`, 400, "invalid_request")
			g.want("POST", p+route, "k", `{"date":"nope"}`, 400, "invalid_request")
			g.want("POST", p+route, "k", `{"date":"2024-02-01","extra":true}`, 400, "invalid_request")
			g.want("POST", p+route, "k", strings.Repeat(" ", maxBody+1)+body, 400, "invalid_request")
		}
	}
	g.want("POST", p+"/fees", "k", `{"date":"2024-02-01","fee":"abc"}`, 400, "invalid_request")
	g.want("POST", p+"/repayments", "k", `{"date":"2024-02-01","amount":"x","prepayment":false}`, 400, "invalid_request")

	g.want("GET", "/loans/bad/payoff-quote?date=2024-02-01", "", "", 400, "invalid_request")
	g.want("GET", p+"/payoff-quote", "", "", 400, "invalid_request")
	g.want("GET", missing+"/payoff-quote?date=2024-02-01", "", "", 404, "not_found")
	// Pending agreements cannot be quoted, accrued, or repaid.
	g.want("GET", p+"/payoff-quote?date=2024-02-01", "", "", 422, "unprocessable")
	g.want("POST", p+"/accruals", "k2", `{"date":"2024-02-01"}`, 422, "unprocessable")
	g.want("POST", p+"/repayments", "k2", bodies["/repayments"], 422, "unprocessable")
	if g.payer.calls != 0 {
		t.Fatalf("payer called %d times for rejected requests", g.payer.calls)
	}

	// Key reuse with a different payload conflicts.
	g.want("POST", p+"/disburse", "d1", "", 200, "")
	g.want("POST", p+"/accruals", "acc", `{"date":"2024-02-01"}`, 200, "")
	g.want("POST", p+"/accruals", "acc", `{"date":"2024-03-01"}`, 409, "conflict")
	g.want("POST", p+"/fees", "fee", `{"date":"2024-02-15","fee":"5.00"}`, 200, "")
	g.want("POST", p+"/fees", "fee", `{"date":"2024-02-15","fee":"6.00"}`, 409, "conflict")
	g.want("POST", p+"/disburse", "d2", "", 422, "unprocessable")
	g.want("POST", p+"/repayments", "big", `{"date":"2024-02-15","amount":"9999.00","prepayment":false}`, 422, "unprocessable")
	g.want("GET", p+"/payoff-quote?date=2023-01-01", "", "", 422, "unprocessable")
}

func TestLifecycleCashErrorsMapToEnvelope(t *testing.T) {
	g := newLRig(t)
	loan := g.apply("a1")
	p := "/loans/" + loan.ID

	cases := []struct {
		err    error
		status int
		code   string
	}{
		{adapter.ErrCircuitOpen, 503, "unavailable"},
		{context.DeadlineExceeded, 503, "unavailable"},
		{adapter.ErrNotSettled, 502, "downstream_error"},
		{&adapter.StatusError{}, 502, "downstream_error"},
		{&net.DNSError{Err: "no such host", Name: "secret-host"}, 502, "downstream_error"},
		{errors.New("payer exploded"), 422, "unprocessable"},
	}
	for i, c := range cases {
		g.payer.err = c.err
		b := g.want("POST", p+"/disburse", fmt.Sprintf("d%d", i), "", c.status, c.code)
		if c.status == 502 && strings.Contains(string(b), "secret-host") {
			t.Fatalf("downstream detail leaked: %s", b)
		}
	}
	// No evidence, no state change.
	var got AgreementView
	if err := json.Unmarshal(g.want("GET", p, "", "", 200, ""), &got); err != nil {
		t.Fatal(err)
	}
	if got.Status != "PENDING" || len(got.PaymentIDs) != 0 {
		t.Fatalf("state advanced without evidence: %+v", got)
	}
}

func TestWriteErrMapsRepositorySentinels(t *testing.T) {
	for err, want := range map[error]int{
		repository.ErrConflict:   409,
		repository.ErrNotFound:   404,
		repository.ErrInvalidKey: 400,
		context.Canceled:         503,
	} {
		rec := httptest.NewRecorder()
		writeErr(rec, fmt.Errorf("wrapped: %w", err))
		if rec.Code != want {
			t.Fatalf("%v -> %d want %d", err, rec.Code, want)
		}
	}
}
