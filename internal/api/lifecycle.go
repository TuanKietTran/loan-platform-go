package api

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"time"

	"github.com/TuanKietTran/loan-platform-go/internal/adapter"
	"github.com/TuanKietTran/loan-platform-go/internal/command"
	"github.com/TuanKietTran/loan-platform-go/internal/domain"
	"github.com/TuanKietTran/loan-platform-go/internal/repository"
	"github.com/go-chi/chi/v5"
	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

const (
	maxBody    = 64 << 10
	dateLayout = "2006-01-02"
)

// Lifecycle exposes the serviced-agreement routes. Cash movement happens only
// through CashService, so settled evidence always comes from the payment
// adapter and never from request JSON.
type Lifecycle struct {
	Agreements *command.AgreementService
	Cash       *command.CashService
}

// Register adds the lifecycle routes to a router mounted at /v1.
func (l *Lifecycle) Register(r chi.Router) {
	r.Post("/applications", l.apply)
	r.Get("/loans/{id}", l.get)
	r.Post("/loans/{id}/disburse", l.disburse)
	r.Post("/loans/{id}/repayments", l.repay)
	r.Post("/loans/{id}/accruals", l.accrue)
	r.Post("/loans/{id}/fees", l.fee)
	r.Get("/loans/{id}/payoff-quote", l.quote)
}

type ApplicationRequest struct {
	BorrowerID      uuid.UUID       `json:"borrower_id"`
	FundingAccount  uuid.UUID       `json:"funding_account"`
	BorrowerAccount uuid.UUID       `json:"borrower_account"`
	Principal       decimal.Decimal `json:"principal"`
	AnnualRate      decimal.Decimal `json:"annual_rate"`
	TermMonths      int             `json:"term_months"`
	MonthlyIncome   decimal.Decimal `json:"monthly_income"`
	MonthlyDebt     decimal.Decimal `json:"monthly_debt"`
	StartDate       string          `json:"start_date"`
}

type RepaymentRequest struct {
	Date       string          `json:"date"`
	Amount     decimal.Decimal `json:"amount"`
	Prepayment bool            `json:"prepayment"`
}

type AccrualRequest struct {
	Date string `json:"date"`
}

type FeeRequest struct {
	Date string          `json:"date"`
	Fee  decimal.Decimal `json:"fee"`
}

type ScheduleItemView struct {
	Number    int    `json:"number"`
	DueDate   string `json:"due_date"`
	Principal string `json:"principal"`
	Interest  string `json:"interest"`
	Total     string `json:"total"`
}

type AgreementView struct {
	ID                   string             `json:"id"`
	Status               string             `json:"status"`
	RiskBand             string             `json:"risk_band"`
	BorrowerID           string             `json:"borrower_id"`
	FundingAccount       string             `json:"funding_account"`
	BorrowerAccount      string             `json:"borrower_account"`
	Currency             string             `json:"currency"`
	Principal            string             `json:"principal"`
	AnnualRate           string             `json:"annual_rate"`
	TermMonths           int                `json:"term_months"`
	StartDate            string             `json:"start_date"`
	OutstandingPrincipal string             `json:"outstanding_principal"`
	AccruedInterest      string             `json:"accrued_interest"`
	Fees                 string             `json:"fees"`
	LastAccrualDate      string             `json:"last_accrual_date"`
	PaymentIDs           []string           `json:"payment_ids"`
	Schedule             []ScheduleItemView `json:"schedule"`
}

type AllocationView struct {
	Fees      string `json:"fees"`
	Interest  string `json:"interest"`
	Principal string `json:"principal"`
}

type RepaymentView struct {
	Agreement  AgreementView  `json:"agreement"`
	Allocation AllocationView `json:"allocation"`
}

type QuoteView struct {
	LoanID    string `json:"loan_id"`
	Date      string `json:"date"`
	Currency  string `json:"currency"`
	Principal string `json:"principal"`
	Interest  string `json:"interest"`
	Fees      string `json:"fees"`
	Total     string `json:"total"`
}

