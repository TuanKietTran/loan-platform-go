package main

import (
	"context"
	"fmt"
	"log"

	"github.com/example/loan-platform/internal/command"
	"github.com/example/loan-platform/internal/domain"
	"github.com/example/loan-platform/internal/query"
	"github.com/example/loan-platform/internal/repository"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type memRepo struct {
	store map[uuid.UUID]domain.Loan
}

func (m *memRepo) Save(ctx context.Context, l domain.Loan) error {
	if m.store == nil {
		m.store = make(map[uuid.UUID]domain.Loan)
	}
	m.store[l.ID] = l
	return nil
}
func (m *memRepo) GetByID(ctx context.Context, id domain.LoanID) (*domain.Loan, error) {
	l, ok := m.store[id]
	if !ok {
		return nil, fmt.Errorf("not found")
	}
	return &l, nil
}

func main() {
	repo := &memRepo{}
	createH := command.NewCreateLoanHandler(repo)
	payoffH := query.NewGetPayoffHandler(repo)

	id, err := createH.Handle(context.Background(), command.CreateLoanCommand{
		Principal:    decimal.NewFromInt(10000),
		InterestRate: decimal.NewFromFloat(0.08),
		TermMonths:   36,
	})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Println("Created loan:", id)

	quote, err := payoffH.Handle(context.Background(), query.GetPayoffQuery{LoanID: id})
	if err != nil {
		log.Fatal(err)
	}
	fmt.Printf("Payoff: principal=%s interest=%s total=%s\n",
		quote.Principal, quote.AccruedInterest, quote.Total)
}
