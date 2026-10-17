package domain

import (
	"github.com/shopspring/decimal"
	"testing"
	"time"
)

func dec(s string) decimal.Decimal { return decimal.RequireFromString(s) }
func terms(m RepaymentMethod, r string, n int, p string) LoanTerms {
	return LoanTerms{Principal: dec(p), AnnualRate: dec(r), TermMonths: n, Currency: "USD", MinorUnitScale: 2, Method: m}
}
func TestSchedules(t *testing.T) {
	for _, x := range []struct {
		m     RepaymentMethod
		r     string
		n     int
		p     string
		first string
	}{{Annuity, "0.12", 12, "1200", "94.62"}, {StraightLine, "0.12", 12, "1200", "100"}, {Annuity, "0", 3, "10", "3.33"}, {StraightLine, "0", 3, "10", "3.33"}} {
		s, e := GenerateSchedule(terms(x.m, x.r, x.n, x.p), time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC))
		if e != nil {
			t.Fatal(e)
		}
		if !s.TotalPrincipal().Equal(dec(x.p)) {
			t.Fatalf("principal sum %s", s.TotalPrincipal())
		}
		if !s.Instalments[0].Principal.Equal(dec(x.first)) {
			t.Fatalf("first principal %s", s.Instalments[0].Principal)
		}
		if !s.Instalments[x.n-1].DueDate.Equal(time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC).AddDate(0, x.n, 0)) {
			t.Fatal("date")
		}
	}
}
func TestValidationAndMoneyHelpers(t *testing.T) {
	cases := []LoanTerms{terms(Annuity, "-1", 1, "1"), terms(Annuity, "0", 0, "1"), terms(Annuity, "0", 601, "1"), terms(Annuity, "0", 1, "0"), terms(Annuity, "0", 1, "1"), terms(Annuity, "0", 1, "1"), terms(Annuity, "0", 1, "1")}
	cases[4].Currency = "usd"
	cases[5].MinorUnitScale = -1
	cases[6].Method = "BAD"
	for _, x := range cases {
		if _, e := GenerateSchedule(x, time.Now()); e == nil {
			t.Errorf("accepted %+v", x)
		}
	}
	x := terms(Annuity, "0", 1, "1")
	x.MinorUnitScale = 7
	if _, e := GenerateSchedule(x, time.Now()); e == nil {
		t.Fatal("scale")
	}
	if _, e := Underwrite(dec("0"), dec("0"), dec("0"), dec(".4")); e == nil {
		t.Fatal("income")
	}
	if _, e := Underwrite(dec("10"), dec("-1"), dec("0"), dec(".4")); e == nil {
		t.Fatal("debt")
	}
	if _, e := Underwrite(dec("10"), dec("0"), dec("-1"), dec(".4")); e == nil {
		t.Fatal("payment")
	}
	if _, e := Underwrite(dec("10"), dec("0"), dec("0"), dec("0")); e == nil {
		t.Fatal("ratio")
	}
	if _, e := Underwrite(dec("10"), dec("0"), dec("0"), dec("1.1")); e == nil {
		t.Fatal("ratio upper")
	}
	if b, e := Underwrite(dec("100"), dec("0"), dec("25"), dec(".4")); e != nil || b != "LOW" {
		t.Fatalf("%s %v", b, e)
	}
	if b, e := Underwrite(dec("100"), dec("0"), dec("30"), dec(".4")); e != nil || b != "MEDIUM" {
		t.Fatalf("%s %v", b, e)
	}
	if b, e := Underwrite(dec("100"), dec("0"), dec("45"), dec(".5")); e != nil || b != "HIGH" {
		t.Fatalf("%s %v", b, e)
	}
	if _, e := Underwrite(dec("100"), dec("0"), dec("41"), dec(".4")); e == nil {
		t.Fatal("affordability")
	}
	if _, e := Payoff(dec("-1"), dec("0"), dec("0"), 2); e == nil {
		t.Fatal("payoff negative")
	}
	if _, e := ApplyPrepayment(dec("100"), dec("0"), 2); e == nil {
		t.Fatal("zero prepayment")
	}
	if _, e := ApplyPrepayment(dec("100"), dec("100"), 2); e == nil {
		t.Fatal("full prepayment")
	}
	if _, e := DailyInterest(dec("-1"), dec(".1"), 1, 2); e == nil {
		t.Fatal("negative principal")
	}
	if _, e := DailyInterest(dec("1"), dec("-.1"), 1, 2); e == nil {
		t.Fatal("negative rate")
	}
	if _, e := DailyInterest(dec("1"), dec(".1"), -1, 2); e == nil {
		t.Fatal("negative days")
	}
	v, e := DailyInterest(dec("1000"), dec(".365"), 1, 2)
	if e != nil || !v.Equal(dec("1")) {
		t.Fatalf("%s %v", v, e)
	}
	q, e := Payoff(dec("100.01"), dec("1.234"), dec(".005"), 2)
	if e != nil || !q.Equal(dec("101.25")) {
		t.Fatalf("%s %v", q, e)
	}
	r, e := ApplyPrepayment(dec("100"), dec("25"), 2)
	if e != nil || !r.Equal(dec("75")) {
		t.Fatalf("%s %v", r, e)
	}
}
func FuzzScheduleConservesPrincipal(f *testing.F) {
	f.Add(int64(1000), uint8(12))
	f.Add(int64(1), uint8(1))
	f.Fuzz(func(t *testing.T, cents int64, months uint8) {
		if cents <= 0 || cents > 1_000_000_000 || months == 0 || months > 60 {
			return
		}
		p := decimal.NewFromInt(cents).Div(dec("100"))
		x := terms(Annuity, ".075", int(months), p.StringFixed(2))
		s, e := GenerateSchedule(x, time.Date(2024, 1, 31, 0, 0, 0, 0, time.UTC))
		if e != nil {
			t.Fatal(e)
		}
		if !s.TotalPrincipal().Equal(x.Principal) {
			t.Fatalf("sum %s != %s", s.TotalPrincipal(), x.Principal)
		}
	})
}
