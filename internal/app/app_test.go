package app

import (
	"context"
	"encoding/json"
	"fmt"
	"net"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/TuanKietTran/loan-platform-go/internal/api"
	"github.com/TuanKietTran/loan-platform-go/internal/domain"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type rig struct {
	t                   *testing.T
	app                 *App
	fraudDown, payDown  atomic.Bool
	decision            atomic.Value
	mu                  sync.Mutex
	byKey               map[string]uuid.UUID
	recs                map[uuid.UUID]map[string]any
	borrower, fund, acc uuid.UUID
}

func newRig(t *testing.T) *rig {
	r := &rig{t: t, byKey: map[string]uuid.UUID{}, recs: map[uuid.UUID]map[string]any{}, borrower: uuid.New(), fund: uuid.New(), acc: uuid.New()}
	r.decision.Store("ALLOW")
	fraud := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, q *http.Request) {
		if r.fraudDown.Load() {
			w.WriteHeader(503)
			return
		}
		var in struct {
			EventID string `json:"event_id"`
		}
		_ = json.NewDecoder(q.Body).Decode(&in)
		fmt.Fprintf(w, `{"event_id":%q,"decision":%q}`, in.EventID, r.decision.Load())
	}))
	t.Cleanup(fraud.Close)
	mux := http.NewServeMux()
	mux.HandleFunc("POST /v1/payments", r.create)
	mux.HandleFunc("POST /v1/payments/{id}/process", r.process)
	mux.HandleFunc("GET /v1/payments/{id}", r.get)
	pay := httptest.NewServer(mux)
	t.Cleanup(pay.Close)
	r.app = New(Config{FraudURL: fraud.URL, PaymentsURL: pay.URL, RecoverEvery: 10 * time.Millisecond})
	return r
}

func (r *rig) create(w http.ResponseWriter, q *http.Request) {
	if r.payDown.Load() {
		w.WriteHeader(502)
		return
	}
	var in map[string]string
	_ = json.NewDecoder(q.Body).Decode(&in)
	r.mu.Lock()
	defer r.mu.Unlock()
	id, ok := r.byKey[in["idempotency_key"]]
	if !ok {
		id = uuid.New()
		r.byKey[in["idempotency_key"]] = id
		r.recs[id] = map[string]any{"ID": id.String(), "SourceAccount": in["source_account"], "DestAccount": in["dest_account"], "Amount": in["amount"], "Currency": in["currency"], "Status": "PENDING", "LedgerTxID": nil}
	}
	_ = json.NewEncoder(w).Encode(map[string]string{"payment_id": id.String()})
}

func (r *rig) process(w http.ResponseWriter, q *http.Request) {
	id, _ := uuid.Parse(q.PathValue("id"))
	r.mu.Lock()
	defer r.mu.Unlock()
	if rec, ok := r.recs[id]; ok && rec["Status"] == "PENDING" {
		rec["Status"], rec["LedgerTxID"] = "SETTLED", uuid.NewString()
	}
	fmt.Fprint(w, `{}`)
}

func (r *rig) get(w http.ResponseWriter, q *http.Request) {
	id, _ := uuid.Parse(q.PathValue("id"))
	r.mu.Lock()
	defer r.mu.Unlock()
	rec, ok := r.recs[id]
	if !ok {
		w.WriteHeader(404)
		return
	}
	_ = json.NewEncoder(w).Encode(rec)
}

func (r *rig) payments() int {
	r.mu.Lock()
	defer r.mu.Unlock()
	return len(r.byKey)
}

func (r *rig) do(method, path, key, body string) (int, []byte) {
	req := httptest.NewRequest(method, path, strings.NewReader(body))
	if key != "" {
		req.Header.Set("Idempotency-Key", key)
	}
	rec := httptest.NewRecorder()
	r.app.Handler.ServeHTTP(rec, req)
	return rec.Code, rec.Body.Bytes()
}

func (r *rig) ok(method, path, key, body string) []byte {
	r.t.Helper()
	code, b := r.do(method, path, key, body)
	if code != 200 {
		r.t.Fatalf("%s %s = %d %s", method, path, code, b)
	}
	return b
}

