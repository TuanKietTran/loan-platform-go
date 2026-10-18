package repository

import (
	"context"
	"errors"
	"sync"

	"github.com/TuanKietTran/loan-platform-go/internal/domain"
	"github.com/google/uuid"
)

var (
	ErrNotFound   = errors.New("agreement not found")
	ErrConflict   = errors.New("idempotency payload conflict")
	ErrInvalidKey = errors.New("idempotency key and fingerprint required")
)

type AgreementResult struct {
	Agreement  domain.Agreement
	Allocation domain.Allocation
}

func (r AgreementResult) clone() AgreementResult { r.Agreement = r.Agreement.Clone(); return r }

// AgreementRepository applies only pure mutations under a store transaction.
// Network I/O must occur outside Apply. A replay returns the original snapshot,
// even if later commands have advanced the current agreement.
type AgreementRepository interface {
	FindApplication(context.Context, string, string) (domain.Agreement, error)
	CreateAgreement(context.Context, string, string, domain.Agreement) (domain.Agreement, error)
	GetAgreement(context.Context, uuid.UUID) (domain.Agreement, error)
	Apply(context.Context, uuid.UUID, string, string, func(*domain.Agreement) (domain.Allocation, error)) (AgreementResult, error)
}

type applicationRecord struct {
	fingerprint string
	agreement   domain.Agreement
}
type operationIdentity struct {
	id  uuid.UUID
	key string
}
type operationRecord struct {
	fingerprint string
	result      AgreementResult
}

// MemoryAgreements provides process-local atomicity, not restart durability.
type MemoryAgreements struct {
	mu           sync.Mutex
	agreements   map[uuid.UUID]domain.Agreement
	applications map[string]applicationRecord
	operations   map[operationIdentity]operationRecord
}

func NewMemoryAgreements() *MemoryAgreements {
	return &MemoryAgreements{agreements: make(map[uuid.UUID]domain.Agreement), applications: make(map[string]applicationRecord), operations: make(map[operationIdentity]operationRecord)}
}
func validIdentity(key, fingerprint string) error {
	if key == "" || fingerprint == "" {
		return ErrInvalidKey
	}
	return nil
}
func (m *MemoryAgreements) FindApplication(ctx context.Context, key, fingerprint string) (domain.Agreement, error) {
	if err := validIdentity(key, fingerprint); err != nil {
		return domain.Agreement{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return domain.Agreement{}, err
	}
	prior, ok := m.applications[key]
	if !ok {
		return domain.Agreement{}, ErrNotFound
	}
	if prior.fingerprint != fingerprint {
		return domain.Agreement{}, ErrConflict
	}
	return prior.agreement.Clone(), nil
}
func (m *MemoryAgreements) CreateAgreement(ctx context.Context, key, fingerprint string, a domain.Agreement) (domain.Agreement, error) {
	if err := validIdentity(key, fingerprint); err != nil {
		return domain.Agreement{}, err
	}
	if a.ID == uuid.Nil {
		return domain.Agreement{}, errors.New("agreement ID required")
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return domain.Agreement{}, err
	}
	if prior, ok := m.applications[key]; ok {
		if prior.fingerprint != fingerprint {
			return domain.Agreement{}, ErrConflict
		}
		return prior.agreement.Clone(), nil
	}
	if _, ok := m.agreements[a.ID]; ok {
		return domain.Agreement{}, ErrConflict
	}
	m.agreements[a.ID] = a.Clone()
	m.applications[key] = applicationRecord{fingerprint: fingerprint, agreement: a.Clone()}
	return a.Clone(), nil
}
func (m *MemoryAgreements) GetAgreement(ctx context.Context, id uuid.UUID) (domain.Agreement, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return domain.Agreement{}, err
	}
	a, ok := m.agreements[id]
	if !ok {
		return domain.Agreement{}, ErrNotFound
	}
	return a.Clone(), nil
}
func (m *MemoryAgreements) Apply(ctx context.Context, id uuid.UUID, key, fingerprint string, change func(*domain.Agreement) (domain.Allocation, error)) (AgreementResult, error) {
	if err := validIdentity(key, fingerprint); err != nil {
		return AgreementResult{}, err
	}
	m.mu.Lock()
	defer m.mu.Unlock()
	if err := ctx.Err(); err != nil {
		return AgreementResult{}, err
	}
	identity := operationIdentity{id: id, key: key}
	if prior, ok := m.operations[identity]; ok {
		if prior.fingerprint != fingerprint {
			return AgreementResult{}, ErrConflict
		}
		return prior.result.clone(), nil
	}
	a, ok := m.agreements[id]
	if !ok {
		return AgreementResult{}, ErrNotFound
	}
	next := a.Clone()
	allocation, err := change(&next)
	if err != nil {
		return AgreementResult{}, err
	}
	if err := ctx.Err(); err != nil {
		return AgreementResult{}, err
	}
	if next.ID != id {
		return AgreementResult{}, errors.New("agreement identity cannot change")
	}
	result := AgreementResult{Agreement: next.Clone(), Allocation: allocation}
	m.agreements[id] = next.Clone()
	m.operations[identity] = operationRecord{fingerprint: fingerprint, result: result.clone()}
	return result, nil
}
