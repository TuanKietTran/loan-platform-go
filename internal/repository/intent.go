package repository

import (
	"context"
	"sync"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

// CashIntent is the durable record of a cash movement the loan service has
// decided to request. It is persisted before any payment call so a crash or
// timeout can be reconciled against the payment service using PaymentKey.
type CashIntent struct {
	AgreementID uuid.UUID
	Key         string
	Fingerprint string
	Operation   string // disburse or repay
	Date        time.Time
	Prepayment  bool
	Source      uuid.UUID
	Destination uuid.UUID
	Amount      decimal.Decimal
	Currency    string
	PaymentKey  string
	Done        bool
}

// IntentRepository stores cash intents. Begin is idempotent per (agreement,key):
// the same fingerprint returns the stored intent, a different one is ErrConflict.
// validate runs under the store lock only when the intent is new, and a
// rejected intent is not stored.
type IntentRepository interface {
	Begin(context.Context, CashIntent, func() error) (CashIntent, error)
	Complete(context.Context, uuid.UUID, string) error
	Pending(context.Context) ([]CashIntent, error)
}

// MemoryIntents provides process-local atomicity, not restart durability.
type MemoryIntents struct {
	mu      sync.Mutex
	intents map[operationIdentity]CashIntent
	order   []operationIdentity
}

func NewMemoryIntents() *MemoryIntents {
	return &MemoryIntents{intents: make(map[operationIdentity]CashIntent)}
}

func (m *MemoryIntents) Begin(ctx context.Context, in CashIntent, validate func() error) (CashIntent, error) {
	if err := validIdentity(in.Key, in.Fingerprint); err != nil {
		return CashIntent{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return CashIntent{}, err
	}
	id := operationIdentity{id: in.AgreementID, key: in.Key}
	if prior, ok := m.intents[id]; ok {
		if prior.Fingerprint != in.Fingerprint {
			return CashIntent{}, ErrConflict
		}
		return prior, nil
	}
	if validate != nil {
		if err := validate(); err != nil {
			return CashIntent{}, err
		}
	}
	in.Done = false
	m.intents[id] = in
	m.order = append(m.order, id)
	return in, nil
}

func (m *MemoryIntents) Complete(ctx context.Context, agreementID uuid.UUID, key string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return err
	}
	id := operationIdentity{id: agreementID, key: key}
	in, ok := m.intents[id]
	if !ok {
		return ErrNotFound
	}
	in.Done = true
	m.intents[id] = in
	return nil
}

func (m *MemoryIntents) Pending(ctx context.Context) ([]CashIntent, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	var out []CashIntent
	for _, id := range m.order {
		if in := m.intents[id]; !in.Done {
			out = append(out, in)
		}
	}
	return out, nil
}
