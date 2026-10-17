package repository

import (
	"context"
	"fmt"
	"sync"

	"github.com/example/loan-platform/internal/domain"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type InMemoryLoan struct {
	mu    sync.RWMutex
	loans map[domain.LoanID]domain.Loan
}

func NewInMemoryLoan() *InMemoryLoan {
	return &InMemoryLoan{
		loans: make(map[domain.LoanID]domain.Loan),
	}
}

func (r *InMemoryLoan) Save(ctx context.Context, l domain.Loan) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	if l.ID == uuid.Nil {
		l.ID = uuid.New()
	}
	r.loans[l.ID] = l
	return nil
}

func (r *InMemoryLoan) GetByID(ctx context.Context, id domain.LoanID) (*domain.Loan, error) {
	r.mu.RLock()
	defer r.mu.RUnlock()
	l, ok := r.loans[id]
	if !ok {
		return nil, fmt.Errorf("loan not found")
	}
	return &l, nil
}

func (r *InMemoryLoan) AccrueInterest(ctx context.Context, id domain.LoanID, days int) error {
	r.mu.Lock()
	defer r.mu.Unlock()
	l, ok := r.loans[id]
	if !ok {
		return fmt.Errorf("loan not found")
	}
	dailyRate := l.InterestRate.Div(decimal.NewFromInt(365))
	interest := l.OutstandingPrincipal.Mul(dailyRate).Mul(decimal.NewFromInt(int64(days)))
	l.AccruedInterest = l.AccruedInterest.Add(interest)
	r.loans[id] = l
	return nil
}
