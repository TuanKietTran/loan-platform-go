package adapter

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/TuanKietTran/loan-platform-go/internal/command"
	"github.com/TuanKietTran/loan-platform-go/internal/domain"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

var _ command.ApplicationScreen = (*Fraud)(nil)

func app() domain.Application {
	return domain.Application{BorrowerAccount: uuid.New(), Terms: domain.LoanTerms{Principal: decimal.NewFromInt(1000), Currency: "USD"}}
}

func TestFraudScreen(t *testing.T) {
	var got scoreRequest
	reply := func(event, decision string) *httptest.Server {
		return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
			_ = json.NewDecoder(r.Body).Decode(&got)
			fmt.Fprintf(w, `{"event_id":%q,"decision":%q}`, event, decision)
		}))
	}
	ctx := context.Background()
	srv := reply("e1", "REVIEW")
	defer srv.Close()
	d, err := NewFraud(srv.URL+"/", nil).Screen(ctx, "e1", app())
	if err != nil || d != "REVIEW" || got.Amount != "1000.00" || got.Currency != "USD" || got.EventID != "e1" {
		t.Fatalf("%q %v %+v", d, err, got)
	}
	for name, s := range map[string]*httptest.Server{"mismatch": reply("other", "ALLOW"), "unknown": reply("e1", "MAYBE")} {
		if _, err := NewFraud(s.URL, nil).Screen(ctx, "e1", app()); err == nil {
			t.Fatal(name, "accepted")
		}
		s.Close()
	}
	if _, err := NewFraud(srv.URL, nil).Screen(ctx, "", app()); err == nil {
		t.Fatal("empty event")
	}
	eur := app()
	eur.Terms.Currency = "EUR"
	if _, err := NewFraud(srv.URL, nil).Screen(ctx, "e1", eur); err == nil {
		t.Fatal("non-USD")
	}
	down := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { w.WriteHeader(503) }))
	defer down.Close()
	var se *StatusError
	if _, err := NewFraud(down.URL, nil).Screen(ctx, "e1", app()); !errors.As(err, &se) || se.Code != 503 || se.Error() == "" {
		t.Fatal(err)
	}
	if _, err := NewFraud("http://127.0.0.1:1", nil).Screen(ctx, "e1", app()); err == nil {
		t.Fatal("unreachable")
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := NewFraud(srv.URL, nil).Screen(cctx, "e1", app()); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
}

func TestCallRejectsBadBodies(t *testing.T) {
	big := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		fmt.Fprint(w, strings.Repeat("x", maxBody+10))
	}))
	defer big.Close()
	ctx := context.Background()
	if err := call(ctx, newClient(nil), big.URL, "GET", "/", nil, &struct{}{}); err == nil {
		t.Fatal("oversize accepted")
	}
	if err := call(ctx, newClient(nil), big.URL, "GET", "/", nil, nil); err == nil {
		t.Fatal("oversize accepted without decode")
	}
	if err := call(ctx, newClient(nil), big.URL, "GET", "/", func() {}, nil); err == nil {
		t.Fatal("unmarshalable input")
	}
	if err := call(ctx, newClient(nil), "://bad", "GET", "/", nil, nil); err == nil {
		t.Fatal("bad URL")
	}
	ok := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) { fmt.Fprint(w, `{`) }))
	defer ok.Close()
	if err := call(ctx, newClient(nil), ok.URL, "GET", "/", nil, &struct{}{}); err == nil {
		t.Fatal("bad json accepted")
	}
}

type fakePayments struct {
	status string
	ledger string
	failOn string
	id     uuid.UUID
	src    string
	dst    string
	posted createPayment
}

