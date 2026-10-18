package command

import (
	"context"
	"errors"

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
	repo repository.LoanRepository
}

func NewCreateLoanHandler(repo repository.LoanRepository) *CreateLoanHandler {
	return &CreateLoanHandler{repo: repo}
}

func (h *CreateLoanHandler) Handle(ctx context.Context, cmd CreateLoanCommand) (domain.LoanID, error) {
	if err := commandKey(cmd.IdempotencyKey); err != nil {
		return uuid.Nil, err
	}
	if cmd.Principal.Sign() <= 0 || cmd.InterestRate.Sign() < 0 || cmd.TermMonths < 1 || cmd.TermMonths > 600 {
		return uuid.Nil, errors.New("invalid loan terms")
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
	return h.repo.CreateOrReplay(ctx, loan)
}