func (r *rig) status(method, path, key, body string, want int) {
	r.t.Helper()
	if code, b := r.do(method, path, key, body); code != want {
		r.t.Fatalf("%s %s = %d want %d: %s", method, path, code, want, b)
	}
}

func (r *rig) body(debt string) string {
	return fmt.Sprintf(`{"borrower_id":%q,"funding_account":%q,"borrower_account":%q,"principal":"1200.00","annual_rate":"0.12","term_months":12,"monthly_income":"2000.00","monthly_debt":%q,"start_date":"2024-01-01"}`, r.borrower, r.fund, r.acc, debt)
}

func parse[T any](t *testing.T, b []byte) T {
	t.Helper()
	var v T
	if err := json.Unmarshal(b, &v); err != nil {
		t.Fatal(err, string(b))
	}
	return v
}

func TestLifecycleEndToEnd(t *testing.T) {
	r := newRig(t)
	r.status("POST", "/v1/applications", "", r.body("0"), 400)
	r.status("POST", "/v1/applications", "a1", `{"nope":1}`, 400)
	r.status("POST", "/v1/applications", "a1", strings.Replace(r.body("0"), "2024-01-01", "01/01/2024", 1), 400)
	r.status("POST", "/v1/applications", "a1", strings.Replace(r.body("0"), `"1200.00"`, `"-5"`, 1), 422)

	loan := parse[api.AgreementView](t, r.ok("POST", "/v1/applications", "a1", r.body("0")))
	if loan.Status != string(domain.StatusPending) || len(loan.Schedule) != 12 || loan.Currency != "USD" || loan.OutstandingPrincipal != "0.00" {
		t.Fatalf("%+v", loan)
	}
	if again := parse[api.AgreementView](t, r.ok("POST", "/v1/applications", "a1", r.body("0"))); again.ID != loan.ID {
		t.Fatal("application replay changed identity")
	}
	r.status("POST", "/v1/applications", "a1", r.body("1"), 409)
	p := "/v1/loans/" + loan.ID

	r.status("GET", "/v1/loans/not-a-uuid", "", "", 400)
	r.status("GET", "/v1/loans/"+uuid.NewString(), "", "", 404)
	r.status("POST", p+"/disburse", "", "", 400)
	disbursed := parse[api.AgreementView](t, r.ok("POST", p+"/disburse", "d1", ""))
	if disbursed.Status != string(domain.StatusActive) || disbursed.OutstandingPrincipal != "1200.00" || len(disbursed.PaymentIDs) != 1 {
		t.Fatalf("%+v", disbursed)
	}
	r.ok("POST", p+"/disburse", "d1", "")
	r.status("POST", p+"/disburse", "d2", "", 422)
	if n := r.payments(); n != 1 {
		t.Fatalf("payments created = %d, want 1", n)
	}

	r.ok("POST", p+"/accruals", "acc1", `{"date":"2024-02-01"}`)
	r.status("POST", p+"/accruals", "acc2", `{"date":"2023-01-01"}`, 422)
	fee := parse[api.AgreementView](t, r.ok("POST", p+"/fees", "fee1", `{"date":"2024-02-15","fee":"10.00"}`))
	if fee.Status != string(domain.StatusDelinquent) || fee.Fees != "10.00" {
		t.Fatalf("%+v", fee)
	}
	r.status("POST", p+"/fees", "fee2", `{"date":"2024-02-15","fee":"abc"}`, 400)

	r.status("GET", p+"/payoff-quote", "", "", 400)
	r.status("GET", "/v1/loans/"+uuid.NewString()+"/payoff-quote?date=2024-02-15", "", "", 404)
	q := parse[api.QuoteView](t, r.ok("GET", p+"/payoff-quote?date=2024-02-15", "", ""))
	sum := decimal.RequireFromString(q.Principal).Add(decimal.RequireFromString(q.Interest)).Add(decimal.RequireFromString(q.Fees))
	if !sum.Equal(decimal.RequireFromString(q.Total)) || q.Fees != "10.00" || q.Principal != "1200.00" || q.Currency != "USD" {
		t.Fatalf("%+v", q)
	}

	r.status("POST", p+"/repayments", "r0", `{"date":"2024-02-15","amount":"5000.00","prepayment":true}`, 422)
	paid := parse[api.RepaymentView](t, r.ok("POST", p+"/repayments", "r1", fmt.Sprintf(`{"date":"2024-02-15","amount":%q,"prepayment":true}`, q.Total)))
	if paid.Agreement.Status != string(domain.StatusPaidOff) || paid.Allocation.Fees != "10.00" || paid.Agreement.OutstandingPrincipal != "0.00" || len(paid.Agreement.PaymentIDs) != 2 {
		t.Fatalf("%+v", paid)
	}
	r.ok("POST", p+"/repayments", "r1", fmt.Sprintf(`{"date":"2024-02-15","amount":%q,"prepayment":true}`, q.Total))
	if n := r.payments(); n != 2 {
		t.Fatalf("payments created = %d, want 2", n)
	}
	r.status("GET", p+"/payoff-quote?date=2024-03-01", "", "", 422)

	r.status("POST", "/v1/loans", "legacy", `{"principal":"1000","interest_rate":"0.05","term_months":12}`, 200)
}