func (f *fakePayments) server() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if f.failOn == r.Method+" "+r.URL.Path {
			w.WriteHeader(502)
			return
		}
		switch {
		case r.Method == "POST" && r.URL.Path == "/v1/payments":
			_ = json.NewDecoder(r.Body).Decode(&f.posted)
			fmt.Fprintf(w, `{"payment_id":%q}`, f.id)
		case r.Method == "POST":
			fmt.Fprint(w, `{"Status":"SETTLED"}`)
		default:
			fmt.Fprintf(w, `{"ID":%q,"SourceAccount":%q,"DestAccount":%q,"Amount":"250.00","Currency":"USD","Status":%q,"LedgerTxID":%s}`, f.id, f.src, f.dst, f.status, f.ledger)
		}
	}))
}

func TestPaymentSettle(t *testing.T) {
	ctx := context.Background()
	src, dst, id, ledger := uuid.New(), uuid.New(), uuid.New(), uuid.New()
	f := &fakePayments{status: "SETTLED", ledger: fmt.Sprintf("%q", ledger), id: id, src: src.String(), dst: dst.String()}
	srv := f.server()
	defer srv.Close()
	p := NewPayment(srv.URL, nil)
	ev, err := p.Settle(ctx, "k", src, dst, decimal.NewFromInt(250), "USD")
	if err != nil || ev.PaymentID != id || ev.LedgerID != ledger || ev.Source != src || ev.Destination != dst || !ev.Amount.Equal(decimal.NewFromInt(250)) || ev.Status != "SETTLED" || ev.Currency != "USD" {
		t.Fatalf("%+v %v", ev, err)
	}
	if f.posted.IdempotencyKey != "k" || f.posted.Amount != "250.00" || f.posted.SourceAccount != src.String() {
		t.Fatalf("%+v", f.posted)
	}
	for _, bad := range []struct{ name, status, ledger string }{
		{"pending", "PENDING", "null"}, {"screened", "SCREENED", "null"}, {"failed", "FAILED", "null"},
		{"no ledger", "SETTLED", "null"}, {"nil ledger", "SETTLED", `"` + uuid.Nil.String() + `"`}, {"bad ledger", "SETTLED", `"x"`},
	} {
		f.status, f.ledger = bad.status, bad.ledger
		if _, err := p.Settle(ctx, "k", src, dst, decimal.NewFromInt(250), "USD"); !errors.Is(err, ErrNotSettled) {
			t.Fatal(bad.name, err)
		}
	}
	f.status, f.ledger = "SETTLED", fmt.Sprintf("%q", ledger)
	f.src = "acct-1"
	if _, err := p.Evidence(ctx, id); err == nil {
		t.Fatal("non-UUID source")
	}
	f.src, f.dst = src.String(), "acct-2"
	if _, err := p.Evidence(ctx, id); err == nil {
		t.Fatal("non-UUID destination")
	}
	f.dst = dst.String()
	if _, err := p.Evidence(ctx, uuid.New()); !errors.Is(err, ErrNotSettled) {
		t.Fatal("identity mismatch accepted", err)
	}
	for _, fail := range []string{"POST /v1/payments", "POST /v1/payments/" + id.String() + "/process", "GET /v1/payments/" + id.String()} {
		f.failOn = fail
		if _, err := p.Settle(ctx, "k", src, dst, decimal.NewFromInt(250), "USD"); err == nil {
			t.Fatal(fail, "succeeded")
		}
	}
	f.failOn = ""
	for _, bad := range []func() error{
		func() error { _, err := p.Settle(ctx, "", src, dst, decimal.NewFromInt(1), "USD"); return err },
		func() error { _, err := p.Settle(ctx, "k", uuid.Nil, dst, decimal.NewFromInt(1), "USD"); return err },
		func() error { _, err := p.Settle(ctx, "k", src, dst, decimal.Zero, "USD"); return err },
	} {
		if bad() == nil {
			t.Fatal("invalid request accepted")
		}
	}
	f.id = uuid.Nil
	if _, err := p.Settle(ctx, "k", src, dst, decimal.NewFromInt(1), "USD"); err == nil {
		t.Fatal("nil payment ID accepted")
	}
}
