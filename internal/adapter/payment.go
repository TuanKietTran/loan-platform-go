package adapter

import (
	"context"
	"errors"
	"net/http"

	"github.com/TuanKietTran/loan-platform-go/internal/domain"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// ErrNotSettled means the payment did not reach SETTLED with a ledger reference.
var ErrNotSettled = errors.New("payment not settled with ledger reference")

// Payment drives payment-orchestration and converts its record into evidence.
type Payment struct {
	BaseURL string
	HTTP    *http.Client
}

func NewPayment(baseURL string, c *http.Client) *Payment {
	return &Payment{BaseURL: baseURL, HTTP: newClient(c)}
}

type createPayment struct {
	IdempotencyKey string `json:"idempotency_key"`
	SourceAccount  string `json:"source_account"`
	DestAccount    string `json:"dest_account"`
	Amount         string `json:"amount"`
	Currency       string `json:"currency"`
}

type paymentRecord struct {
	ID            uuid.UUID
	SourceAccount string
	DestAccount   string
	Amount        decimal.Decimal
	Currency      string
	Status        string
	LedgerTxID    *string
}

// Settle creates (idempotently by key), processes and re-reads a payment. The
// returned evidence is built only from the downstream record, so a payment
// that is pending, screened, failed or lacks a ledger transaction is refused.
func (p *Payment) Settle(ctx context.Context, key string, source, dest uuid.UUID, amount decimal.Decimal, currency string) (domain.PaymentEvidence, error) {
	if key == "" || source == uuid.Nil || dest == uuid.Nil || amount.Sign() <= 0 || currency == "" {
		return domain.PaymentEvidence{}, errors.New("invalid payment request")
	}
	c := newClient(p.HTTP)
	var created struct {
		PaymentID uuid.UUID `json:"payment_id"`
	}
	err := call(ctx, c, p.BaseURL, http.MethodPost, "/v1/payments", createPayment{key, source.String(), dest.String(), amount.StringFixed(2), currency}, &created)
	if err != nil {
		return domain.PaymentEvidence{}, err
	}
	if created.PaymentID == uuid.Nil {
		return domain.PaymentEvidence{}, errors.New("payment ID missing")
	}
	if err := call(ctx, c, p.BaseURL, http.MethodPost, "/v1/payments/"+created.PaymentID.String()+"/process", nil, nil); err != nil {
		return domain.PaymentEvidence{}, err
	}
	return p.Evidence(ctx, created.PaymentID)
}

// Evidence reads a payment and returns settled evidence or ErrNotSettled.
func (p *Payment) Evidence(ctx context.Context, id uuid.UUID) (domain.PaymentEvidence, error) {
	var rec paymentRecord
	if err := call(ctx, newClient(p.HTTP), p.BaseURL, http.MethodGet, "/v1/payments/"+id.String(), nil, &rec); err != nil {
		return domain.PaymentEvidence{}, err
	}
	if rec.ID != id || rec.Status != "SETTLED" || rec.LedgerTxID == nil {
		return domain.PaymentEvidence{}, ErrNotSettled
	}
	ledger, err := uuid.Parse(*rec.LedgerTxID)
	if err != nil || ledger == uuid.Nil {
		return domain.PaymentEvidence{}, ErrNotSettled
	}
	src, err := uuid.Parse(rec.SourceAccount)
	if err != nil {
		return domain.PaymentEvidence{}, errors.New("payment source account is not a UUID")
	}
	dst, err := uuid.Parse(rec.DestAccount)
	if err != nil {
		return domain.PaymentEvidence{}, errors.New("payment destination account is not a UUID")
	}
	return domain.PaymentEvidence{PaymentID: id, LedgerID: ledger, Status: rec.Status, Source: src, Destination: dst, Currency: rec.Currency, Amount: rec.Amount}, nil
}