func TestDeclinedApplicationCannotMoveMoney(t *testing.T) {
	r := newRig(t)
	r.decision.Store("DENY")
	loan := parse[api.AgreementView](t, r.ok("POST", "/v1/applications", "a1", r.body("0")))
	if loan.Status != string(domain.StatusDeclined) {
		t.Fatal(loan.Status)
	}
	r.status("POST", "/v1/loans/"+loan.ID+"/disburse", "d1", "", 422)
	if r.payments() != 0 {
		t.Fatal("payment created for declined loan")
	}
}

func TestDownstreamFailuresAndReadiness(t *testing.T) {
	r := newRig(t)
	r.status("GET", "/healthz", "", "", 200)
	r.status("GET", "/readyz", "", "", 200)
	r.fraudDown.Store(true)
	r.status("GET", "/readyz", "", "", 503)
	r.status("POST", "/v1/applications", "a1", r.body("0"), 502)
	r.fraudDown.Store(false)
	r.status("POST", "/v1/applications", "a1", r.body("0"), 200)

	bad := New(Config{FraudURL: "http://127.0.0.1:1", PaymentsURL: "http://127.0.0.1:1"})
	if err := bad.Ready(context.Background()); err == nil || !strings.Contains(err.Error(), "payments") || !strings.Contains(err.Error(), "fraud") {
		t.Fatal(err)
	}
	if probe(context.Background(), http.DefaultClient, "://bad") == nil {
		t.Fatal("bad probe URL")
	}
}

func TestRecoverLoopCompletesInterruptedDisbursement(t *testing.T) {
	r := newRig(t)
	loan := parse[api.AgreementView](t, r.ok("POST", "/v1/applications", "a1", r.body("0")))
	p := "/v1/loans/" + loan.ID
	r.payDown.Store(true)
	r.status("POST", p+"/disburse", "d1", "", 502)
	if got := parse[api.AgreementView](t, r.ok("GET", p, "", "")); got.Status != string(domain.StatusPending) {
		t.Fatal("state advanced without evidence", got.Status)
	}
	r.payDown.Store(false)

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan struct{})
	go func() { RecoverLoop(ctx, r.app.Cash, 10*time.Millisecond, t.Logf); close(done) }()
	deadline := time.Now().Add(5 * time.Second)
	for {
		got := parse[api.AgreementView](t, r.ok("GET", p, "", ""))
		if got.Status == string(domain.StatusActive) {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("recovery did not complete", got.Status)
		}
		time.Sleep(20 * time.Millisecond)
	}
	cancel()
	<-done
	if n := r.payments(); n != 1 {
		t.Fatalf("payments created = %d, want 1", n)
	}
}

