package command

import (
	"context"
	"errors"
	"sync"
	"testing"

	"github.com/TuanKietTran/loan-platform-go/internal/domain"
	"github.com/TuanKietTran/loan-platform-go/internal/repository"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// fakePayer is idempotent by key like the payment service. failAfter creates
// the payment but reports a timeout, the ambiguous-commit case.
type fakePayer struct {
	mu        sync.Mutex
	payments  map[string]domain.PaymentEvidence
	calls     int
	failAfter bool
	amount    *decimal.Decimal
}

func newFakePayer() *fakePayer { return &fakePayer{payments: map[string]domain.PaymentEvidence{}} }

func (f *fakePayer) Settle(_ context.Context, key string, source, dest uuid.UUID, amount decimal.Decimal, currency string) (domain.PaymentEvidence, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.calls++
	ev, ok := f.payments[key]
	if !ok {
		if f.amount != nil {
			amount = *f.amount
		}
		ev = domain.PaymentEvidence{PaymentID: uuid.New(), LedgerID: uuid.New(), Status: "SETTLED", Source: source, Destination: dest, Currency: currency, Amount: amount}
		f.payments[key] = ev
	}
	if f.failAfter {
		return domain.PaymentEvidence{}, errors.New("timeout")
	}
	return ev, nil
}

func cashFixture(t *testing.T) (*CashService, *fakePayer, *repository.MemoryIntents, domain.Agreement) {
	t.Helper()
	store := repository.NewMemoryAgreements()
	svc := NewAgreementService(store, screenFunc(func(context.Context, string, domain.Application) (string, error) { return "ALLOW", nil }), uuid.New)
	a, err := svc.ApplyApplication(context.Background(), "app", commandApplication())
	if err != nil {
		t.Fatal(err)
	}
	payer, intents := newFakePayer(), repository.NewMemoryIntents()
	return NewCashService(svc, intents, payer), payer, intents, a
}

func TestCashDisburseReplayMovesMoneyOnce(t *testing.T) {
	ctx := context.Background()
	c, payer, intents, a := cashFixture(t)
	res, err := c.Disburse(ctx, a.ID, "d1")
	if err != nil || res.Agreement.Status != domain.StatusActive {
		t.Fatal(res.Agreement.Status, err)
	}
	replay, err := c.Disburse(ctx, a.ID, "d1")
	if err != nil || len(replay.Agreement.PaymentIDs) != 1 || len(payer.payments) != 1 {
		t.Fatal("duplicate money", err, len(payer.payments))
	}
	if p, _ := intents.Pending(ctx); len(p) != 0 {
		t.Fatal("intent left pending", p)
	}
	if _, err := c.Disburse(ctx, a.ID, "d2"); err == nil || len(payer.payments) != 1 {
		t.Fatal("second disbursement must be rejected before payment", err)
	}
}

func TestCashAmbiguousTimeoutReconcilesWithoutDuplicate(t *testing.T) {
	ctx := context.Background()
	c, payer, intents, a := cashFixture(t)
	payer.failAfter = true
	if _, err := c.Disburse(ctx, a.ID, "d1"); err == nil {
		t.Fatal("timeout hidden")
	}
	if p, _ := intents.Pending(ctx); len(p) != 1 || p[0].PaymentKey == "" {
		t.Fatal("intent not persisted before payment", p)
	}
	if got, _ := c.Agreements.Store.GetAgreement(ctx, a.ID); got.Status != domain.StatusPending {
		t.Fatal("state advanced without evidence", got.Status)
	}
	if n, err := c.Recover(ctx); n != 0 || err == nil {
		t.Fatal(n, err)
	}
	payer.failAfter = false
	if n, err := c.Recover(ctx); n != 1 || err != nil {
		t.Fatal(n, err)
	}
	got, _ := c.Agreements.Store.GetAgreement(ctx, a.ID)
	if got.Status != domain.StatusActive || len(payer.payments) != 1 || len(got.PaymentIDs) != 1 {
		t.Fatal(got.Status, len(payer.payments))
	}
	if n, err := c.Recover(ctx); n != 0 || err != nil {
		t.Fatal(n, err)
	}
}

func TestCashRepayValidationConflictAndMismatch(t *testing.T) {
	ctx := context.Background()
	c, payer, intents, a := cashFixture(t)
	date := a.Application.StartDate.AddDate(0, 0, 32)
	hundred := decimal.NewFromInt(100)
	if _, err := c.Repay(ctx, a.ID, "r1", date, hundred, true); err == nil || payer.calls != 0 {
		t.Fatal("repayment before disbursement reached payments", err)
	}
	if p, _ := intents.Pending(ctx); len(p) != 0 {
		t.Fatal("rejected intent stored")
	}
	if _, err := c.Disburse(ctx, a.ID, "d1"); err != nil {
		t.Fatal(err)
	}
	res, err := c.Repay(ctx, a.ID, "r1", date, hundred, true)
	if err != nil || len(res.Agreement.PaymentIDs) != 2 {
		t.Fatal(err)
	}
	if _, err := c.Repay(ctx, a.ID, "r1", date, hundred, true); err != nil || len(payer.payments) != 2 {
		t.Fatal("repay replay", err, len(payer.payments))
	}
	if _, err := c.Repay(ctx, a.ID, "r1", date, decimal.NewFromInt(50), true); !errors.Is(err, repository.ErrConflict) {
		t.Fatal(err)
	}
	wrong := decimal.NewFromInt(7)
	payer.amount = &wrong
	if _, err := c.Repay(ctx, a.ID, "r2", date, hundred, true); err == nil {
		t.Fatal("mismatched evidence applied")
	}
	if p, _ := intents.Pending(ctx); len(p) != 1 {
		t.Fatal("mismatch must stay pending", p)
	}
}

func TestCashGuards(t *testing.T) {
	ctx := context.Background()
	c, _, _, a := cashFixture(t)
	one := decimal.NewFromInt(1)
	if _, err := c.Disburse(ctx, a.ID, " "); !errors.Is(err, repository.ErrInvalidKey) {
		t.Fatal(err)
	}
	if _, err := c.Repay(ctx, a.ID, "", a.Application.StartDate, one, false); !errors.Is(err, repository.ErrInvalidKey) {
		t.Fatal(err)
	}
	if _, err := c.Disburse(ctx, uuid.New(), "k"); !errors.Is(err, repository.ErrNotFound) {
		t.Fatal(err)
	}
	if _, err := c.Repay(ctx, uuid.New(), "k", a.Application.StartDate, one, false); !errors.Is(err, repository.ErrNotFound) {
		t.Fatal(err)
	}
	cctx, cancel := context.WithCancel(ctx)
	cancel()
	if _, err := c.Disburse(cctx, a.ID, "k"); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	if _, err := c.Recover(cctx); !errors.Is(err, context.Canceled) {
		t.Fatal(err)
	}
	bad := &CashService{}
	if _, err := bad.Disburse(ctx, a.ID, "k"); err == nil {
		t.Fatal("missing deps")
	}
	if _, err := bad.Repay(ctx, a.ID, "k", a.Application.StartDate, one, false); err == nil {
		t.Fatal("missing deps")
	}
	if _, err := bad.Recover(ctx); err == nil {
		t.Fatal("missing deps")
	}
	if _, err := c.run(ctx, repository.CashIntent{AgreementID: a.ID, Key: "x", Operation: "bogus", Source: uuid.New(), Destination: uuid.New(), Amount: one, Currency: "USD"}); err == nil {
		t.Fatal("unsupported operation")
	}
}

func TestCashConcurrentDisburseMovesMoneyOnce(t *testing.T) {
	ctx := context.Background()
	c, payer, _, a := cashFixture(t)
	var wg sync.WaitGroup
	for i := 0; i < 8; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if _, err := c.Disburse(ctx, a.ID, "d1"); err != nil {
				t.Error(err)
			}
		}()
	}
	wg.Wait()
	got, _ := c.Agreements.Store.GetAgreement(ctx, a.ID)
	if len(payer.payments) != 1 || len(got.PaymentIDs) != 1 {
		t.Fatal(len(payer.payments), len(got.PaymentIDs))
	}
}
