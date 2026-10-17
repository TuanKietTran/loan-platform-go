package main

import (
	"context"
	"fmt"
	"log"
	"net/http"

	"github.com/example/loan-platform/internal/api"
	"github.com/example/loan-platform/internal/command"
	"github.com/example/loan-platform/internal/domain"
	"github.com/example/loan-platform/internal/query"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/google/uuid"
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
	handlers := api.NewHandlers(createH, payoffH)

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Mount("/v1", handlers.Routes())

	log.Println("Loan API listening on :8084")
	log.Fatal(http.ListenAndServe(":8084", r))
}