func TestLoadConfig(t *testing.T) {
	env := map[string]string{"FRAUD_URL": "http://f:1", "PAYMENTS_URL": "https://p"}
	get := func(k string) string { return env[k] }
	c, err := LoadConfig(get)
	if err != nil || c.Addr != ":8084" || c.RecoverEvery != 30*time.Second {
		t.Fatal(c, err)
	}
	env["ADDR"], env["RECOVER_INTERVAL"] = ":9", "5s"
	if c, err = LoadConfig(get); err != nil || c.Addr != ":9" || c.RecoverEvery != 5*time.Second {
		t.Fatal(c, err)
	}
	for name, mutate := range map[string]func(){
		"missing fraud":     func() { env["FRAUD_URL"] = "" },
		"scheme":            func() { env["PAYMENTS_URL"] = "ftp://p" },
		"relative":          func() { env["PAYMENTS_URL"] = "p:80/x" },
		"interval garbage":  func() { env["RECOVER_INTERVAL"] = "soon" },
		"interval negative": func() { env["RECOVER_INTERVAL"] = "-1s" },
	} {
		env = map[string]string{"FRAUD_URL": "http://f:1", "PAYMENTS_URL": "https://p"}
		mutate()
		if _, err := LoadConfig(get); err == nil {
			t.Fatal(name, "accepted")
		}
	}
}

func TestServeShutsDownGracefullyAndRunRejectsBadAddr(t *testing.T) {
	r := newRig(t)
	ln, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	errc := make(chan error, 1)
	go func() { errc <- Serve(ctx, ln, r.app) }()
	var res *http.Response
	for i := 0; i < 100; i++ {
		if res, err = http.Get("http://" + ln.Addr().String() + "/healthz"); err == nil {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if err != nil || res.StatusCode != 200 {
		t.Fatal(err)
	}
	res.Body.Close()
	cancel()
	if err := <-errc; err != nil {
		t.Fatal(err)
	}

	closed, _ := net.Listen("tcp", "127.0.0.1:0")
	closed.Close()
	if err := Serve(context.Background(), closed, r.app); err == nil {
		t.Fatal("serve on closed listener")
	}
	if err := Run(context.Background(), Config{Addr: "256.0.0.1:99999"}); err == nil {
		t.Fatal("bad address accepted")
	}
}

func TestLifecycleRejectsMalformedRequests(t *testing.T) {
	r := newRig(t)
	loan := parse[api.AgreementView](t, r.ok("POST", "/v1/applications", "a1", r.body("0")))
	p := "/v1/loans/" + loan.ID
	missing := "/v1/loans/" + uuid.NewString()
	for _, route := range []string{"/repayments", "/accruals", "/fees"} {
		r.status("POST", "/v1/loans/bad"+route, "k", `{}`, 400)
		r.status("POST", p+route, "k", `{`, 400)
		r.status("POST", p+route, "k", `{"date":"nope"}`, 400)
		r.status("POST", missing+route, "k", `{"date":"2024-02-01","amount":"1.00","fee":"1.00"}`, 400)
	}
	r.status("POST", "/v1/loans/bad/disburse", "k", "", 400)
	r.status("POST", missing+"/disburse", "k", "", 404)
	r.status("POST", p+"/repayments", "", `{"date":"2024-02-01","amount":"1.00","prepayment":false}`, 400)
}

func TestRuntimeDiagnosticsLoopbackAndSchema(t *testing.T) {
	a := New(Config{RecoverEvery: time.Hour})
	s := httptest.NewServer(a.Handler)
	defer s.Close()
	req, _ := http.NewRequest(http.MethodGet, s.URL+"/debug/runtime", nil)
	req.Header.Set("X-Request-ID", "diag-test")
	resp, err := s.Client().Do(req)
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK || resp.Header.Get("X-Request-ID") != "diag-test" {
		t.Fatalf("status=%d id=%q", resp.StatusCode, resp.Header.Get("X-Request-ID"))
	}
	var got runtimeDiagnostics
	if err := json.NewDecoder(resp.Body).Decode(&got); err != nil {
		t.Fatal(err)
	}
	if got.HeapAllocBytes == 0 || got.HeapSysBytes == 0 || got.Goroutines < 1 || got.OpenConnections < 0 {
		t.Fatalf("diagnostics: %+v", got)
	}
}
