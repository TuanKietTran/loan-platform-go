package api

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/TuanKietTran/loan-platform-go/internal/command"
	"github.com/TuanKietTran/loan-platform-go/internal/query"
	"github.com/TuanKietTran/loan-platform-go/internal/repository"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
)

func TestLegacyValidationAndPayoffErrors(t *testing.T) {
	store := repository.NewInMemoryLoan()
	handlers := NewHandlers(command.NewCreateLoanHandler(store), query.NewGetPayoffHandler(store))
	r := chi.NewRouter()
	r.Mount("/v1", handlers.Routes())
	for _, tc := range []struct {
		name, body, header string
		status             int
	}{
		{"malformed", "{", "", 400},
		{"disagreement", `{"principal":"10","term_months":1,"idempotency_key":"body"}`, "header", 400},
		{"zero", `{"principal":"0","term_months":1}`, "key", 400},
		{"negative", `{"principal":"-1","term_months":1}`, "key", 400},
		{"rate", `{"principal":"10","interest_rate":"-0.1","term_months":1}`, "key", 400},
		{"term", `{"principal":"10","term_months":601}`, "key", 400},
		{"blank key", `{"principal":"10","term_months":1}`, " ", 400},
		{"header key", `{"principal":"10","term_months":1}`, "key", 200},
	} {
		t.Run(tc.name, func(t *testing.T) {
			req := httptest.NewRequest(http.MethodPost, "/v1/loans", strings.NewReader(tc.body))
			req.Header.Set("Idempotency-Key", tc.header)
			w := httptest.NewRecorder()
			r.ServeHTTP(w, req)
			if w.Code != tc.status {
				t.Fatalf("status %d: %s", w.Code, w.Body.String())
			}
		})
	}
	loan, ok := store.GetByKey("key")
	if !ok {
		t.Fatal("header key not persisted")
	}
	for _, tc := range []struct {
		id     string
		status int
	}{{"not-a-uuid", 400}, {uuid.NewString(), 404}, {loan.ID.String(), 200}} {
		w := httptest.NewRecorder()
		r.ServeHTTP(w, httptest.NewRequest(http.MethodGet, "/v1/loans/"+tc.id+"/payoff", nil))
		if w.Code != tc.status {
			t.Fatalf("payoff status %d: %s", w.Code, w.Body.String())
		}
	}
}
