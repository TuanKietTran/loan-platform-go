package repository

import (
	"context"

	"github.com/example/loan-platform/internal/domain"
)

type LoanRepository interface {
	Save(ctx context.Context, l domain.Loan) error
	GetByID(ctx context.Context, id domain.LoanID) (*domain.Loan, error)
}