type ErrorBody struct {
	Code    string `json:"code"`
	Message string `json:"message"`
}

type ErrorEnvelope struct {
	Error ErrorBody `json:"error"`
}

func money(d decimal.Decimal) string { return d.StringFixed(2) }
func day(t time.Time) string         { return t.Format(dateLayout) }

func view(a domain.Agreement) AgreementView {
	t := a.Application.Terms
	v := AgreementView{
		ID: a.ID.String(), Status: string(a.Status), RiskBand: a.RiskBand,
		BorrowerID: a.Application.BorrowerID.String(), FundingAccount: a.Application.FundingAccount.String(), BorrowerAccount: a.Application.BorrowerAccount.String(),
		Currency: t.Currency, Principal: money(t.Principal), AnnualRate: t.AnnualRate.String(), TermMonths: t.TermMonths,
		StartDate:            day(a.Application.StartDate),
		OutstandingPrincipal: money(a.Outstanding), AccruedInterest: money(a.Interest), Fees: money(a.Fees),
		PaymentIDs: []string{}, Schedule: []ScheduleItemView{},
	}
	if !a.LastAccrual.IsZero() {
		v.LastAccrualDate = day(a.LastAccrual)
	}
	for _, id := range a.PaymentIDs {
		v.PaymentIDs = append(v.PaymentIDs, id.String())
	}
	for _, i := range a.Schedule.Instalments {
		v.Schedule = append(v.Schedule, ScheduleItemView{Number: i.Number, DueDate: day(i.DueDate), Principal: money(i.Principal), Interest: money(i.Interest), Total: money(i.Total)})
	}
	return v
}

// WriteError writes the shared JSON error envelope.
func WriteError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, ErrorEnvelope{Error: ErrorBody{Code: code, Message: message}})
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(v)
}

// writeErr maps service errors. Downstream detail is withheld from clients.
func writeErr(w http.ResponseWriter, err error) {
	var se *adapter.StatusError
	var ne net.Error
	switch {
	case errors.Is(err, repository.ErrConflict):
		WriteError(w, http.StatusConflict, "conflict", err.Error())
	case errors.Is(err, repository.ErrNotFound):
		WriteError(w, http.StatusNotFound, "not_found", err.Error())
	case errors.Is(err, repository.ErrInvalidKey):
		WriteError(w, http.StatusBadRequest, "invalid_request", "Idempotency-Key header must be 1-200 bytes and not blank")
	case errors.Is(err, adapter.ErrCircuitOpen), errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		WriteError(w, http.StatusServiceUnavailable, "unavailable", "downstream unavailable; retry with the same Idempotency-Key")
	case errors.Is(err, adapter.ErrNotSettled), errors.As(err, &se), errors.As(err, &ne):
		WriteError(w, http.StatusBadGateway, "downstream_error", "downstream service failed; retry with the same Idempotency-Key")
	default:
		WriteError(w, http.StatusUnprocessableEntity, "unprocessable", err.Error())
	}
}

func decodeBody(w http.ResponseWriter, r *http.Request, v any) bool {
	d := json.NewDecoder(http.MaxBytesReader(w, r.Body, maxBody))
	d.DisallowUnknownFields()
	if err := d.Decode(v); err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "invalid JSON body")
		return false
	}
	return true
}

func parseDate(w http.ResponseWriter, s string) (time.Time, bool) {
	t, err := time.Parse(dateLayout, s)
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "date must be YYYY-MM-DD")
		return time.Time{}, false
	}
	return t, true
}

func loanID(w http.ResponseWriter, r *http.Request) (uuid.UUID, bool) {
	id, err := uuid.Parse(chi.URLParam(r, "id"))
	if err != nil {
		WriteError(w, http.StatusBadRequest, "invalid_request", "invalid loan UUID")
		return uuid.Nil, false
	}
	return id, true
}

