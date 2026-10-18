package domain

import (
	"reflect"
	"testing"
	"time"

	"github.com/google/uuid"
	"github.com/shopspring/decimal"
)

func application() Application {
	return Application{BorrowerID: uuid.New(), FundingAccount: uuid.New(), BorrowerAccount: uuid.New(), Terms: terms(StraightLine, "0.12", 12, "1200"), MonthlyIncome: dec("2000"), MonthlyDebt: dec("100"), StartDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)}
}
func agreement(t *testing.T) Agreement {
	t.Helper()
	a, err := Originate(uuid.New(), application(), "ALLOW")
	if err != nil {
		t.Fatal(err)
	}
	return a
}
func evidence(a Agreement, amount string, disburse bool) PaymentEvidence {
	source, dest := a.Application.BorrowerAccount, a.Application.FundingAccount
	if disburse {
		source, dest = dest, source
	}
	return PaymentEvidence{PaymentID: uuid.New(), LedgerID: uuid.New(), Status: "SETTLED", Source: source, Destination: dest, Currency: "USD", Amount: dec(amount)}
}
func active(t *testing.T) Agreement {
	t.Helper()
	a := agreement(t)
	if err := a.Disburse(evidence(a, "1200", true)); err != nil {
		t.Fatal(err)
	}
	return a
}
func TestApplicationValidation(t *testing.T) {
	for name, change := range map[string]func(*Application){
		"borrower":      func(a *Application) { a.BorrowerID = uuid.Nil },
		"funding":       func(a *Application) { a.FundingAccount = uuid.Nil },
		"destination":   func(a *Application) { a.BorrowerAccount = uuid.Nil },
		"same accounts": func(a *Application) { a.BorrowerAccount = a.FundingAccount },
		"date":          func(a *Application) { a.StartDate = time.Time{} },
		"timestamp":     func(a *Application) { a.StartDate = a.StartDate.Add(time.Hour) },
		"timezone":      func(a *Application) { a.StartDate = a.StartDate.In(time.FixedZone("local", 0)) },
		"currency":      func(a *Application) { a.Terms.Currency = "EUR" },
		"scale":         func(a *Application) { a.Terms.MinorUnitScale = 3 },
		"precision":     func(a *Application) { a.Terms.Principal = dec("1.001") },
		"term":          func(a *Application) { a.Terms.TermMonths = 0 },
		"income":        func(a *Application) { a.MonthlyIncome = decimal.Zero },
		"debt":          func(a *Application) { a.MonthlyDebt = dec("-1") },
	} {
		t.Run(name, func(t *testing.T) {
			a := application()
			change(&a)
			if _, err := Originate(uuid.New(), a, "ALLOW"); err == nil {
				t.Fatal("accepted invalid application")
			}
		})
	}
	if _, err := Originate(uuid.Nil, application(), "ALLOW"); err == nil {
		t.Fatal("missing ID")
	}
	if _, err := Originate(uuid.New(), application(), "unknown"); err == nil {
		t.Fatal("unknown decision")
	}
	for _, tc := range []struct {
		decision string
		status   LoanStatus
	}{{"ALLOW", StatusPending}, {"REVIEW", StatusReview}, {"DENY", StatusDeclined}, {"BLOCK", StatusDeclined}} {
		a, err := Originate(uuid.New(), application(), tc.decision)
		if err != nil || a.Status != tc.status || a.RiskBand != "LOW" {
			t.Fatalf("%+v %v", a, err)
		}
		if tc.status != StatusPending && a.Disburse(evidence(a, "1200", true)) == nil {
			t.Fatal("unapproved disbursement")
		}
	}
	app := application()
	app.MonthlyIncome = dec("100")
	a, err := Originate(uuid.New(), app, "ALLOW")
	if err != nil || a.Status != StatusDeclined {
		t.Fatal("affordability not enforced")
	}
}
func TestDisbursementRequiresMatchingSettledEvidence(t *testing.T) {
	for name, change := range map[string]func(*PaymentEvidence){
		"payment":     func(p *PaymentEvidence) { p.PaymentID = uuid.Nil },
		"ledger":      func(p *PaymentEvidence) { p.LedgerID = uuid.Nil },
		"pending":     func(p *PaymentEvidence) { p.Status = "PROCESSING" },
		"source":      func(p *PaymentEvidence) { p.Source = uuid.New() },
		"destination": func(p *PaymentEvidence) { p.Destination = uuid.New() },
		"currency":    func(p *PaymentEvidence) { p.Currency = "EUR" },
		"amount":      func(p *PaymentEvidence) { p.Amount = dec("1199.99") },
	} {
		t.Run(name, func(t *testing.T) {
			a := agreement(t)
			before := a.Clone()
			p := evidence(a, "1200", true)
			change(&p)
			if a.Disburse(p) == nil || !reflect.DeepEqual(a, before) {
				t.Fatal("invalid evidence mutated state")
			}
		})
	}
	a := active(t)
	if !a.Outstanding.Equal(dec("1200")) || a.Status != StatusActive || !a.LastAccrual.Equal(a.Application.StartDate) {
		t.Fatal("wrong activation")
	}
	if a.Disburse(evidence(a, "1200", true)) == nil {
		t.Fatal("double disbursement")
	}
}
func TestCumulativeAccrualLeapDayAndQuote(t *testing.T) {
	a := active(t)
	b := a.Clone()
	start := a.LastAccrual
	for day := 1; day <= 60; day++ {
		if err := a.Accrue(start.AddDate(0, 0, day)); err != nil {
			t.Fatal(err)
		}
	}
	if err := b.Accrue(start.AddDate(0, 0, 60)); err != nil {
		t.Fatal(err)
	}
	if !a.Interest.Equal(dec("23.67")) || !a.Interest.Equal(b.Interest) {
		t.Fatalf("split accrual %s vs %s", a.Interest, b.Interest)
	}
	snapshot := a.Clone()
	q, err := a.Quote(start.AddDate(0, 0, 61))
	if err != nil || !q.Equal(dec("1224.07")) || !reflect.DeepEqual(a, snapshot) {
		t.Fatalf("quote %s %v", q, err)
	}
	for _, date := range []time.Time{start, start.Add(time.Hour), {}} {
		if a.Accrue(date) == nil {
			t.Fatal("invalid accrual date")
		}
	}
	if err := a.Accrue(a.LastAccrual); err != nil || !reflect.DeepEqual(a, snapshot) {
		t.Fatal("same date changed accrual")
	}
	pending := agreement(t)
	if _, err := pending.Quote(start); err == nil {
		t.Fatal("pending quote")
	}
}
func TestLateFeeOnceAndRepaymentWaterfall(t *testing.T) {
	a := active(t)
	due := a.Schedule.Instalments[0].DueDate
	if err := a.Assess(due, dec("10")); err != nil || a.Status != StatusActive || !a.Fees.IsZero() {
		t.Fatal("charged on due date")
	}
	date := due.AddDate(0, 0, 1)
	if err := a.Assess(date, dec("10")); err != nil || a.Status != StatusDelinquent || !a.Fees.Equal(dec("10")) {
		t.Fatal("late fee")
	}
	before := a.Clone()
	if err := a.Assess(date, dec("10")); err != nil || !reflect.DeepEqual(a, before) {
		t.Fatal("duplicate fee")
	}
	p := evidence(a, "30", false)
	allocation, err := a.Repay(date, p, false)
	if err != nil || !allocation.Fees.Equal(dec("10")) || !allocation.Interest.Equal(dec("12.62")) || !allocation.Principal.Equal(dec("7.38")) {
		t.Fatalf("%+v %v", allocation, err)
	}
	before = a.Clone()
	if _, err = a.Repay(date, p, false); err == nil || !reflect.DeepEqual(a, before) {
		t.Fatal("duplicate repayment")
	}
	p = evidence(a, "1", false)
	p.PaymentID = a.PaymentIDs[0]
	if _, err = a.Repay(date, p, false); err == nil {
		t.Fatal("reused disbursement payment")
	}
	if a.Assess(date, dec("-1")) == nil || a.Assess(date, dec(".001")) == nil {
		t.Fatal("invalid fee")
	}
	a = active(t)
	a.PaidSinceSchedule = dec("1000")
	if err := a.Assess(date, dec("10")); err != nil || a.Status != StatusActive || !a.Fees.IsZero() {
		t.Fatal("already paid instalment")
	}
}
func TestRepaymentPrepaymentAndPayoff(t *testing.T) {
	a := active(t)
	date := a.LastAccrual
	allocation, err := a.Repay(date, evidence(a, "200", false), true)
	if err != nil || !allocation.Principal.Equal(dec("200")) || !a.Schedule.TotalPrincipal().Equal(dec("1000")) || len(a.Schedule.Instalments) != 12 || !a.PaidSinceSchedule.IsZero() {
		t.Fatalf("prepayment %+v %v", allocation, err)
	}
	q, err := a.Quote(date.AddDate(0, 0, 10))
	if err != nil || !q.Equal(dec("1003.29")) {
		t.Fatalf("quote %s %v", q, err)
	}
	allocation, err = a.Repay(date.AddDate(0, 0, 10), evidence(a, q.String(), false), false)
	if err != nil || a.Status != StatusPaidOff || !a.Outstanding.IsZero() || !a.Interest.IsZero() || !allocation.Principal.Equal(dec("1000")) {
		t.Fatal("payoff", err)
	}
	if a.Accrue(date.AddDate(0, 0, 11)) == nil {
		t.Fatal("accrued after payoff")
	}
	// A matured loan receiving partial principal gets a one-instalment schedule.
	a = active(t)
	date = date.AddDate(2, 0, 0)
	q, _ = a.Quote(date)
	amount := q.Sub(dec("100"))
	if _, err = a.Repay(date, evidence(a, amount.String(), false), true); err != nil || len(a.Schedule.Instalments) != 1 || !a.Schedule.TotalPrincipal().Equal(dec("100")) {
		t.Fatal("mature prepayment", err)
	}
}
func TestRepaymentErrorsAreAtomicAndClonesAreIsolated(t *testing.T) {
	for _, amount := range []string{"0", "-1", ".001", "1200.01"} {
		a := active(t)
		before := a.Clone()
		if _, err := a.Repay(a.LastAccrual, evidence(a, amount, false), false); err == nil || !reflect.DeepEqual(a, before) {
			t.Fatal("invalid amount mutated", amount)
		}
	}
	a := active(t)
	before := a.Clone()
	p := evidence(a, "10", false)
	p.Status = "FAILED"
	if _, err := a.Repay(a.LastAccrual, p, false); err == nil || !reflect.DeepEqual(a, before) {
		t.Fatal("invalid evidence")
	}
	if _, err := a.Repay(a.LastAccrual.AddDate(0, 0, -1), p, false); err == nil {
		t.Fatal("backdated repayment")
	}
	b := a.Clone()
	b.Schedule.Instalments[0].Number = 99
	b.PaymentIDs[0] = uuid.New()
	if a.Schedule.Instalments[0].Number == 99 || a.PaymentIDs[0] == b.PaymentIDs[0] {
		t.Fatal("clone aliases slices")
	}
	if err := a.Assess(a.LastAccrual.AddDate(0, 2, 0), dec("1")); err != nil {
		t.Fatal(err)
	}
	b = a.Clone()
	b.FeeInstalments[0] = 99
	if a.FeeInstalments[0] == 99 {
		t.Fatal("fee alias")
	}
}

