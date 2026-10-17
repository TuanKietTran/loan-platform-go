package command

import (
	"context"
	"fmt"
	"sync"

	"github.com/TuanKietTran/loan-platform-go/internal/domain"
	"github.com/TuanKietTran/loan-platform-go/internal/repository"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type CreateLoanCommand struct {
	Principal      decimal.Decimal
	InterestRate   decimal.Decimal
	TermMonths     int
	IdempotencyKey string
}

type CreateLoanHandler struct {
	mu   sync.Mutex
	repo repository.LoanRepository
}

func NewCreateLoanHandler(repo repository.LoanRepository) *CreateLoanHandler {
	return &CreateLoanHandler{repo: repo}
}

func (h *CreateLoanHandler) Handle(ctx context.Context, cmd CreateLoanCommand) (domain.LoanID, error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if finder, ok := h.repo.(interface {
		GetByKey(string) (*domain.Loan, bool)
	}); ok {
		if prior, found := finder.GetByKey(cmd.IdempotencyKey); found {
			if prior.Principal.Equal(cmd.Principal) && prior.InterestRate.Equal(cmd.InterestRate) && prior.TermMonths == cmd.TermMonths {
				return prior.ID, nil
			}
			return uuid.Nil, fmt.Errorf("idempotency payload conflict")
		}
	}
	loan := domain.Loan{
		ID:                   uuid.New(),
		Principal:            cmd.Principal,
		InterestRate:         cmd.InterestRate,
		TermMonths:           cmd.TermMonths,
		Status:               domain.StatusPending,
		OutstandingPrincipal: cmd.Principal,
		AccruedInterest:      decimal.Zero,
		IdempotencyKey:       cmd.IdempotencyKey,
	}
	if err := h.repo.Save(ctx, loan); err != nil {
		return uuid.Nil, err
	}
	return loan.ID, nil
}
