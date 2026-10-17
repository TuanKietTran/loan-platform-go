package repository

import (
	"context"

	"github.com/TuanKietTran/loan-platform-go/internal/domain"
)

type LoanRepository interface {
	Save(ctx context.Context, l domain.Loan) error
	GetByID(ctx context.Context, id domain.LoanID) (*domain.Loan, error)
}
