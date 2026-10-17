package command

import (
	"context"

	"github.com/example/loan-platform/internal/domain"
	"github.com/example/loan-platform/internal/repository"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type CreateLoanCommand struct {
	Principal    decimal.Decimal
	InterestRate decimal.Decimal
	TermMonths   int
}

type CreateLoanHandler struct {
	repo repository.LoanRepository
}

func NewCreateLoanHandler(repo repository.LoanRepository) *CreateLoanHandler {
	return &CreateLoanHandler{repo: repo}
}

func (h *CreateLoanHandler) Handle(ctx context.Context, cmd CreateLoanCommand) (domain.LoanID, error) {
	loan := domain.Loan{
		ID:                   uuid.New(),
		Principal:            cmd.Principal,
		InterestRate:         cmd.InterestRate,
		TermMonths:           cmd.TermMonths,
		Status:               domain.StatusPending,
		OutstandingPrincipal: cmd.Principal,
		AccruedInterest:      decimal.Zero,
	}
	if err := h.repo.Save(ctx, loan); err != nil {
		return uuid.Nil, err
	}
	return loan.ID, nil
}
