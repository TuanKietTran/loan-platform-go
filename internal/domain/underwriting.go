package domain

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/shopspring/decimal"
)

// RoundingMode identifies the minor-unit rounding applied to instalments and interest.
const RoundingMode = "half-away-from-zero"

// DayCountConvention identifies the accrual year used by daily interest calculations.
const DayCountConvention = "ACT/365F"

type RepaymentMethod string

const (
	Annuity      RepaymentMethod = "ANNUITY"
	StraightLine RepaymentMethod = "STRAIGHT_LINE"
)

type LoanTerms struct {
	Principal      decimal.Decimal
	AnnualRate     decimal.Decimal // nominal annual rate as a fraction, e.g. 0.12
	TermMonths     int
	Currency       string
	MinorUnitScale int32
	Method         RepaymentMethod
}

type Instalment struct {
	Number                     int
	DueDate                    time.Time
	Principal, Interest, Total decimal.Decimal
}
type Schedule struct {
	Currency    string
	Scale       int32
	Method      RepaymentMethod
	Instalments []Instalment
}

func (t LoanTerms) validate() error {
	if t.Principal.Sign() <= 0 {
		return errors.New("principal must be positive")
	}
	if t.AnnualRate.Sign() < 0 {
		return errors.New("annual rate cannot be negative")
	}
	if t.TermMonths <= 0 || t.TermMonths > 600 {
		return errors.New("term_months must be between 1 and 600")
	}
	if len(t.Currency) != 3 || strings.ToUpper(t.Currency) != t.Currency {
		return errors.New("currency must be a three-letter uppercase code")
	}
	if t.MinorUnitScale < 0 || t.MinorUnitScale > 6 {
		return errors.New("minor unit scale must be between 0 and 6")
	}
	if t.Method != Annuity && t.Method != StraightLine {
		return errors.New("unsupported repayment method")
	}
	return nil
}

func Underwrite(monthlyIncome, monthlyDebt, monthlyPayment, maxDebtRatio decimal.Decimal) (string, error) {
	if monthlyIncome.Sign() <= 0 || monthlyDebt.Sign() < 0 || monthlyPayment.Sign() < 0 || maxDebtRatio.Sign() <= 0 || maxDebtRatio.GreaterThan(decimal.NewFromInt(1)) {
		return "", errors.New("invalid affordability inputs")
	}
	if monthlyDebt.Add(monthlyPayment).Div(monthlyIncome).GreaterThan(maxDebtRatio) {
		return "", errors.New("affordability limit exceeded")
	}
	if monthlyDebt.Add(monthlyPayment).Div(monthlyIncome).LessThanOrEqual(decimal.RequireFromString("0.25")) {
		return "LOW", nil
	}
	if monthlyDebt.Add(monthlyPayment).Div(monthlyIncome).LessThanOrEqual(decimal.RequireFromString("0.40")) {
		return "MEDIUM", nil
	}
	return "HIGH", nil
}

func GenerateSchedule(t LoanTerms, start time.Time) (Schedule, error) {
	if err := t.validate(); err != nil {
		return Schedule{}, err
	}
	principal := t.Principal.Round(t.MinorUnitScale)
	monthlyRate := t.AnnualRate.Div(decimal.NewFromInt(12))
	payment := decimal.Zero
	if t.Method == StraightLine {
		payment = principal.Div(decimal.NewFromInt(int64(t.TermMonths))).Round(t.MinorUnitScale)
	} else if monthlyRate.IsZero() {
		payment = principal.Div(decimal.NewFromInt(int64(t.TermMonths))).Round(t.MinorUnitScale)
	} else {
		factor := decimal.NewFromInt(1).Add(monthlyRate).Pow(decimal.NewFromInt(int64(t.TermMonths)))
		payment = principal.Mul(monthlyRate).Mul(factor).Div(factor.Sub(decimal.NewFromInt(1))).Round(t.MinorUnitScale)
	}
	remaining := principal
	s := Schedule{Currency: t.Currency, Scale: t.MinorUnitScale, Method: t.Method, Instalments: make([]Instalment, 0, t.TermMonths)}
	for i := 1; i <= t.TermMonths; i++ {
		interest := remaining.Mul(monthlyRate).Round(t.MinorUnitScale)
		p := payment.Sub(interest)
		if t.Method == StraightLine {
			p = principal.Div(decimal.NewFromInt(int64(t.TermMonths))).Round(t.MinorUnitScale)
		}
		if i == t.TermMonths || p.GreaterThan(remaining) {
			p = remaining
		}
		if p.Sign() < 0 {
			return Schedule{}, errors.New("instalment does not amortize principal")
		}
		remaining = remaining.Sub(p)
		s.Instalments = append(s.Instalments, Instalment{Number: i, DueDate: start.AddDate(0, i, 0), Principal: p, Interest: interest, Total: p.Add(interest)})
	}
	if !remaining.IsZero() {
		return Schedule{}, fmt.Errorf("principal not conserved: remainder %s", remaining)
	}
	return s, nil
}

func (s Schedule) TotalPrincipal() decimal.Decimal {
	total := decimal.Zero
	for _, i := range s.Instalments {
		total = total.Add(i.Principal)
	}
	return total
}
func Payoff(outstanding, accruedInterest, fee decimal.Decimal, scale int32) (decimal.Decimal, error) {
	if outstanding.Sign() < 0 || accruedInterest.Sign() < 0 || fee.Sign() < 0 {
		return decimal.Zero, errors.New("payoff components cannot be negative")
	}
	return outstanding.Add(accruedInterest).Add(fee).Round(scale), nil
}

// ApplyPrepayment reduces principal; a new schedule is generated for the remaining term.
func ApplyPrepayment(outstanding, prepayment decimal.Decimal, scale int32) (decimal.Decimal, error) {
	if outstanding.Sign() <= 0 || prepayment.Sign() <= 0 || prepayment.GreaterThanOrEqual(outstanding) {
		return decimal.Zero, errors.New("prepayment must be positive and less than outstanding principal")
	}
	return outstanding.Sub(prepayment).Round(scale), nil
}

// ACT/365F daily interest; the returned amount is rounded once at the currency scale.
func DailyInterest(principal, annualRate decimal.Decimal, days int, scale int32) (decimal.Decimal, error) {
	if principal.Sign() < 0 || annualRate.Sign() < 0 || days < 0 {
		return decimal.Zero, errors.New("negative accrual input")
	}
	return principal.Mul(annualRate).Mul(decimal.NewFromInt(int64(days))).Div(decimal.NewFromInt(365)).Round(scale), nil
}