func FuzzRepaymentConservesCash(f *testing.F) {
	f.Add(uint32(120000), uint32(10000))
	f.Add(uint32(1), uint32(0))
	f.Fuzz(func(t *testing.T, principal, paid uint32) {
		cents := int64(principal%100000000) + 1
		payment := int64(paid)%cents + 1
		app := application()
		app.Terms.Principal = decimal.New(cents, -2)
		app.MonthlyIncome = dec("10000000")
		app.Terms.AnnualRate = decimal.Zero
		a, err := Originate(uuid.New(), app, "ALLOW")
		if err != nil {
			t.Fatal(err)
		}
		if err = a.Disburse(evidence(a, app.Terms.Principal.String(), true)); err != nil {
			t.Fatal(err)
		}
		amount := decimal.New(payment, -2)
		allocation, err := a.Repay(app.StartDate, evidence(a, amount.String(), false), true)
		if err != nil || !a.Outstanding.Add(allocation.Principal).Equal(app.Terms.Principal) || !allocation.Fees.Add(allocation.Interest).Add(allocation.Principal).Equal(amount) {
			t.Fatalf("cash not conserved: %+v %v", allocation, err)
		}
		if a.Status != StatusPaidOff && !a.Schedule.TotalPrincipal().Equal(a.Outstanding) {
			t.Fatal("schedule principal not conserved")
		}
	})
}
