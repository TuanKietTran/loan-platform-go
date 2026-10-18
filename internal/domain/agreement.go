package domain

import (
	"errors"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

const (
	StatusReview     LoanStatus = "REVIEW"
	StatusDeclined   LoanStatus = "DECLINED"
	StatusDelinquent LoanStatus = "DELINQUENT"
)

// Application is a synthetic USD application. Dates are UTC calendar dates.
type Application struct {
	BorrowerID      uuid.UUID
	FundingAccount  uuid.UUID
	BorrowerAccount uuid.UUID
	Terms           LoanTerms
	MonthlyIncome   decimal.Decimal
	MonthlyDebt     decimal.Decimal
	StartDate       time.Time
}

type Agreement struct {
	ID          uuid.UUID
	Application Application
	Status      LoanStatus
	RiskBand    string
	Schedule    Schedule
	Outstanding decimal.Decimal
	Interest    decimal.Decimal
	Fees        decimal.Decimal
	LastAccrual time.Time
	// PrincipalDays and InterestBooked retain sub-cent accrual across calls.
	PrincipalDays     decimal.Decimal
	InterestBooked    decimal.Decimal
	PaidSinceSchedule decimal.Decimal
	FeeInstalments    []int
	PaymentIDs        []uuid.UUID
}

// PaymentEvidence is accepted only after the payment service reports SETTLED
// with an associated ledger transaction. Loans never post a parallel journal.
type PaymentEvidence struct {
	PaymentID   uuid.UUID
	LedgerID    uuid.UUID
	Status      string
	Source      uuid.UUID
	Destination uuid.UUID
	Currency    string
	Amount      decimal.Decimal
}

func calendarDate(t time.Time) bool {
	return !t.IsZero() && t.Year() >= 1 && t.Year() <= 9999 && t.Location() == time.UTC && t.Hour() == 0 && t.Minute() == 0 && t.Second() == 0 && t.Nanosecond() == 0
}
func (a Application) Validate() error {
	if a.BorrowerID == uuid.Nil || a.FundingAccount == uuid.Nil || a.BorrowerAccount == uuid.Nil || a.FundingAccount == a.BorrowerAccount {
		return errors.New("distinct funding and borrower accounts and borrower ID required")
	}
	if !calendarDate(a.StartDate) {
		return errors.New("start_date must be a UTC calendar date")
	}
	if a.Terms.Currency != "USD" || a.Terms.MinorUnitScale != 2 {
		return errors.New("only USD scale 2 is supported")
	}
	if !a.Terms.Principal.Equal(a.Terms.Principal.Round(2)) {
		return errors.New("principal exceeds minor-unit precision")
	}
	if err := a.Terms.validate(); err != nil {
		return err
	}
	if a.MonthlyIncome.Sign() <= 0 || a.MonthlyDebt.Sign() < 0 {
		return errors.New("invalid income or debt")
	}
	return nil
}

// Originate applies a fixed 40% affordability ceiling to the largest instalment.
// REVIEW and denied applications are retained but cannot be disbursed.
func Originate(id uuid.UUID, a Application, fraudDecision string) (Agreement, error) {
	if id == uuid.Nil {
		return Agreement{}, errors.New("agreement ID required")
	}
	if err := a.Validate(); err != nil {
		return Agreement{}, err
	}
	s, err := GenerateSchedule(a.Terms, a.StartDate)
	if err != nil {
		return Agreement{}, err
	}
	max := decimal.Zero
	for _, i := range s.Instalments {
		if i.Total.GreaterThan(max) {
			max = i.Total
		}
	}
	band, affordability := Underwrite(a.MonthlyIncome, a.MonthlyDebt, max, decimal.RequireFromString("0.40"))
	status := StatusPending
	switch fraudDecision {
	case "ALLOW":
	case "REVIEW":
		status = StatusReview
	case "DENY", "BLOCK":
		status = StatusDeclined
	default:
		return Agreement{}, errors.New("unknown fraud decision")
	}
	if affordability != nil {
		status = StatusDeclined
	}
	return Agreement{ID: id, Application: a, Status: status, RiskBand: band, Schedule: s}, nil
}

func (a Agreement) Clone() Agreement {
	a.Schedule.Instalments = append([]Instalment(nil), a.Schedule.Instalments...)
	a.FeeInstalments = append([]int(nil), a.FeeInstalments...)
	a.PaymentIDs = append([]uuid.UUID(nil), a.PaymentIDs...)
	return a
}

func (a Agreement) validateEvidence(p PaymentEvidence, amount decimal.Decimal, disbursement bool) error {
	source, dest := a.Application.BorrowerAccount, a.Application.FundingAccount
	if disbursement {
		source, dest = dest, source
	}
	if p.PaymentID == uuid.Nil || p.LedgerID == uuid.Nil || p.Status != "SETTLED" || p.Currency != a.Application.Terms.Currency || p.Source != source || p.Destination != dest || !p.Amount.Equal(amount) {
		return errors.New("settled payment evidence does not match cash movement")
	}
	for _, id := range a.PaymentIDs {
		if id == p.PaymentID {
			return errors.New("payment already applied")
		}
	}
	return nil
}

func (a *Agreement) Disburse(p PaymentEvidence) error {
	if a.Status != StatusPending {
		return errors.New("only approved pending agreements can disburse")
	}
	if err := a.validateEvidence(p, a.Application.Terms.Principal, true); err != nil {
		return err
	}
	a.Outstanding = a.Application.Terms.Principal
	a.LastAccrual = a.Application.StartDate
	a.Status = StatusActive
	a.PaymentIDs = append(a.PaymentIDs, p.PaymentID)
	return nil
}

func (a Agreement) servicing() bool { return a.Status == StatusActive || a.Status == StatusDelinquent }

// Accrue books ACT/365F interest through date, excluding that date. Repeated
// calls at the same date do nothing; splitting periods does not round twice.
func (a *Agreement) Accrue(date time.Time) error {
	if !a.servicing() {
		return errors.New("agreement is not servicing")
	}
	if !calendarDate(date) || date.Before(a.LastAccrual) {
		return errors.New("accrual date must not move backwards")
	}
	days := (date.Unix() - a.LastAccrual.Unix()) / 86400
	basis := a.PrincipalDays.Add(a.Outstanding.Mul(decimal.NewFromInt(days)))
	booked := basis.Mul(a.Application.Terms.AnnualRate).Div(decimal.NewFromInt(365)).Round(2)
	a.Interest = a.Interest.Add(booked.Sub(a.InterestBooked))
	a.PrincipalDays, a.InterestBooked, a.LastAccrual = basis, booked, date
	return nil
}

// Assess applies one fixed fee per overdue unpaid instalment, never per retry.
// An instalment is overdue the day after its due date; no grace period is used.
func (a *Agreement) Assess(date time.Time, lateFee decimal.Decimal) error {
	if lateFee.Sign() < 0 || !lateFee.Equal(lateFee.Round(2)) {
		return errors.New("invalid late fee")
	}
	if err := a.Accrue(date); err != nil {
		return err
	}
	required, delinquent := decimal.Zero, false
	for _, i := range a.Schedule.Instalments {
		required = required.Add(i.Total)
		if !i.DueDate.Before(date) || !required.GreaterThan(a.PaidSinceSchedule) {
			continue
		}
		delinquent = true
		seen := false
		for _, n := range a.FeeInstalments {
			if n == i.Number {
				seen = true
				break
			}
		}
		if !seen {
			a.Fees = a.Fees.Add(lateFee)
			a.FeeInstalments = append(a.FeeInstalments, i.Number)
		}
	}
	a.Status = StatusActive
	if delinquent {
		a.Status = StatusDelinquent
	}
	return nil
}

func (a Agreement) Quote(date time.Time) (decimal.Decimal, error) {
	if err := a.Accrue(date); err != nil {
		return decimal.Zero, err
	}
	return Payoff(a.Outstanding, a.Interest, a.Fees, 2)
}

type Allocation struct{ Fees, Interest, Principal decimal.Decimal }

// Repay allocates fees, then accrued interest, then principal. A prepayment
// regenerates the remaining schedule from the payment date with the same rate
// and original maturity count. Failed evidence or amounts never mutate state.
func (a *Agreement) Repay(date time.Time, p PaymentEvidence, prepayment bool) (Allocation, error) {
	next := a.Clone()
	if err := next.Accrue(date); err != nil {
		return Allocation{}, err
	}
	if p.Amount.Sign() <= 0 || !p.Amount.Equal(p.Amount.Round(2)) {
		return Allocation{}, errors.New("payment must be positive minor units")
	}
	if err := next.validateEvidence(p, p.Amount, false); err != nil {
		return Allocation{}, err
	}
	total := next.Outstanding.Add(next.Interest).Add(next.Fees)
	if p.Amount.GreaterThan(total) {
		return Allocation{}, errors.New("payment exceeds payoff")
	}
	left := p.Amount
	allocation := Allocation{}
	allocation.Fees = decimal.Min(left, next.Fees)
	left = left.Sub(allocation.Fees)
	allocation.Interest = decimal.Min(left, next.Interest)
	left = left.Sub(allocation.Interest)
	allocation.Principal = left
	next.Fees = next.Fees.Sub(allocation.Fees)
	next.Interest = next.Interest.Sub(allocation.Interest)
	next.Outstanding = next.Outstanding.Sub(allocation.Principal)
	next.PaidSinceSchedule = next.PaidSinceSchedule.Add(allocation.Interest).Add(allocation.Principal)
	if next.Outstanding.IsZero() && next.Interest.IsZero() && next.Fees.IsZero() {
		next.Status = StatusPaidOff
	} else if prepayment && allocation.Principal.IsPositive() {
		remaining := 0
		for _, i := range next.Schedule.Instalments {
			if i.DueDate.After(date) {
				remaining++
			}
		}
		// Past maturity, retain one final instalment a month after the payment.
		if remaining == 0 {
			remaining = 1
		}
		terms := next.Application.Terms
		terms.Principal, terms.TermMonths = next.Outstanding, remaining
		schedule, err := GenerateSchedule(terms, date)
		if err != nil {
			return Allocation{}, err
		}
		next.Schedule = schedule
		next.PaidSinceSchedule = decimal.Zero
		next.FeeInstalments = nil
		next.Status = StatusActive
	}
	next.PaymentIDs = append(next.PaymentIDs, p.PaymentID)
	*a = next
	return allocation, nil
}
