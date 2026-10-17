package main

import (
	"log"
	"net/http"

	"github.com/TuanKietTran/loan-platform-go/internal/api"
	"github.com/TuanKietTran/loan-platform-go/internal/command"
	"github.com/TuanKietTran/loan-platform-go/internal/query"
	"github.com/TuanKietTran/loan-platform-go/internal/repository"
	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
)

func main() {
	repo := repository.NewInMemoryLoan()
	createH := command.NewCreateLoanHandler(repo)
	payoffH := query.NewGetPayoffHandler(repo)
	handlers := api.NewHandlers(createH, payoffH)

	r := chi.NewRouter()
	r.Use(middleware.Logger)
	r.Use(middleware.Recoverer)
	r.Mount("/v1", handlers.Routes())

	log.Println("Loan API (in-memory) listening on :8084")
	log.Fatal(http.ListenAndServe(":8084", r))
}
