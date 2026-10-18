package command

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/TuanKietTran/loan-platform-go/internal/domain"
	"github.com/TuanKietTran/loan-platform-go/internal/repository"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type screenFunc func(context.Context, string, domain.Application) (string, error)

func (f screenFunc) Screen(ctx context.Context, key string, a domain.Application) (string, error) {
	return f(ctx, key, a)
}
func commandApplication() domain.Application {
	return domain.Application{BorrowerID: uuid.New(), FundingAccount: uuid.New(), BorrowerAccount: uuid.New(), Terms: domain.LoanTerms{Principal: decimal.NewFromInt(1200), AnnualRate: decimal.RequireFromString("0.12"), TermMonths: 12, Currency: "USD", MinorUnitScale: 2, Method: domain.StraightLine}, MonthlyIncome: decimal.NewFromInt(2000), StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}
}
func payment(a domain.Agreement, amount string, disburse bool) domain.PaymentEvidence {
	source, dest := a.Application.BorrowerAccount, a.Application.FundingAccount
	if disburse {
		source, dest = dest, source
	}
	return domain.PaymentEvidence{PaymentID: uuid.New(), LedgerID: uuid.New(), Status: "SETTLED", Amount: decimal.RequireFromString(amount), Currency: "USD", Source: source, Destination: dest}
}
func TestAgreementServiceLifecycleReplayAndConflict(t *testing.T) {
	ctx := context.Background()
	calls := 0
	service := NewAgreementService(repository.NewMemoryAgreements(), screenFunc(func(context.Context, string, domain.Application) (string, error) { calls++; return "ALLOW", nil }), uuid.New)
	app := commandApplication()
	a, err := service.ApplyApplication(ctx, "application", app)
	if err != nil {
		t.Fatal(err)
	}
	replay, err := service.ApplyApplication(ctx, "application", app)
	if err != nil || replay.ID != a.ID || calls != 1 {
		t.Fatal("application replay", err)
	}
	different := app
	different.MonthlyDebt = decimal.NewFromInt(1)
	if _, err = service.ApplyApplication(ctx, "application", different); !errors.Is(err, repository.ErrConflict) || calls != 1 {
		t.Fatal("application conflict", err)
	}
	disbursement := payment(a, "1200", true)
	result, err := service.Disburse(ctx, a.ID, "disburse", disbursement)
	if err != nil || result.Agreement.Status != domain.StatusActive {
		t.Fatal(err)
	}
	if _, err = service.Accrue(ctx, a.ID, "disburse", app.StartDate); !errors.Is(err, repository.ErrConflict) {
		t.Fatal("operation key reused", err)
	}
	date := app.StartDate.AddDate(0, 0, 32)
	if _, err = service.Accrue(ctx, a.ID, "accrual", date); err != nil {
		t.Fatal(err)
	}
	result, err = service.Assess(ctx, a.ID, "assessment", date, decimal.NewFromInt(10))
	if err != nil || result.Agreement.Status != domain.StatusDelinquent {
		t.Fatal(err)
	}
	p := payment(a, "100", false)
	result, err = service.Repay(ctx, a.ID, "repay", date, p, true)
	if err != nil || !result.Allocation.Fees.Equal(decimal.NewFromInt(10)) {
		t.Fatal(err)
	}
	result, err = service.Repay(ctx, a.ID, "repay", date, p, true)
	if err != nil || len(result.Agreement.PaymentIDs) != 2 {
		t.Fatal("repayment replay", err)
	}
	// Origin replay remains the original pending result after later servicing.
	replay, err = service.ApplyApplication(ctx, "application", app)
	if err != nil || replay.Status != domain.StatusPending {
		t.Fatal("application snapshot", err)
	}
	result, err = service.Disburse(ctx, a.ID, "disburse", disbursement)
	if err != nil || result.Agreement.Status != domain.StatusActive {
		t.Fatal("disburse snapshot", err)
	}
	if _, err = service.apply(ctx, a.ID, "unknown", servicingPayload{Operation: "bad"}); err == nil {
		t.Fatal("unknown operation")
	}
}
func TestConcurrentApplicationUsesStableFraudIdentity(t *testing.T) {
	ctx := context.Background()
	app := commandApplication()
	var mu sync.Mutex
	events := map[string]bool{}
	ids := map[uuid.UUID]bool{}
	service := NewAgreementService(repository.NewMemoryAgreements(), screenFunc(func(_ context.Context, event string, _ domain.Application) (string, error) {
		mu.Lock()
		events[event] = true
		mu.Unlock()
		return "ALLOW", nil
	}), uuid.New)
	var wg sync.WaitGroup
	for range 30 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			a, err := service.ApplyApplication(ctx, "shared", app)
			if err != nil {
				t.Error(err)
				return
			}
			mu.Lock()
			ids[a.ID] = true
			mu.Unlock()
		}()
	}
	wg.Wait()
	if len(ids) != 1 || len(events) != 1 {
		t.Fatalf("ids %d events %d", len(ids), len(events))
	}
}
func TestAgreementServiceFailsClosedAndValidatesBeforeScoring(t *testing.T) {
	ctx := context.Background()
	app := commandApplication()
	calls := 0
	dependency := errors.New("fraud unavailable")
	service := NewAgreementService(repository.NewMemoryAgreements(), screenFunc(func(context.Context, string, domain.Application) (string, error) { calls++; return "", dependency }), uuid.New)
	for _, key := range []string{"", " ", strings.Repeat("x", 201)} {
		if _, err := service.ApplyApplication(ctx, key, app); !errors.Is(err, repository.ErrInvalidKey) {
			t.Fatal(err)
		}
		if _, err := service.Accrue(ctx, uuid.New(), key, app.StartDate); !errors.Is(err, repository.ErrInvalidKey) {
			t.Fatal(err)
		}
	}
	invalid := app
	invalid.Terms.Currency = "EUR"
	if _, err := service.ApplyApplication(ctx, "invalid", invalid); err == nil || calls != 0 {
		t.Fatal("invalid application scored")
	}
	canceled, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := service.ApplyApplication(canceled, "canceled", app); !errors.Is(err, context.Canceled) || calls != 0 {
		t.Fatal(err)
	}
	if _, err := service.ApplyApplication(ctx, "retry", app); !errors.Is(err, dependency) {
		t.Fatal(err)
	}
	if _, err := service.Store.FindApplication(ctx, "retry", "hash"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatal("unavailable fraud persisted application")
	}
	service.Screen = screenFunc(func(context.Context, string, domain.Application) (string, error) { return "unexpected", nil })
	if _, err := service.ApplyApplication(ctx, "unknown", app); err == nil {
		t.Fatal("unknown fraud approved")
	}
	service.Screen = nil
	if _, err := service.ApplyApplication(ctx, "nil", app); err == nil {
		t.Fatal("missing dependency")
	}
	service.Screen = screenFunc(func(context.Context, string, domain.Application) (string, error) { return "REVIEW", nil })
	service.NewID = nil
	if _, err := service.ApplyApplication(ctx, "nil-id", app); err == nil {
		t.Fatal("missing ID source")
	}
	service.NewID = uuid.New
	a, err := service.ApplyApplication(ctx, "review", app)
	if err != nil || a.Status != domain.StatusReview {
		t.Fatal(err)
	}
	if _, err = service.Disburse(ctx, a.ID, "disburse", payment(a, "1200", true)); err == nil {
		t.Fatal("review disbursed")
	}
	// JSON time encoding errors are returned before the repository is mutated.
	if _, err = service.Accrue(ctx, a.ID, "bad-date", time.Date(10000, 1, 1, 0, 0, 0, 0, time.UTC)); err == nil {
		t.Fatal("bad date encoded")
	}
	if _, err = fingerprint(make(chan int)); err == nil {
		t.Fatal("unsupported fingerprint value")
	}
}
