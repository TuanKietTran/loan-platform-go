package api

import (
	"encoding/json"
	"net/http"

	"github.com/TuanKietTran/loan-platform-go/internal/command"
	"github.com/TuanKietTran/loan-platform-go/internal/query"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

type Handlers struct {
	create *command.CreateLoanHandler
	payoff *query.GetPayoffHandler
}

func NewHandlers(c *command.CreateLoanHandler, p *query.GetPayoffHandler) *Handlers {
	return &Handlers{create: c, payoff: p}
}

func (h *Handlers) Routes() chi.Router {
	r := chi.NewRouter()
	r.Post("/loans", h.createLoan)
	r.Get("/loans/{id}/payoff", h.getPayoff)
	return r
}

type createReq struct {
	Principal      decimal.Decimal `json:"principal"`
	InterestRate   decimal.Decimal `json:"interest_rate"`
	TermMonths     int             `json:"term_months"`
	IdempotencyKey string          `json:"idempotency_key"`
}

func (h *Handlers) createLoan(w http.ResponseWriter, r *http.Request) {
	var req createReq
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, "invalid json", http.StatusBadRequest)
		return
	}
	key := req.IdempotencyKey
	if header := r.Header.Get("Idempotency-Key"); header != "" {
		if key != "" && key != header {
			http.Error(w, "idempotency key disagreement", http.StatusBadRequest)
			return
		}
		key = header
	}
	if key == "" {
		http.Error(w, "idempotency key required", http.StatusBadRequest)
		return
	}
	id, err := h.create.Handle(r.Context(), command.CreateLoanCommand{
		Principal:      req.Principal,
		InterestRate:   req.InterestRate,
		TermMonths:     req.TermMonths,
		IdempotencyKey: key,
	})
	if err != nil {
		status := http.StatusBadRequest
		if err.Error() == "idempotency payload conflict" {
			status = http.StatusConflict
		}
		http.Error(w, err.Error(), status)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]string{"loan_id": id.String()})
}

func (h *Handlers) getPayoff(w http.ResponseWriter, r *http.Request) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		http.Error(w, "invalid id", http.StatusBadRequest)
		return
	}
	q, err := h.payoff.Handle(r.Context(), query.GetPayoffQuery{LoanID: id})
	if err != nil {
		http.Error(w, err.Error(), http.StatusNotFound)
		return
	}
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(q)
}
