package query

import (
	"context"

	"github.com/TuanKietTran/loan-platform-go/internal/domain"
	"github.com/TuanKietTran/loan-platform-go/internal/repository"
	"github.com/shopspring/decimal"
)

type GetPayoffQuery struct {
	LoanID domain.LoanID
}

type PayoffQuote struct {
	Principal       decimal.Decimal
	AccruedInterest decimal.Decimal
	Total           decimal.Decimal
}

type GetPayoffHandler struct {
	repo repository.LoanRepository
}

func NewGetPayoffHandler(repo repository.LoanRepository) *GetPayoffHandler {
	return &GetPayoffHandler{repo: repo}
}

func (h *GetPayoffHandler) Handle(ctx context.Context, q GetPayoffQuery) (PayoffQuote, error) {
	loan, err := h.repo.GetByID(ctx, q.LoanID)
	if err != nil {
		return PayoffQuote{}, err
	}
	total := loan.OutstandingPrincipal.Add(loan.AccruedInterest)
	return PayoffQuote{
		Principal:       loan.OutstandingPrincipal,
		AccruedInterest: loan.AccruedInterest,
		Total:           total,
	}, nil
}
