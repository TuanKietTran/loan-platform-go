package command

import (
	"context"
	"errors"
	"time"

	"github.com/TuanKietTran/loan-platform-go/internal/domain"
	"github.com/TuanKietTran/loan-platform-go/internal/repository"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// CashPayer moves money and returns trusted settled evidence. It must be
// idempotent by key (adapter.Payment is), so reconciliation can call it again.
type CashPayer interface {
	Settle(ctx context.Context, key string, source, dest uuid.UUID, amount decimal.Decimal, currency string) (domain.PaymentEvidence, error)
}

// CashService persists a cash intent with a payment idempotency key derived
// from agreement and operation key before any external call. After a timeout,
// crash or ambiguous commit the same key reconciles to the same payment, so
// money is never moved twice. Loans never write the ledger directly.
type CashService struct {
	Agreements *AgreementService
	Intents    repository.IntentRepository
	Payer      CashPayer
}

func NewCashService(a *AgreementService, intents repository.IntentRepository, payer CashPayer) *CashService {
	return &CashService{Agreements: a, Intents: intents, Payer: payer}
}

func paymentKey(id uuid.UUID, key string) string {
	return "loan-cash-" + uuid.NewSHA1(uuid.NameSpaceURL, []byte("loans:cash:"+id.String()+":"+key)).String()
}

// probe is a throwaway evidence used only to dry-run domain validation before
// an intent is stored; it is never applied.
func probe(in repository.CashIntent) domain.PaymentEvidence {
	return domain.PaymentEvidence{PaymentID: uuid.New(), LedgerID: uuid.New(), Status: "SETTLED", Source: in.Source, Destination: in.Destination, Currency: in.Currency, Amount: in.Amount}
}

func (c *CashService) ready() error {
	if c.Agreements == nil || c.Intents == nil || c.Payer == nil {
		return errors.New("cash dependencies required")
	}
	return nil
}

// Disburse pays the principal from the funding to the borrower account.
func (c *CashService) Disburse(ctx context.Context, id uuid.UUID, key string) (repository.AgreementResult, error) {
	if err := c.ready(); err != nil {
		return repository.AgreementResult{}, err
	}
	if err := commandKey(key); err != nil {
		return repository.AgreementResult{}, err
	}
	a, err := c.Agreements.Store.GetAgreement(ctx, id)
	if err != nil {
		return repository.AgreementResult{}, err
	}
	in := repository.CashIntent{AgreementID: id, Key: key, Operation: "disburse", Source: a.Application.FundingAccount, Destination: a.Application.BorrowerAccount, Amount: a.Application.Terms.Principal, Currency: a.Application.Terms.Currency}
	return c.start(ctx, in, func() error { next := a.Clone(); return next.Disburse(probe(in)) })
}

// Repay collects amount from the borrower account to the funding account.
func (c *CashService) Repay(ctx context.Context, id uuid.UUID, key string, date time.Time, amount decimal.Decimal, prepayment bool) (repository.AgreementResult, error) {
	if err := c.ready(); err != nil {
		return repository.AgreementResult{}, err
	}
	if err := commandKey(key); err != nil {
		return repository.AgreementResult{}, err
	}
	a, err := c.Agreements.Store.GetAgreement(ctx, id)
	if err != nil {
		return repository.AgreementResult{}, err
	}
	in := repository.CashIntent{AgreementID: id, Key: key, Operation: "repay", Date: date, Prepayment: prepayment, Source: a.Application.BorrowerAccount, Destination: a.Application.FundingAccount, Amount: amount, Currency: a.Application.Terms.Currency}
	return c.start(ctx, in, func() error { next := a.Clone(); _, err := next.Repay(date, probe(in), prepayment); return err })
}

func (c *CashService) start(ctx context.Context, in repository.CashIntent, validate func() error) (repository.AgreementResult, error) {
	in.PaymentKey = paymentKey(in.AgreementID, in.Key)
	hash, err := fingerprint(in)
	if err != nil {
		return repository.AgreementResult{}, err
	}
	in.Fingerprint = hash
	stored, err := c.Intents.Begin(ctx, in, validate)
	if err != nil {
		return repository.AgreementResult{}, err
	}
	return c.run(ctx, stored)
}

// run reconciles one stored intent: settle (idempotently), apply trusted
// evidence (idempotently), then mark the intent done.
func (c *CashService) run(ctx context.Context, in repository.CashIntent) (repository.AgreementResult, error) {
	ev, err := c.Payer.Settle(ctx, in.PaymentKey, in.Source, in.Destination, in.Amount, in.Currency)
	if err != nil {
		return repository.AgreementResult{}, err
	}
	if ev.Source != in.Source || ev.Destination != in.Destination || ev.Currency != in.Currency || !ev.Amount.Equal(in.Amount) {
		return repository.AgreementResult{}, errors.New("payment evidence does not match cash intent")
	}
	var res repository.AgreementResult
	switch in.Operation {
	case "disburse":
		res, err = c.Agreements.Disburse(ctx, in.AgreementID, in.Key, ev)
	case "repay":
		res, err = c.Agreements.Repay(ctx, in.AgreementID, in.Key, in.Date, ev, in.Prepayment)
	default:
		err = errors.New("unsupported cash operation")
	}
	if err != nil {
		return repository.AgreementResult{}, err
	}
	if err := c.Intents.Complete(ctx, in.AgreementID, in.Key); err != nil {
		return repository.AgreementResult{}, err
	}
	return res, nil
}

// Recover re-runs every unfinished intent and returns how many completed.
func (c *CashService) Recover(ctx context.Context) (int, error) {
	if err := c.ready(); err != nil {
		return 0, err
	}
	pending, err := c.Intents.Pending(ctx)
	if err != nil {
		return 0, err
	}
	done := 0
	var errs []error
	for _, in := range pending {
		if _, err := c.run(ctx, in); err != nil {
			errs = append(errs, err)
			continue
		}
		done++
	}
	return done, errors.Join(errs...)
}