func (l *Lifecycle) apply(w http.ResponseWriter, r *http.Request) {
	var req ApplicationRequest
	if !decodeBody(w, r, &req) {
		return
	}
	start, ok := parseDate(w, req.StartDate)
	if !ok {
		return
	}
	app := domain.Application{
		BorrowerID: req.BorrowerID, FundingAccount: req.FundingAccount, BorrowerAccount: req.BorrowerAccount,
		Terms:         domain.LoanTerms{Principal: req.Principal, AnnualRate: req.AnnualRate, TermMonths: req.TermMonths, Currency: "USD", MinorUnitScale: 2, Method: domain.StraightLine},
		MonthlyIncome: req.MonthlyIncome, MonthlyDebt: req.MonthlyDebt, StartDate: start,
	}
	a, err := l.Agreements.ApplyApplication(r.Context(), r.Header.Get("Idempotency-Key"), app)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view(a))
}

func (l *Lifecycle) get(w http.ResponseWriter, r *http.Request) {
	id, ok := loanID(w, r)
	if !ok {
		return
	}
	a, err := l.Agreements.Store.GetAgreement(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view(a))
}

func (l *Lifecycle) disburse(w http.ResponseWriter, r *http.Request) {
	id, ok := loanID(w, r)
	if !ok {
		return
	}
	res, err := l.Cash.Disburse(r.Context(), id, r.Header.Get("Idempotency-Key"))
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view(res.Agreement))
}

func (l *Lifecycle) repay(w http.ResponseWriter, r *http.Request) {
	id, ok := loanID(w, r)
	if !ok {
		return
	}
	var req RepaymentRequest
	if !decodeBody(w, r, &req) {
		return
	}
	date, ok := parseDate(w, req.Date)
	if !ok {
		return
	}
	res, err := l.Cash.Repay(r.Context(), id, r.Header.Get("Idempotency-Key"), date, req.Amount, req.Prepayment)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, RepaymentView{Agreement: view(res.Agreement), Allocation: AllocationView{Fees: money(res.Allocation.Fees), Interest: money(res.Allocation.Interest), Principal: money(res.Allocation.Principal)}})
}

func (l *Lifecycle) accrue(w http.ResponseWriter, r *http.Request) {
	id, ok := loanID(w, r)
	if !ok {
		return
	}
	var req AccrualRequest
	if !decodeBody(w, r, &req) {
		return
	}
	date, ok := parseDate(w, req.Date)
	if !ok {
		return
	}
	res, err := l.Agreements.Accrue(r.Context(), id, r.Header.Get("Idempotency-Key"), date)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view(res.Agreement))
}

func (l *Lifecycle) fee(w http.ResponseWriter, r *http.Request) {
	id, ok := loanID(w, r)
	if !ok {
		return
	}
	var req FeeRequest
	if !decodeBody(w, r, &req) {
		return
	}
	date, ok := parseDate(w, req.Date)
	if !ok {
		return
	}
	res, err := l.Agreements.Assess(r.Context(), id, r.Header.Get("Idempotency-Key"), date, req.Fee)
	if err != nil {
		writeErr(w, err)
		return
	}
	writeJSON(w, http.StatusOK, view(res.Agreement))
}

// quote reports remaining principal, interest accrued through the date
// (ACT/365F, date excluded) and unpaid fees of the serviced agreement. It does
// not mutate the stored agreement.
func (l *Lifecycle) quote(w http.ResponseWriter, r *http.Request) {
	id, ok := loanID(w, r)
	if !ok {
		return
	}
	date, ok := parseDate(w, r.URL.Query().Get("date"))
	if !ok {
		return
	}
	a, err := l.Agreements.Store.GetAgreement(r.Context(), id)
	if err != nil {
		writeErr(w, err)
		return
	}
	total, err := a.Quote(date)
	if err != nil {
		writeErr(w, err)
		return
	}
	next := a.Clone()
	_ = next.Accrue(date)
	writeJSON(w, http.StatusOK, QuoteView{LoanID: id.String(), Date: day(date), Currency: a.Application.Terms.Currency, Principal: money(next.Outstanding), Interest: money(next.Interest), Fees: money(next.Fees), Total: money(total)})
}
