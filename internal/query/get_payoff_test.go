package query

import (
	"context"
	"testing"

	"github.com/TuanKietTran/loan-platform-go/internal/domain"
	"github.com/TuanKietTran/loan-platform-go/internal/repository"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func TestLegacyPayoffIncludesAccruedInterestAndMissingLoan(t *testing.T) {
	ctx := context.Background()
	store := repository.NewInMemoryLoan()
	handler := NewGetPayoffHandler(store)
	id := uuid.New()
	loan := domain.Loan{ID: id, Principal: decimal.NewFromInt(1000), OutstandingPrincipal: decimal.NewFromInt(900), AccruedInterest: decimal.RequireFromString("12.34")}
	if err := store.Save(ctx, loan); err != nil {
		t.Fatal(err)
	}
	quote, err := handler.Handle(ctx, GetPayoffQuery{LoanID: id})
	if err != nil || !quote.Principal.Equal(loan.OutstandingPrincipal) || !quote.AccruedInterest.Equal(loan.AccruedInterest) || !quote.Total.Equal(decimal.RequireFromString("912.34")) {
		t.Fatalf("%+v %v", quote, err)
	}
	if _, err = handler.Handle(ctx, GetPayoffQuery{LoanID: uuid.New()}); err == nil {
		t.Fatal("missing loan")
	}
}
